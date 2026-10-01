// Package delivery owns bounded orchestration from durable command intent to
// MQTT broker acknowledgement, plus independent command expiry.
package delivery

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
)

const (
	defaultPollInterval = time.Second
	defaultDBTimeout    = 2 * time.Second
	defaultPublishLimit = 5 * time.Second
)

type Repository interface {
	ClaimNextDispatch(context.Context, uuid.UUID) (*commands.Command, error)
	MarkDispatched(context.Context, uuid.UUID, uuid.UUID) (commands.Command, error)
	Fail(context.Context, uuid.UUID, uuid.UUID, commands.FailureCode) (commands.Command, error)
	ExpireDueForOrganization(context.Context, uuid.UUID, int) ([]commands.Command, error)
}

type Publisher interface {
	Ready() bool
	Publish(context.Context, string, []byte) error
}

type Clock interface{ Now() time.Time }
type clockFunc func() time.Time

func (f clockFunc) Now() time.Time { return f() }

type Config struct {
	PollInterval time.Duration
	DBTimeout    time.Duration
	PublishLimit time.Duration
	ExpireBatch  int
}

func DefaultConfig() Config {
	return Config{PollInterval: defaultPollInterval, DBTimeout: defaultDBTimeout, PublishLimit: defaultPublishLimit, ExpireBatch: commands.MaxExpireBatchSize}
}

type Runtime struct {
	repository Repository
	publisher  Publisher
	orgID      uuid.UUID
	tenantSlug string
	config     Config
	clock      Clock
	logger     *slog.Logger

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	start   atomic.Bool
	healthy atomic.Bool
}

func New(repository Repository, publisher Publisher, organizationID uuid.UUID, tenantSlug string, config Config, clock Clock, logger *slog.Logger) (*Runtime, error) {
	if repository == nil || publisher == nil || organizationID == uuid.Nil {
		return nil, errors.New("command delivery runtime requires repository, publisher, and organization")
	}
	if _, err := mqttcontract.ResponseFilter(tenantSlug); err != nil {
		return nil, errors.New("command delivery runtime tenant is invalid")
	}
	defaults := DefaultConfig()
	if config.PollInterval <= 0 {
		config.PollInterval = defaults.PollInterval
	}
	if config.DBTimeout <= 0 {
		config.DBTimeout = defaults.DBTimeout
	}
	if config.PublishLimit <= 0 {
		config.PublishLimit = defaults.PublishLimit
	}
	if config.ExpireBatch <= 0 || config.ExpireBatch > commands.MaxExpireBatchSize {
		config.ExpireBatch = defaults.ExpireBatch
	}
	if clock == nil {
		clock = clockFunc(time.Now)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runtime{
		repository: repository, publisher: publisher, orgID: organizationID,
		tenantSlug: tenantSlug, config: config, clock: clock, logger: logger,
		done: make(chan struct{}),
	}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !r.start.CompareAndSwap(false, true) {
		return errors.New("command delivery runtime has already started")
	}
	r.ctx, r.cancel = context.WithCancel(ctx)
	r.healthy.Store(true)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); r.dispatchLoop() }()
	go func() { defer workers.Done(); r.expiryLoop() }()
	go func() {
		workers.Wait()
		r.healthy.Store(false)
		close(r.done)
	}()
	return nil
}

func (r *Runtime) Ready() bool { return r.healthy.Load() }

func (r *Runtime) Stop(ctx context.Context) error {
	if !r.start.Load() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) dispatchLoop() {
	if err := r.DispatchOnce(r.ctx); err != nil && r.ctx.Err() == nil {
		r.logger.Error("command dispatch iteration failed", "reason_code", "command_dispatch_iteration_failed", "organization_id", r.orgID)
	}
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			if err := r.DispatchOnce(r.ctx); err != nil && r.ctx.Err() == nil {
				r.logger.Error("command dispatch iteration failed", "reason_code", "command_dispatch_iteration_failed", "organization_id", r.orgID)
			}
		}
	}
}

func (r *Runtime) expiryLoop() {
	if err := r.ExpireOnce(r.ctx); err != nil && r.ctx.Err() == nil {
		r.logger.Error("command expiry iteration failed", "reason_code", "command_expiry_iteration_failed", "organization_id", r.orgID)
	}
	ticker := time.NewTicker(r.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			if err := r.ExpireOnce(r.ctx); err != nil && r.ctx.Err() == nil {
				r.logger.Error("command expiry iteration failed", "reason_code", "command_expiry_iteration_failed", "organization_id", r.orgID)
			}
		}
	}
}

func (r *Runtime) DispatchOnce(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !r.publisher.Ready() {
		return nil
	}
	claimContext, cancelClaim := context.WithTimeout(ctx, r.config.DBTimeout)
	command, err := r.repository.ClaimNextDispatch(claimContext, r.orgID)
	cancelClaim()
	if err != nil {
		return err
	}
	if command == nil || !r.publisher.Ready() || ctx.Err() != nil {
		return nil
	}
	remaining := command.ExpiresAt.Sub(r.clock.Now())
	if remaining <= 0 {
		return nil
	}
	topic, err := mqttcontract.CommandTopic(r.tenantSlug, command.DeviceID)
	if err != nil {
		r.failLocalCommand(ctx, *command, "command_topic_invalid")
		return err
	}
	payload, err := mqttcontract.EncodeCommand(command.ID, string(command.Type), command.CreatedAt, command.ExpiresAt)
	if err != nil {
		r.failLocalCommand(ctx, *command, "command_contract_invalid")
		return err
	}
	deadline := r.clock.Now().Add(r.config.PublishLimit)
	if deadline.After(command.ExpiresAt) {
		deadline = command.ExpiresAt
	}
	publishContext, cancelPublish := context.WithDeadline(ctx, deadline)
	err = r.publisher.Publish(publishContext, topic, payload)
	cancelPublish()
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Warn("command publish outcome is uncertain", "reason_code", "command_publish_uncertain", "organization_id", r.orgID, "device_id", command.DeviceID, "command_id", command.ID, "attempt", command.DispatchAttempts)
		}
		return nil
	}
	markContext, cancelMark := context.WithTimeout(ctx, r.config.DBTimeout)
	dispatched, err := r.repository.MarkDispatched(markContext, r.orgID, command.ID)
	cancelMark()
	if err != nil {
		r.logger.Error("command dispatch state could not be confirmed", "reason_code", "command_dispatch_state_write_failed", "organization_id", r.orgID, "device_id", command.DeviceID, "command_id", command.ID, "attempt", command.DispatchAttempts)
		return nil
	}
	if dispatched.Status == commands.StatusDispatched {
		r.logger.Info("command accepted by MQTT broker", "reason_code", "command_dispatched", "organization_id", r.orgID, "device_id", command.DeviceID, "command_id", command.ID, "attempt", command.DispatchAttempts)
	}
	return nil
}

func (r *Runtime) failLocalCommand(ctx context.Context, command commands.Command, reason string) {
	failureContext, cancel := context.WithTimeout(ctx, r.config.DBTimeout)
	defer cancel()
	failed, err := r.repository.Fail(failureContext, r.orgID, command.ID, commands.FailureDelivery)
	if err != nil {
		r.logger.Error("invalid command could not be failed", "reason_code", "command_delivery_rejection_state_failed", "organization_id", r.orgID, "device_id", command.DeviceID, "command_id", command.ID)
		return
	}
	r.logger.Error("command rejected before MQTT publish", "reason_code", reason, "organization_id", r.orgID, "device_id", command.DeviceID, "command_id", command.ID, "status", failed.Status)
}

func (r *Runtime) ExpireOnce(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	expiryContext, cancel := context.WithTimeout(ctx, r.config.DBTimeout)
	expired, err := r.repository.ExpireDueForOrganization(expiryContext, r.orgID, r.config.ExpireBatch)
	cancel()
	if err != nil {
		return err
	}
	for _, command := range expired {
		r.logger.Info("command reached its deadline", "reason_code", "command_timed_out", "organization_id", r.orgID, "device_id", command.DeviceID, "command_id", command.ID)
	}
	return nil
}
