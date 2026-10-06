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
	messageIDB := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	if err := projectionRepository.Consume(ctx, ingestion.AcceptedTelemetry{
		IngestionID: uuid.New(), MessageID: messageIDB, OrganizationID: organizationB,
		DeviceID: deviceB.ID, ObservedAt: observedAt.Add(time.Minute),
		ReceivedAt: observedAt.Add(time.Minute + time.Second), TemperatureCelsius: 18.25,
	}); err != nil {
		t.Fatalf("persist telemetry B: %v", err)
	}

	handler, err := NewHandlerWithTelemetry(registryRepository, projectionRepository, organizationA, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create GraphQL handler: %v", err)
	}
	response := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { deviceCurrentState(deviceId: \"`+deviceA.ID.String()+`\") { messageId temperatureCelsius lastSeenAt } deviceTelemetry(deviceId: \"`+deviceA.ID.String()+`\") { edges { node { messageId temperatureCelsius } } pageInfo { hasNextPage } } }" }`)
	if len(response.Errors) != 0 || !strings.Contains(string(response.Data), "29.75") {
		t.Fatalf("tenant A telemetry response = %+v", response)
	}
	type scopedTelemetry struct {
		DeviceCurrentState *struct {
			MessageID string `json:"messageId"`
		} `json:"deviceCurrentState"`
		DeviceTelemetry struct {
			Edges []struct {
				Node struct {
					MessageID string `json:"messageId"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"deviceTelemetry"`
	}

	crossTenant := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { deviceCurrentState(deviceId: \"`+deviceB.ID.String()+`\") { messageId } deviceTelemetry(deviceId: \"`+deviceB.ID.String()+`\") { edges { node { messageId } } } }" }`)
	if len(crossTenant.Errors) != 0 {
		t.Fatalf("cross-tenant telemetry response has errors: %+v", crossTenant.Errors)
	}
	var isolatedTelemetry scopedTelemetry
	if err := json.Unmarshal(crossTenant.Data, &isolatedTelemetry); err != nil {
		t.Fatalf("decode A-to-B telemetry response: %v", err)
	}
	if isolatedTelemetry.DeviceCurrentState != nil || len(isolatedTelemetry.DeviceTelemetry.Edges) != 0 {
		t.Fatalf("tenant A received tenant B telemetry: %+v", isolatedTelemetry)
	}
	handlerB, err := NewHandlerWithTelemetry(registryRepository, projectionRepository, organizationB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create tenant B GraphQL handler: %v", err)
	}
	ownedTelemetry := doGraphQLForOrganization(t, handlerB, organizationB, `{ "query": "query { deviceCurrentState(deviceId: \"`+deviceB.ID.String()+`\") { messageId temperatureCelsius } deviceTelemetry(deviceId: \"`+deviceB.ID.String()+`\") { edges { node { messageId temperatureCelsius } } pageInfo { hasNextPage } } }" }`)
	if len(ownedTelemetry.Errors) != 0 || !strings.Contains(string(ownedTelemetry.Data), messageIDB.String()) || !strings.Contains(string(ownedTelemetry.Data), "18.25") {
		t.Fatalf("tenant B telemetry response = %+v", ownedTelemetry)
	}
	foreignTelemetry := doGraphQLForOrganization(t, handlerB, organizationB, `{ "query": "query { deviceCurrentState(deviceId: \"`+deviceA.ID.String()+`\") { messageId } deviceTelemetry(deviceId: \"`+deviceA.ID.String()+`\") { edges { node { messageId } } } }" }`)
	if len(foreignTelemetry.Errors) != 0 {
		t.Fatalf("tenant B cross-tenant telemetry response has errors: %+v", foreignTelemetry.Errors)
	}
	var reverseIsolatedTelemetry scopedTelemetry
	if err := json.Unmarshal(foreignTelemetry.Data, &reverseIsolatedTelemetry); err != nil {
		t.Fatalf("decode B-to-A telemetry response: %v", err)
	}
	if reverseIsolatedTelemetry.DeviceCurrentState != nil || len(reverseIsolatedTelemetry.DeviceTelemetry.Edges) != 0 {
		t.Fatalf("tenant B received tenant A telemetry: %+v", reverseIsolatedTelemetry)
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
