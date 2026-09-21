package ingestion

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeResolver struct {
	organizationID uuid.UUID
	err            error
	called         bool
}

func (r *fakeResolver) ResolveDevice(context.Context, string, uuid.UUID) (uuid.UUID, error) {
	r.called = true
	return r.organizationID, r.err
}

type fakeConsumer struct {
	accepted []AcceptedTelemetry
	err      error
}

func (c *fakeConsumer) Consume(_ context.Context, telemetry AcceptedTelemetry) error {
	c.accepted = append(c.accepted, telemetry)
	return c.err
}

func TestHandlerAcceptsValidTelemetryAndAssignsMetadata(t *testing.T) {
	organizationID := uuid.New()
	deviceID := uuid.New()
	messageID := uuid.New()
	ingestionID := uuid.New()
	receivedAt := time.Date(2026, time.September, 21, 4, 0, 0, 0, time.UTC)
	resolver := &fakeResolver{organizationID: organizationID}
	consumer := &fakeConsumer{}
	handler, err := NewHandler(resolver, consumer, HandlerConfig{
		Now:   func() time.Time { return receivedAt },
		NewID: func() uuid.UUID { return ingestionID },
	})
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}

	accepted, err := handler.Handle(context.Background(), Delivery{
		IngestionID: ingestionID,
		Topic:       "pulsegrid/v1/tenants/pulsegrid-dev/devices/" + deviceID.String() + "/telemetry",
		Payload:     []byte(`{"schemaVersion":1,"messageId":"` + messageID.String() + `","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":23.5}`),
		QoS:         1,
		Duplicate:   true,
		ReceivedAt:  receivedAt,
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if accepted.IngestionID != ingestionID || accepted.MessageID != messageID || accepted.OrganizationID != organizationID || accepted.DeviceID != deviceID || !accepted.MQTTDuplicate {
		t.Fatalf("accepted metadata = %+v", accepted)
	}
	if accepted.TemperatureCelsius != 23.5 || !accepted.ObservedAt.Equal(time.Date(2026, time.September, 21, 3, 59, 0, 0, time.UTC)) || !accepted.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("accepted values = %+v", accepted)
	}
	if !resolver.called || len(consumer.accepted) != 1 {
		t.Fatalf("resolver called=%t consumer calls=%d", resolver.called, len(consumer.accepted))
	}
}

func TestHandlerRejectsInvalidDeliveriesWithoutCallingResolver(t *testing.T) {
	deviceID := uuid.New()
	messageID := uuid.New()
	base := Delivery{
		Topic:      "pulsegrid/v1/tenants/pulsegrid-dev/devices/" + deviceID.String() + "/telemetry",
		Payload:    []byte(`{"schemaVersion":1,"messageId":"` + messageID.String() + `","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":23.5}`),
		QoS:        1,
		ReceivedAt: time.Date(2026, time.September, 21, 4, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name   string
		mutate func(*Delivery)
		want   string
	}{
		{name: "topic shape", mutate: func(delivery *Delivery) { delivery.Topic = "pulsegrid/v1/other" }, want: ReasonInvalidTopic},
		{name: "invalid tenant slug", mutate: func(delivery *Delivery) {
			delivery.Topic = strings.Replace(delivery.Topic, "pulsegrid-dev", "Pulsegrid", 1)
		}, want: ReasonInvalidTopic},
		{name: "unsupported qos", mutate: func(delivery *Delivery) { delivery.QoS = 0 }, want: ReasonUnsupportedQoS},
		{name: "retained", mutate: func(delivery *Delivery) { delivery.Retained = true }, want: ReasonRetainedMessage},
		{name: "oversized", mutate: func(delivery *Delivery) { delivery.Payload = []byte(strings.Repeat("x", MaxPayloadBytes)) }, want: ReasonPayloadTooLarge},
		{name: "invalid utf8", mutate: func(delivery *Delivery) { delivery.Payload = []byte{0xff} }, want: ReasonPayloadInvalidUTF8},
		{name: "unknown field", mutate: func(delivery *Delivery) {
			delivery.Payload = []byte(`{"schemaVersion":1,"messageId":"` + messageID.String() + `","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":23.5,"extra":true}`)
		}, want: ReasonPayloadMalformed},
		{name: "duplicate field", mutate: func(delivery *Delivery) {
			delivery.Payload = []byte(`{"schemaVersion":1,"schemaVersion":1,"messageId":"` + messageID.String() + `","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":23.5}`)
		}, want: ReasonPayloadMalformed},
		{name: "trailing value", mutate: func(delivery *Delivery) { delivery.Payload = append(delivery.Payload, []byte(` null`)...) }, want: ReasonPayloadMalformed},
		{name: "unsupported version", mutate: func(delivery *Delivery) {
			delivery.Payload = []byte(`{"schemaVersion":2,"messageId":"` + messageID.String() + `","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":23.5}`)
		}, want: ReasonSchemaVersionUnsupported},
		{name: "invalid message ID", mutate: func(delivery *Delivery) {
			delivery.Payload = []byte(`{"schemaVersion":1,"messageId":"BAD","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":23.5}`)
		}, want: ReasonMessageIDInvalid},
		{name: "non-UTC observedAt", mutate: func(delivery *Delivery) {
			delivery.Payload = []byte(`{"schemaVersion":1,"messageId":"` + messageID.String() + `","observedAt":"2026-09-21T03:59:00+01:00","temperatureCelsius":23.5}`)
		}, want: ReasonObservedAtInvalid},
		{name: "future observedAt", mutate: func(delivery *Delivery) {
			delivery.Payload = []byte(`{"schemaVersion":1,"messageId":"` + messageID.String() + `","observedAt":"2026-09-21T04:05:01Z","temperatureCelsius":23.5}`)
		}, want: ReasonObservedAtFuture},
		{name: "invalid temperature", mutate: func(delivery *Delivery) {
			delivery.Payload = []byte(`{"schemaVersion":1,"messageId":"` + messageID.String() + `","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":null}`)
		}, want: ReasonTemperatureInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeResolver{organizationID: uuid.New()}
			consumer := &fakeConsumer{}
			handler, err := NewHandler(resolver, consumer, HandlerConfig{})
			if err != nil {
				t.Fatalf("NewHandler returned error: %v", err)
			}
			delivery := base
			delivery.Payload = append([]byte(nil), base.Payload...)
			test.mutate(&delivery)
			if _, err := handler.Handle(context.Background(), delivery); err == nil || ReasonOf(err) != test.want || !IsRejected(err) {
				t.Fatalf("Handle error = %v reason=%q, want rejected %q", err, ReasonOf(err), test.want)
			}
			if resolver.called || len(consumer.accepted) != 0 {
				t.Fatalf("invalid delivery crossed boundary: resolver=%t consumer=%d", resolver.called, len(consumer.accepted))
			}
		})
	}
}

func TestHandlerMapsRegistryAndConsumerFailures(t *testing.T) {
	deviceID := uuid.New()
	delivery := validDelivery(deviceID)
	for _, test := range []struct {
		name        string
		resolverErr error
		consumerErr error
		wantReason  string
		wantReject  bool
	}{
		{name: "unknown device", resolverErr: ErrDeviceNotFound, wantReason: ReasonDeviceNotRegistered, wantReject: true},
		{name: "registry down", resolverErr: errors.New("database unavailable"), wantReason: ReasonRegistryUnavailable},
		{name: "consumer down", consumerErr: errors.New("consumer unavailable"), wantReason: ReasonConsumerFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeResolver{organizationID: uuid.New(), err: test.resolverErr}
			consumer := &fakeConsumer{err: test.consumerErr}
			handler, err := NewHandler(resolver, consumer, HandlerConfig{})
			if err != nil {
				t.Fatalf("NewHandler returned error: %v", err)
			}
			if _, err := handler.Handle(context.Background(), delivery); err == nil || ReasonOf(err) != test.wantReason || IsRejected(err) != test.wantReject {
				t.Fatalf("Handle error = %v reason=%q rejected=%t", err, ReasonOf(err), IsRejected(err))
			}
		})
	}
}

func TestHandlerUsesOldTimestampsAndDistinctDeliveryIDs(t *testing.T) {
	deviceID := uuid.New()
	resolver := &fakeResolver{organizationID: uuid.New()}
	consumer := &fakeConsumer{}
	handler, err := NewHandler(resolver, consumer, HandlerConfig{
		Now: func() time.Time { return time.Date(2026, time.September, 21, 4, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	first := validDelivery(deviceID)
	first.IngestionID = uuid.New()
	second := first
	second.IngestionID = uuid.New()
	for _, delivery := range []Delivery{first, second} {
		if _, err := handler.Handle(context.Background(), delivery); err != nil {
			t.Fatalf("Handle returned error: %v", err)
		}
	}
	if len(consumer.accepted) != 2 || consumer.accepted[0].MessageID != consumer.accepted[1].MessageID || consumer.accepted[0].IngestionID == consumer.accepted[1].IngestionID {
		t.Fatalf("duplicate delivery metadata = %+v", consumer.accepted)
	}
}

func TestHandlerRejectsCanceledContext(t *testing.T) {
	handler, err := NewHandler(&fakeResolver{organizationID: uuid.New()}, &fakeConsumer{}, HandlerConfig{})
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handler.Handle(ctx, validDelivery(uuid.New())); err == nil || ReasonOf(err) != ReasonShutdownInterrupted || !IsFailed(err) {
		t.Fatalf("Handle error = %v reason=%q", err, ReasonOf(err))
	}
}

func FuzzDecodeTelemetryDoesNotPanic(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":1,"messageId":"11111111-1111-4111-8111-111111111111","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":23.5}`))
	f.Add([]byte(`{"schemaVersion":1,"schemaVersion":1}`))
	f.Add([]byte{0xff, 0xfe, 0xfd})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 4096 {
			t.Skip()
		}
		_, _ = decodeTelemetry(raw)
	})
}

func validDelivery(deviceID uuid.UUID) Delivery {
	return Delivery{
		IngestionID: uuid.New(),
		Topic:       "pulsegrid/v1/tenants/pulsegrid-dev/devices/" + deviceID.String() + "/telemetry",
		Payload:     []byte(`{"schemaVersion":1,"messageId":"11111111-1111-4111-8111-111111111111","observedAt":"2026-09-21T03:59:00Z","temperatureCelsius":23.5}`),
		QoS:         1,
		ReceivedAt:  time.Date(2026, time.September, 21, 4, 0, 0, 0, time.UTC),
	}
}
