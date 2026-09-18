package mqttsimulator

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	telemetrySchemaVersion = 1
	telemetryTopicPrefix   = "pulsegrid/v1/tenants"
	maximumPayloadBytes    = 1024
)

// Telemetry is the versioned device observation published by the fixture.
// Additional fields are intentionally not part of this contract.
type Telemetry struct {
	SchemaVersion      int       `json:"schemaVersion"`
	MessageID          uuid.UUID `json:"messageId"`
	ObservedAt         time.Time `json:"observedAt"`
	TemperatureCelsius float64   `json:"temperatureCelsius"`
}

// Topic returns the exact v1 telemetry topic for the configured tenant/device.
func (c Config) Topic() string {
	return fmt.Sprintf("%s/%s/devices/%s/telemetry", telemetryTopicPrefix, c.TenantSlug, c.DeviceID.String())
}

// NewTelemetry creates one logical observation with a fresh identity and UTC
// timestamp. The timestamp is supplied by the caller so tests can be exact.
func NewTelemetry(observedAt time.Time, temperature float64) Telemetry {
	return Telemetry{
		SchemaVersion:      telemetrySchemaVersion,
		MessageID:          uuid.New(),
		ObservedAt:         observedAt.UTC(),
		TemperatureCelsius: temperature,
	}
}

// EncodeTelemetry serializes the v1 observation and enforces the fixture's
// bounded payload contract.
func EncodeTelemetry(telemetry Telemetry) ([]byte, error) {
	if telemetry.SchemaVersion != telemetrySchemaVersion {
		return nil, errors.New("telemetry schema version must be 1")
	}
	if telemetry.MessageID == uuid.Nil {
		return nil, errors.New("telemetry message ID is required")
	}
	if telemetry.ObservedAt.IsZero() {
		return nil, errors.New("telemetry observed time is required")
	}
	telemetry.ObservedAt = telemetry.ObservedAt.UTC()
	if math.IsNaN(telemetry.TemperatureCelsius) || math.IsInf(telemetry.TemperatureCelsius, 0) {
		return nil, errors.New("telemetry temperature must be finite")
	}

	encoded, err := json.Marshal(telemetry)
	if err != nil {
		return nil, errors.New("encode telemetry payload")
	}
	if len(encoded) >= maximumPayloadBytes {
		return nil, errors.New("telemetry payload exceeds 1 KiB")
	}
	return encoded, nil
}

func validateTopic(topic string) error {
	if topic == "" || strings.ContainsAny(topic, "#+") {
		return errors.New("telemetry topic contains an MQTT wildcard")
	}
	return nil
}
