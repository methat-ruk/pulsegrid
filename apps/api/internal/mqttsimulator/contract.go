package mqttsimulator

import (
	"errors"
	"strings"
	"time"

	telemetrycontract "github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/contract"
)

const (
	telemetrySchemaVersion = telemetrycontract.SchemaVersion
	maximumPayloadBytes    = telemetrycontract.MaxPayloadBytes
)

// Telemetry aliases the shared versioned wire contract.
type Telemetry = telemetrycontract.Telemetry

// Topic returns the exact v1 telemetry topic for the configured tenant/device.
func (c Config) Topic() string {
	return telemetrycontract.Topic(c.TenantSlug, c.DeviceID)
}

// NewTelemetry creates one logical observation with a fresh identity and UTC
// timestamp. The timestamp is supplied by the caller so tests can be exact.
func NewTelemetry(observedAt time.Time, temperature float64) Telemetry {
	return telemetrycontract.NewTelemetry(observedAt, temperature)
}

// EncodeTelemetry serializes the v1 observation and enforces the fixture's
// bounded payload contract.
func EncodeTelemetry(telemetry Telemetry) ([]byte, error) {
	return telemetrycontract.EncodeTelemetry(telemetry)
}

func validateTopic(topic string) error {
	if topic == "" || strings.ContainsAny(topic, "#+") {
		return errors.New("telemetry topic contains an MQTT wildcard")
	}
	return nil
}
