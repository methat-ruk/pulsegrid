package mqttsimulator

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTelemetryTopicAndPayloadContract(t *testing.T) {
	cfg, err := LoadFrom(validEnvironment(EnvironmentDevelopment), t.TempDir(), missingDotenv)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if got, want := cfg.Topic(), "pulsegrid/v1/tenants/pulsegrid-dev/devices/"+testDeviceID+"/telemetry"; got != want {
		t.Fatalf("topic = %q, want %q", got, want)
	}

	observedAt := time.Date(2026, time.September, 18, 11, 0, 0, 123000000, time.FixedZone("ICT", 7*60*60))
	messageID := uuid.MustParse("5edacace-70a7-4a8f-846d-3f4d60c56f3a")
	payload, err := EncodeTelemetry(Telemetry{
		SchemaVersion:      1,
		MessageID:          messageID,
		ObservedAt:         observedAt,
		TemperatureCelsius: 23.5,
	})
	if err != nil {
		t.Fatalf("EncodeTelemetry returned error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	wantKeys := map[string]bool{"schemaVersion": true, "messageId": true, "observedAt": true, "temperatureCelsius": true}
	if len(decoded) != len(wantKeys) {
		t.Fatalf("payload fields = %v, want exactly %v", keys, wantKeys)
	}
	for key := range decoded {
		if !wantKeys[key] {
			t.Fatalf("unexpected payload field %q", key)
		}
	}
	if decoded["schemaVersion"] != float64(1) || decoded["messageId"] != messageID.String() || decoded["observedAt"] != "2026-09-18T04:00:00.123Z" || decoded["temperatureCelsius"] != 23.5 {
		t.Fatalf("payload = %s", payload)
	}
	if len(payload) >= maximumPayloadBytes {
		t.Fatalf("payload size = %d, want < %d", len(payload), maximumPayloadBytes)
	}
}

func TestEncodeTelemetryRejectsInvalidValues(t *testing.T) {
	base := Telemetry{SchemaVersion: 1, MessageID: uuid.New(), ObservedAt: time.Now().UTC(), TemperatureCelsius: 1}
	for name, mutate := range map[string]func(*Telemetry){
		"schema":       func(value *Telemetry) { value.SchemaVersion = 2 },
		"message ID":   func(value *Telemetry) { value.MessageID = uuid.Nil },
		"observed at":  func(value *Telemetry) { value.ObservedAt = time.Time{} },
		"NaN":          func(value *Telemetry) { value.TemperatureCelsius = math.NaN() },
		"positive inf": func(value *Telemetry) { value.TemperatureCelsius = math.Inf(1) },
	} {
		t.Run(name, func(t *testing.T) {
			value := base
			mutate(&value)
			if _, err := EncodeTelemetry(value); err == nil {
				t.Fatal("EncodeTelemetry accepted invalid value")
			}
		})
	}
}
