//go:build integration

package graph

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/rules"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

func TestGraphQLThresholdRulesAndAlertsUseRealPostgres(t *testing.T) {
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
	rulesRepository, err := rules.NewRepository(pool)
	if err != nil {
		t.Fatalf("create threshold rules repository: %v", err)
	}
	projectionRepository, err := projection.NewRepositoryWithEvaluator(pool, rulesRepository)
	if err != nil {
		t.Fatalf("create telemetry projection repository: %v", err)
	}
	organizationA, err := registryRepository.CreateOrganization(ctx, "rules-graphql-a-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "Rules GraphQL A")
	if err != nil {
		t.Fatalf("create organization A: %v", err)
	}
	organizationB, err := registryRepository.CreateOrganization(ctx, "rules-graphql-b-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "Rules GraphQL B")
	if err != nil {
		t.Fatalf("create organization B: %v", err)
	}
	deviceA, err := registryRepository.CreateDevice(ctx, organizationA, registry.CreateDeviceInput{DeviceKey: "rules-a", DisplayName: "Rules A"})
	if err != nil {
		t.Fatalf("create device A: %v", err)
	}
	deviceB, err := registryRepository.CreateDevice(ctx, organizationB, registry.CreateDeviceInput{DeviceKey: "rules-b", DisplayName: "Rules B"})
	if err != nil {
		t.Fatalf("create device B: %v", err)
	}
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM threshold_alerts WHERE device_id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM threshold_rules WHERE device_id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM device_current_state WHERE device_id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM telemetry_observations WHERE device_id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM telemetry_observation_keys WHERE device_id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM devices WHERE id IN ($1, $2)", deviceA.ID, deviceB.ID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM organizations WHERE id IN ($1, $2)", organizationA, organizationB)
	}()

	handler, err := NewHandlerWithRules(registryRepository, projectionRepository, rulesRepository, organizationA, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create GraphQL handler: %v", err)
	}
	createdRule := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "mutation { createThresholdRule(input: { deviceId: \"`+deviceA.ID.String()+`\", comparator: GT, thresholdCelsius: 25 }) { id enabled revision } }" }`)
	if len(createdRule.Errors) != 0 {
		t.Fatalf("create rule GraphQL response = %+v", createdRule)
	}

	observedAt := time.Date(2026, 9, 24, 10, 0, 0, 123456789, time.UTC)
	messageID := uuid.New()
	accepted := ingestion.AcceptedTelemetry{
		IngestionID: uuid.New(), MessageID: messageID, OrganizationID: organizationA,
		DeviceID: deviceA.ID, ObservedAt: observedAt, ReceivedAt: observedAt.Add(time.Second),
		TemperatureCelsius: 31,
	}
	if err := projectionRepository.Consume(ctx, accepted); err != nil {
		t.Fatalf("persist matching observation: %v", err)
	}
	if err := projectionRepository.Consume(ctx, accepted); err != nil {
		t.Fatalf("replay matching observation: %v", err)
	}
	if err := projectionRepository.Consume(ctx, ingestion.AcceptedTelemetry{
		IngestionID: uuid.New(), MessageID: uuid.New(), OrganizationID: organizationA,
		DeviceID: deviceA.ID, ObservedAt: observedAt.Add(time.Minute), ReceivedAt: observedAt.Add(2 * time.Second),
		TemperatureCelsius: 10,
	}); err != nil {
		t.Fatalf("persist newer non-matching observation: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM telemetry_observations WHERE device_id = $1 AND message_id = $2", deviceA.ID, messageID); err != nil {
		t.Fatalf("prune triggering history row: %v", err)
	}

	alertResponse := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { alerts { edges { node { id deviceId ruleId messageId observedAt receivedAt temperatureCelsius metric comparator thresholdCelsius createdAt } } pageInfo { hasNextPage } } }" }`)
	if len(alertResponse.Errors) != 0 {
		t.Fatalf("alerts GraphQL response = %+v", alertResponse)
	}
	var alertData struct {
		Alerts struct {
			Edges []struct {
				Node struct {
					ID                 string  `json:"id"`
					DeviceID           string  `json:"deviceId"`
					MessageID          string  `json:"messageId"`
					ObservedAt         string  `json:"observedAt"`
					TemperatureCelsius float64 `json:"temperatureCelsius"`
					Metric             string  `json:"metric"`
					Comparator         string  `json:"comparator"`
					ThresholdCelsius   float64 `json:"thresholdCelsius"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(alertResponse.Data, &alertData); err != nil {
		t.Fatalf("decode alerts response: %v", err)
	}
	if len(alertData.Alerts.Edges) != 1 {
		t.Fatalf("alert count = %d, want one despite replay", len(alertData.Alerts.Edges))
	}
	alert := alertData.Alerts.Edges[0].Node
	if alert.DeviceID != deviceA.ID.String() || alert.MessageID != messageID.String() || alert.TemperatureCelsius != 31 || alert.Metric != rules.MetricTemperatureCelsius || alert.Comparator != string(rules.GreaterThan) || alert.ThresholdCelsius != 25 {
		t.Fatalf("alert snapshot = %+v", alert)
	}

	foreignRule, err := rulesRepository.CreateRule(ctx, organizationB, rules.CreateRuleInput{
		DeviceID: deviceB.ID, Comparator: rules.GreaterThan, ThresholdCelsius: 20, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create foreign rule: %v", err)
	}
	foreignRuleCreate := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "mutation { createThresholdRule(input: { deviceId: \"`+deviceB.ID.String()+`\", comparator: GT, thresholdCelsius: 25 }) { id } }" }`)
	assertErrorCode(t, foreignRuleCreate, errorCodeBadUserInput)
	foreignRuleUpdate := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "mutation { updateThresholdRule(input: { id: \"`+foreignRule.ID.String()+`\", expectedRevision: 1, comparator: GT, thresholdCelsius: 25, enabled: true }) { id } }" }`)
	assertErrorCode(t, foreignRuleUpdate, errorCodeBadUserInput)
	foreignMessageID := uuid.New()
	if err := projectionRepository.Consume(ctx, ingestion.AcceptedTelemetry{
		IngestionID: uuid.New(), MessageID: foreignMessageID, OrganizationID: organizationB,
		DeviceID: deviceB.ID, ObservedAt: observedAt, ReceivedAt: observedAt.Add(time.Second),
		TemperatureCelsius: 30,
	}); err != nil {
		t.Fatalf("persist foreign matching observation: %v", err)
	}
	var foreignAlertID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM threshold_alerts WHERE rule_id = $1 AND message_id = $2", foreignRule.ID, foreignMessageID).Scan(&foreignAlertID); err != nil {
		t.Fatalf("read foreign alert identity: %v", err)
	}
	crossTenant := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { thresholdRules(deviceId: \"`+deviceB.ID.String()+`\") { id } alert(id: \"`+foreignAlertID.String()+`\") { id } alerts(first: 10, deviceId: \"`+deviceB.ID.String()+`\") { edges { node { id } } } }" }`)
	if len(crossTenant.Errors) != 0 {
		t.Fatalf("cross-tenant rule/alert response has errors: %+v", crossTenant.Errors)
	}
	var isolated struct {
		ThresholdRules []struct {
			ID string `json:"id"`
		} `json:"thresholdRules"`
		Alert *struct {
			ID string `json:"id"`
		} `json:"alert"`
		Alerts struct {
			Edges []struct {
				Node struct {
					ID string `json:"id"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(crossTenant.Data, &isolated); err != nil {
		t.Fatalf("decode cross-tenant rule/alert response: %v", err)
	}
	if len(isolated.ThresholdRules) != 0 || isolated.Alert != nil || len(isolated.Alerts.Edges) != 0 {
		t.Fatalf("cross-tenant rule/alert data was visible: %+v", isolated)
	}
}
