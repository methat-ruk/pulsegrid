// Package contract owns the versioned telemetry wire representation shared by
// the local producer fixture and the application ingestion boundary.
package contract

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	SchemaVersion   = 1
	MaxPayloadBytes = 1024
	TopicPrefix     = "pulsegrid/v1/tenants"
)

// Telemetry is the versioned device observation on the MQTT wire.
type Telemetry struct {
	SchemaVersion      int       `json:"schemaVersion"`
	MessageID          uuid.UUID `json:"messageId"`
	ObservedAt         time.Time `json:"observedAt"`
	TemperatureCelsius float64   `json:"temperatureCelsius"`
}

// Topic returns the exact v1 telemetry topic for a tenant/device pair.
func Topic(tenantSlug string, deviceID uuid.UUID) string {
	return fmt.Sprintf("%s/%s/devices/%s/telemetry", TopicPrefix, tenantSlug, deviceID.String())
}

// NewTelemetry creates one logical observation with a fresh identity and UTC
// timestamp. The timestamp is supplied by the caller so tests can be exact.
func NewTelemetry(observedAt time.Time, temperature float64) Telemetry {
	return Telemetry{
		SchemaVersion:      SchemaVersion,
		MessageID:          uuid.New(),
		ObservedAt:         observedAt.UTC(),
		TemperatureCelsius: temperature,
	}
}

// EncodeTelemetry serializes and bounds one v1 observation.
func EncodeTelemetry(telemetry Telemetry) ([]byte, error) {
	if telemetry.SchemaVersion != SchemaVersion {
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
	if len(encoded) >= MaxPayloadBytes {
		return nil, errors.New("telemetry payload exceeds 1 KiB")
	}
	return encoded, nil
}
