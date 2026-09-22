package main

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

func TestStartupFailureDetailsClassifySafeActionableMessages(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
		wantMsg  string
	}{
		{
			name:     "missing organization",
			err:      classifyOrganizationLookupFailure(registry.ErrNotFound),
			wantCode: startupOrganizationMissing,
			wantMsg:  "seeded pulsegrid-dev organization is missing; run the development seed command",
		},
		{
			name:     "missing schema",
			err:      classifyOrganizationLookupFailure(&pgconn.PgError{Code: "42P01", Message: "relation organizations does not exist"}),
			wantCode: startupDatabaseSchemaUnavailable,
			wantMsg:  "development database schema is unavailable; run migrations",
		},
		{
			name:     "database unavailable",
			err:      classifyOrganizationLookupFailure(errors.New("connection refused")),
			wantCode: startupDatabaseUnavailable,
			wantMsg:  "development database is unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotCode, gotMessage := startupFailureDetails(test.err)
			if gotCode != test.wantCode || gotMessage != test.wantMsg {
				t.Fatalf("startup failure details = (%q, %q), want (%q, %q)", gotCode, gotMessage, test.wantCode, test.wantMsg)
			}
		})
	}
}

func TestStartupFailureDetailsDoNotExposeUnderlyingError(t *testing.T) {
	secretCause := errors.New("dial postgres://pulsegrid:super-secret@127.0.0.1:5432/pulsegrid_dev")
	failure := newStartupFailure(startupDatabaseUnavailable, "development database is unavailable", secretCause)
	_, message := startupFailureDetails(failure)
	if message != "development database is unavailable" {
		t.Fatalf("startup message = %q, want safe message", message)
	}
}

func TestClassifyTelemetrySchemaFailureUsesSafeSchemaAction(t *testing.T) {
	failure := classifyTelemetrySchemaFailure(projection.ErrSchemaUnavailable)
	code, message := startupFailureDetails(failure)
	if code != startupDatabaseSchemaUnavailable || message != "development database schema is unavailable; run migrations" {
		t.Fatalf("telemetry schema failure details = (%q, %q)", code, message)
	}
}
