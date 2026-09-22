package projection

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
)

func TestValidateAcceptedRejectsInvalidValues(t *testing.T) {
	valid := ingestion.AcceptedTelemetry{
		IngestionID:        uuid.New(),
		MessageID:          uuid.New(),
		OrganizationID:     uuid.New(),
		DeviceID:           uuid.New(),
		ObservedAt:         time.Date(2026, 9, 22, 4, 0, 0, 123456789, time.UTC),
		ReceivedAt:         time.Date(2026, 9, 22, 4, 0, 1, 987654321, time.UTC),
		TemperatureCelsius: 23.5,
	}
	if err := validateAccepted(valid); err != nil {
		t.Fatalf("valid telemetry rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ingestion.AcceptedTelemetry)
	}{
		{name: "nil message ID", mutate: func(value *ingestion.AcceptedTelemetry) { value.MessageID = uuid.Nil }},
		{name: "zero observed time", mutate: func(value *ingestion.AcceptedTelemetry) { value.ObservedAt = time.Time{} }},
		{name: "nan temperature", mutate: func(value *ingestion.AcceptedTelemetry) { value.TemperatureCelsius = math.NaN() }},
		{name: "infinite temperature", mutate: func(value *ingestion.AcceptedTelemetry) { value.TemperatureCelsius = math.Inf(1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.mutate(&value)
			if err := validateAccepted(value); err != ErrInvalidInput {
				t.Fatalf("validate error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestSameLogicalObservationNormalizesPostgresTimestampPrecision(t *testing.T) {
	observedAt := time.Date(2026, 9, 22, 4, 0, 0, 123456789, time.UTC)
	if !sameLogicalObservation(observedAt.Truncate(time.Microsecond), 23.5, observedAt, 23.5) {
		t.Fatal("timestamps that differ only below PostgreSQL microsecond precision should be the same logical observation")
	}
	if sameLogicalObservation(observedAt, 23.5, observedAt, 23.500001) {
		t.Fatal("different temperatures should be a logical conflict")
	}
}
