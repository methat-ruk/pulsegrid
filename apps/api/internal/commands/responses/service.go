// Package responses validates command-device replies and asks the command
// authority to apply outcomes under tenant/device/command scope.
package responses

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqttcontract"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands/mqtttransport"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
)

const processingTimeout = 2 * time.Second

type DeviceResolver interface {
	ResolveDevice(context.Context, string, uuid.UUID) (uuid.UUID, error)
}

type Repository interface {
	ApplyDeviceResponse(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, commands.Event, commands.FailureCode) (commands.Command, error)
}

type Service struct {
	resolver   DeviceResolver
	repository Repository
	transport  *mqtttransport.Transport
	logger     *slog.Logger
	context    context.Context
	cancel     context.CancelFunc
	stop       chan struct{}
	done       chan struct{}
	start      atomic.Bool
	ready      atomic.Bool
	stopOnce   sync.Once
}

func New(resolver DeviceResolver, repository Repository, transport *mqtttransport.Transport, logger *slog.Logger) (*Service, error) {
	if resolver == nil || repository == nil || transport == nil {
		return nil, errors.New("command response service requires resolver, repository, and transport")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		resolver: resolver, repository: repository, transport: transport,
		logger: logger, stop: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

// Start owns one bounded response worker. Its processing context survives the
// process signal long enough to drain admitted responses during Stop.
func (s *Service) Start() error {
	if !s.start.CompareAndSwap(false, true) {
		return errors.New("command response service has already started")
	}
	s.context, s.cancel = context.WithCancel(context.Background())
	s.ready.Store(true)
	go s.run()
	return nil
}

func (s *Service) Ready() bool { return s.ready.Load() }

// Stop is called after transport admission has stopped. It drains the now
// stable queue until empty or the shared shutdown deadline expires.
func (s *Service) Stop(ctx context.Context) error {
	if !s.start.Load() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ready.Store(false)
	s.stopOnce.Do(func() { close(s.stop) })
	select {
	case <-s.done:
		s.cancel()
		return nil
	case <-ctx.Done():
		s.cancel()
		return ctx.Err()
	}
}

func (s *Service) run() {
	defer close(s.done)
	queue := s.transport.Deliveries()
	for {
		select {
		case delivery := <-queue:
			s.process(delivery)
		case <-s.stop:
			for {
				select {
				case delivery := <-queue:
					s.process(delivery)
				default:
					return
				}
			}
		case <-s.context.Done():
			return
		}
	}
}

func (s *Service) process(delivery mqtttransport.Delivery) {
	if delivery.Oversized {
		s.logger.Warn("command response rejected", "reason_code", "command_response_too_large", "delivery_id", delivery.ID, "payload_bytes", delivery.PayloadBytes)
		return
	}
	if delivery.TopicInvalid {
		s.logger.Warn("command response rejected", "reason_code", "command_response_topic_too_large", "delivery_id", delivery.ID, "payload_bytes", delivery.PayloadBytes)
		return
	}
	if delivery.QoS != 1 {
		s.logger.Warn("command response rejected", "reason_code", "command_response_qos_unsupported", "delivery_id", delivery.ID, "qos", delivery.QoS)
		return
	}
	if delivery.Retained {
		s.logger.Warn("command response rejected", "reason_code", "command_response_retained_rejected", "delivery_id", delivery.ID)
		return
	}
	tenant, deviceID, err := mqttcontract.ParseDeviceTopic(delivery.Topic, mqttcontract.ResponseSuffix)
	if err != nil {
		s.logger.Warn("command response rejected", "reason_code", "command_response_topic_invalid", "delivery_id", delivery.ID, "payload_bytes", delivery.PayloadBytes)
		return
	}
	response, err := mqttcontract.DecodeResponse(delivery.Payload)
	if err != nil {
		s.logger.Warn("command response rejected", "reason_code", "command_response_payload_invalid", "delivery_id", delivery.ID, "tenant", tenant, "device_id", deviceID, "payload_bytes", delivery.PayloadBytes)
		return
	}
	parent := s.context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, processingTimeout)
	defer cancel()
	organizationID, err := s.resolver.ResolveDevice(ctx, tenant, deviceID)
	if err != nil {
		reason := "command_response_registry_unavailable"
		if errors.Is(err, registry.ErrNotFound) {
			reason = "command_response_device_unregistered"
		}
		s.logger.Warn("command response device was not accepted", "reason_code", reason, "delivery_id", delivery.ID, "tenant", tenant, "device_id", deviceID)
		return
	}
	event, failureCode := responseTransition(response)
	command, err := s.repository.ApplyDeviceResponse(ctx, organizationID, deviceID, response.CommandID, event, failureCode)
	if errors.Is(err, commands.ErrNotFound) {
		s.logger.Warn("command response did not match a command", "reason_code", "command_response_command_unmatched", "delivery_id", delivery.ID, "tenant", tenant, "organization_id", organizationID, "device_id", deviceID, "command_id", response.CommandID)
		return
	}
	if errors.Is(err, commands.ErrTimedOut) {
		s.logger.Info("late command response left timeout unchanged", "reason_code", "command_response_late", "delivery_id", delivery.ID, "tenant", tenant, "organization_id", organizationID, "device_id", deviceID, "command_id", response.CommandID)
		return
	}
	if err != nil {
		reason := "command_response_transition_invalid"
		if errors.Is(err, commands.ErrConflict) {
			reason = "command_response_terminal_conflict"
		}
		s.logger.Warn("command response did not change state", "reason_code", reason, "delivery_id", delivery.ID, "tenant", tenant, "organization_id", organizationID, "device_id", deviceID, "command_id", response.CommandID)
		return
	}
	s.logger.Info("command response applied", "reason_code", "command_response_applied", "delivery_id", delivery.ID, "tenant", tenant, "organization_id", organizationID, "device_id", deviceID, "command_id", command.ID, "status", command.Status, "duplicate", delivery.Duplicate, "mqtt_epoch", delivery.Generation)
}

func responseTransition(response mqttcontract.Response) (commands.Event, commands.FailureCode) {
	switch response.Outcome {
	case mqttcontract.OutcomeAck:
		return commands.EventAck, ""
	case mqttcontract.OutcomeCompleted:
		return commands.EventComplete, ""
	default:
		return commands.EventFail, commands.FailureDeviceReported
	}
}
