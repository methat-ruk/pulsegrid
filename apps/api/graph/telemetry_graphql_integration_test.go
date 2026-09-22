//go:build integration

package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

func TestGraphQLTelemetryUsesRealPostgresAndTenantScope(t *testing.T) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	if databaseConfiguration.Environment != config.Test {
		t.Fatalf("integration environment = %q, want test", databaseConfiguration.Environment)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	defer pool.Close()
	registryRepository, err := registry.NewRepository(pool)
	if err != nil {
		t.Fatalf("create registry repository: %v", err)
	}
	projectionRepository, err := projection.NewRepository(pool)
	if err != nil {
		t.Fatalf("create projection repository: %v", err)
	}
	organizationA, err := registryRepository.CreateOrganization(ctx, "telemetry-graphql-a-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "Telemetry GraphQL A")
	if err != nil {
		t.Fatalf("create organization A: %v", err)
	}
	organizationB, err := registryRepository.CreateOrganization(ctx, "telemetry-graphql-b-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "Telemetry GraphQL B")
	if err != nil {
		t.Fatalf("create organization B: %v", err)
	}
	deviceA, err := registryRepository.CreateDevice(ctx, organizationA, registry.CreateDeviceInput{DeviceKey: "telemetry-a", DisplayName: "Telemetry A"})
	if err != nil {
		t.Fatalf("create device A: %v", err)
	}
	deviceB, err := registryRepository.CreateDevice(ctx, organizationB, registry.CreateDeviceInput{DeviceKey: "telemetry-b", DisplayName: "Telemetry B"})
	if err != nil {
		t.Fatalf("create device B: %v", err)
	}
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM device_current_state WHERE device_id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM telemetry_observations WHERE device_id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM devices WHERE id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM organizations WHERE id IN ($1, $2)", organizationA, organizationB)
	}
	defer cleanup()

	observedAt := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	if err := projectionRepository.Consume(ctx, ingestion.AcceptedTelemetry{
		IngestionID:        uuid.New(),
		MessageID:          uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		OrganizationID:     organizationA,
		DeviceID:           deviceA.ID,
		ObservedAt:         observedAt,
		ReceivedAt:         observedAt.Add(time.Second),
		TemperatureCelsius: 29.75,
	}); err != nil {
		t.Fatalf("persist telemetry A: %v", err)
	}

	handler, err := NewHandlerWithTelemetry(registryRepository, projectionRepository, organizationA, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create GraphQL handler: %v", err)
	}
	response := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { deviceCurrentState(deviceId: \"`+deviceA.ID.String()+`\") { messageId temperatureCelsius lastSeenAt } deviceTelemetry(deviceId: \"`+deviceA.ID.String()+`\") { edges { node { messageId temperatureCelsius } } pageInfo { hasNextPage } } }" }`)
	if len(response.Errors) != 0 || !strings.Contains(string(response.Data), "29.75") {
		t.Fatalf("tenant A telemetry response = %+v", response)
	}

	crossTenant := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { deviceCurrentState(deviceId: \"`+deviceB.ID.String()+`\") { messageId } deviceTelemetry(deviceId: \"`+deviceB.ID.String()+`\") { edges { node { messageId } } } }" }`)
	if len(crossTenant.Errors) != 0 || strings.Contains(string(crossTenant.Data), "55555555-5555-4555-8555-555555555555") {
		t.Fatalf("cross-tenant telemetry response = %+v", crossTenant)
	}
}

func doGraphQLForOrganization(t *testing.T, handler http.Handler, organizationID uuid.UUID, body string) graphqlResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", bytes.NewBufferString(body))
	request = request.WithContext(WithRequestContext(request.Context(), "integration-telemetry-request", Principal{OrganizationID: organizationID}))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GraphQL status = %d, body = %s", response.Code, response.Body.String())
	}
	var parsed graphqlResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode GraphQL response: %v", err)
	}
	return parsed
}
