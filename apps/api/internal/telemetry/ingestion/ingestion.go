// Package ingestion owns the transport-independent MQTT telemetry boundary.
// It validates untrusted topic/payload values, resolves tenant/device
// authority, and hands a narrow accepted value to the next application stage.
package ingestion

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	telemetrycontract "github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/contract"
)

const (
	TelemetrySchemaVersion = telemetrycontract.SchemaVersion
	MaxPayloadBytes        = telemetrycontract.MaxPayloadBytes
	DefaultFutureSkew      = 5 * time.Minute
	TelemetryTopicFilter   = "pulsegrid/v1/tenants/+/devices/+/telemetry"
)

type ErrorClass string

const (
	Rejected ErrorClass = "rejected"
	Failed   ErrorClass = "failed"
)

const (
	ReasonInvalidTopic             = "telemetry_topic_invalid"
	ReasonUnsupportedQoS           = "telemetry_qos_unsupported"
	ReasonRetainedMessage          = "telemetry_retained_rejected"
	ReasonPayloadTooLarge          = "telemetry_payload_too_large"
	ReasonPayloadInvalidUTF8       = "telemetry_payload_invalid_utf8"
	ReasonPayloadMalformed         = "telemetry_payload_malformed"
	ReasonSchemaVersionUnsupported = "telemetry_schema_version_unsupported"
	ReasonMessageIDInvalid         = "telemetry_message_id_invalid"
	ReasonObservedAtInvalid        = "telemetry_observed_at_invalid"
	ReasonObservedAtFuture         = "telemetry_observed_at_future"
	ReasonTemperatureInvalid       = "telemetry_temperature_invalid"
	ReasonDeviceNotRegistered      = "telemetry_device_not_registered_for_tenant"
	ReasonRegistryUnavailable      = "telemetry_registry_unavailable"
	ReasonConsumerFailed           = "telemetry_consumer_failed"
	ReasonShutdownInterrupted      = "telemetry_shutdown_interrupted"
)

var (
	ErrDeviceNotFound = errors.New("device not found for tenant")
)

// Error is a safe, stable classification returned to the process boundary.
// The underlying cause is retained for tests and callers that need errors.Is,
// but callers should log Reason rather than the cause.
type Error struct {
	Class  ErrorClass
	Reason string
	Cause  error
}

func (e *Error) Error() string {
	return e.Reason
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func newRejected(reason string, cause error) error {
	return &Error{Class: Rejected, Reason: reason, Cause: cause}
}

func newFailed(reason string, cause error) error {
	return &Error{Class: Failed, Reason: reason, Cause: cause}
}

// ReasonOf returns a stable reason code suitable for structured logs.
func ReasonOf(err error) string {
	if classified, ok := errors.AsType[*Error](err); ok {
		return classified.Reason
	}
	return ReasonConsumerFailed
}

// IsRejected reports whether an error represents a permanent input rejection.
func IsRejected(err error) bool {
	classified, ok := errors.AsType[*Error](err)
	return ok && classified.Class == Rejected
}

// IsFailed reports whether an error represents a dependency or application
// processing failure rather than invalid input.
func IsFailed(err error) bool {
	classified, ok := errors.AsType[*Error](err)
	return ok && classified.Class == Failed
}

// Delivery is an owned copy of one MQTT message and its transport metadata.
// The transport must copy topic and payload before enqueueing this value.
type Delivery struct {
	IngestionID uuid.UUID
	Topic       string
	Payload     []byte
	QoS         byte
	Retained    bool
	Duplicate   bool
	ReceivedAt  time.Time
}

// AcceptedTelemetry is the narrow application value consumed by MVP-006.
// OrganizationID comes from the registry, never from the MQTT topic.
type AcceptedTelemetry struct {
	IngestionID        uuid.UUID
	MessageID          uuid.UUID
	OrganizationID     uuid.UUID
	DeviceID           uuid.UUID
	ObservedAt         time.Time
	ReceivedAt         time.Time
	TemperatureCelsius float64
	MQTTDuplicate      bool
}

// DeviceResolver is the only registry capability required by ingestion.
type DeviceResolver interface {
	ResolveDevice(ctx context.Context, tenantSlug string, deviceID uuid.UUID) (uuid.UUID, error)
}

// AcceptedTelemetryConsumer receives a validated, tenant-resolved telemetry
// value. MVP-006 can replace the diagnostic sink without changing transport or
// validation ownership.
type AcceptedTelemetryConsumer interface {
	Consume(ctx context.Context, telemetry AcceptedTelemetry) error
}

// Consumer is retained as a concise local alias for callers inside the package
// boundary and tests.
type Consumer = AcceptedTelemetryConsumer

// ConsumerFunc adapts a function to Consumer.
type ConsumerFunc func(context.Context, AcceptedTelemetry) error

func (f ConsumerFunc) Consume(ctx context.Context, telemetry AcceptedTelemetry) error {
	return f(ctx, telemetry)
}

// HandlerConfig contains deterministic sources and bounded policy for Handler.
type HandlerConfig struct {
	Now           func() time.Time
	NewID         func() uuid.UUID
	MaxFutureSkew time.Duration
}

// Handler validates and resolves one MQTT delivery.
type Handler struct {
	resolver      DeviceResolver
	consumer      AcceptedTelemetryConsumer
	now           func() time.Time
	newID         func() uuid.UUID
	maxFutureSkew time.Duration
}

// NewHandler creates the transport-independent ingestion boundary.
func NewHandler(resolver DeviceResolver, consumer AcceptedTelemetryConsumer, config HandlerConfig) (*Handler, error) {
	if resolver == nil {
		return nil, errors.New("telemetry ingestion requires a device resolver")
	}
	if consumer == nil {
		return nil, errors.New("telemetry ingestion requires a consumer")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewID == nil {
		config.NewID = uuid.New
	}
	if config.MaxFutureSkew <= 0 {
		config.MaxFutureSkew = DefaultFutureSkew
	}
	return &Handler{
		resolver:      resolver,
		consumer:      consumer,
		now:           config.Now,
		newID:         config.NewID,
		maxFutureSkew: config.MaxFutureSkew,
	}, nil
}

// Handle validates, resolves, and synchronously emits one accepted delivery.
func (h *Handler) Handle(ctx context.Context, delivery Delivery) (AcceptedTelemetry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return AcceptedTelemetry{}, newFailed(ReasonShutdownInterrupted, err)
	}

	ingestionID := delivery.IngestionID
	if ingestionID == uuid.Nil {
		ingestionID = h.newID()
		if ingestionID == uuid.Nil {
			return AcceptedTelemetry{}, newFailed(ReasonConsumerFailed, errors.New("telemetry ingestion ID generator returned nil UUID"))
		}
	}
	receivedAt := delivery.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = h.now().UTC()
	}
	if receivedAt.IsZero() {
		return AcceptedTelemetry{}, newFailed(ReasonConsumerFailed, errors.New("telemetry receive clock returned zero time"))
	}

	topic, err := parseTopic(delivery.Topic)
	if err != nil {
		return AcceptedTelemetry{}, newRejected(ReasonInvalidTopic, err)
	}
	if delivery.QoS != 1 {
		return AcceptedTelemetry{}, newRejected(ReasonUnsupportedQoS, nil)
	}
	if delivery.Retained {
		return AcceptedTelemetry{}, newRejected(ReasonRetainedMessage, nil)
	}
	if len(delivery.Payload) >= MaxPayloadBytes {
		return AcceptedTelemetry{}, newRejected(ReasonPayloadTooLarge, nil)
	}
	if !validUTF8(delivery.Payload) {
		return AcceptedTelemetry{}, newRejected(ReasonPayloadInvalidUTF8, nil)
	}

	payload, err := decodeTelemetry(delivery.Payload)
	if err != nil {
		return AcceptedTelemetry{}, newRejected(payloadReason(err), err)
	}
	if payload.ObservedAt.After(receivedAt.UTC().Add(h.maxFutureSkew)) {
		return AcceptedTelemetry{}, newRejected(ReasonObservedAtFuture, nil)
	}

	organizationID, err := h.resolver.ResolveDevice(ctx, topic.TenantSlug, topic.DeviceID)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			return AcceptedTelemetry{}, newRejected(ReasonDeviceNotRegistered, nil)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return AcceptedTelemetry{}, newFailed(ReasonShutdownInterrupted, err)
		}
		return AcceptedTelemetry{}, newFailed(ReasonRegistryUnavailable, err)
	}
	if organizationID == uuid.Nil {
		return AcceptedTelemetry{}, newFailed(ReasonRegistryUnavailable, errors.New("registry returned nil organization ID"))
	}

	accepted := AcceptedTelemetry{
		IngestionID:        ingestionID,
		MessageID:          payload.MessageID,
		OrganizationID:     organizationID,
		DeviceID:           topic.DeviceID,
		ObservedAt:         payload.ObservedAt,
		ReceivedAt:         receivedAt.UTC(),
		TemperatureCelsius: payload.TemperatureCelsius,
		MQTTDuplicate:      delivery.Duplicate,
	}
	if err := h.consumer.Consume(ctx, accepted); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return AcceptedTelemetry{}, newFailed(ReasonShutdownInterrupted, err)
		}
		return AcceptedTelemetry{}, newFailed(ReasonConsumerFailed, err)
	}
	return accepted, nil
}
