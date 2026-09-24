//go:build integration

package rules

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

type repositoryFixture struct {
	pool       *pgxpool.Pool
	repository *Repository
	projection *projection.Repository
	registry   *registry.Repository
	orgID      uuid.UUID
	deviceID   uuid.UUID
	foreignOrg uuid.UUID
	foreignDev uuid.UUID
}

func newRepositoryFixture(t *testing.T) *repositoryFixture {
	t.Helper()
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
	registryRepository, err := registry.NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create registry repository: %v", err)
	}
	rulesRepository, err := NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create rules repository: %v", err)
	}
	projectionRepository, err := projection.NewRepositoryWithEvaluator(pool, rulesRepository)
	if err != nil {
		pool.Close()
		t.Fatalf("create projection repository: %v", err)
	}
	orgID, err := registryRepository.CreateOrganization(ctx, "rules-"+uuid.NewString(), "Rules Integration")
	if err != nil {
		pool.Close()
		t.Fatalf("create integration organization: %v", err)
	}
	device, err := registryRepository.CreateDevice(ctx, orgID, registry.CreateDeviceInput{DeviceKey: "rules-device", DisplayName: "Rules Device"})
	if err != nil {
		pool.Close()
		t.Fatalf("create integration device: %v", err)
	}
	foreignOrg, err := registryRepository.CreateOrganization(ctx, "rules-foreign-"+uuid.NewString(), "Foreign Rules")
	if err != nil {
		pool.Close()
		t.Fatalf("create foreign organization: %v", err)
	}
	foreignDevice, err := registryRepository.CreateDevice(ctx, foreignOrg, registry.CreateDeviceInput{DeviceKey: "foreign-device", DisplayName: "Foreign Device"})
	if err != nil {
		pool.Close()
		t.Fatalf("create foreign device: %v", err)
	}
	fixture := &repositoryFixture{
		pool:       pool,
		repository: rulesRepository,
		projection: projectionRepository,
		registry:   registryRepository,
		orgID:      orgID,
		deviceID:   device.ID,
		foreignOrg: foreignOrg,
		foreignDev: foreignDevice.ID,
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM threshold_alerts WHERE device_id = ANY($1)", []uuid.UUID{fixture.deviceID, fixture.foreignDev})
		_, _ = pool.Exec(cleanupContext, "DELETE FROM threshold_rules WHERE device_id = ANY($1)", []uuid.UUID{fixture.deviceID, fixture.foreignDev})
		_, _ = pool.Exec(cleanupContext, "DELETE FROM device_current_state WHERE device_id = ANY($1)", []uuid.UUID{fixture.deviceID, fixture.foreignDev})
		_, _ = pool.Exec(cleanupContext, "DELETE FROM telemetry_observations WHERE device_id = ANY($1)", []uuid.UUID{fixture.deviceID, fixture.foreignDev})
		_, _ = pool.Exec(cleanupContext, "DELETE FROM telemetry_observation_keys WHERE device_id = ANY($1)", []uuid.UUID{fixture.deviceID, fixture.foreignDev})
		_, _ = pool.Exec(cleanupContext, "DELETE FROM devices WHERE id = ANY($1)", []uuid.UUID{fixture.deviceID, fixture.foreignDev})
		_, _ = pool.Exec(cleanupContext, "DELETE FROM organizations WHERE id = ANY($1)", []uuid.UUID{fixture.orgID, fixture.foreignOrg})
		pool.Close()
	})
	return fixture
}

func TestRepositoryRuleMutationLimitAndTenantScope(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	created, err := fixture.repository.CreateRule(ctx, fixture.orgID, CreateRuleInput{
		DeviceID: fixture.deviceID, Comparator: GreaterThan, ThresholdCelsius: 25, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create threshold rule: %v", err)
	}
	if created.Revision != 1 || created.Metric != MetricTemperatureCelsius || created.Comparator != GreaterThan || !created.Enabled {
		t.Fatalf("created rule = %+v", created)
	}
	updated, err := fixture.repository.UpdateRule(ctx, fixture.orgID, UpdateRuleInput{
		ID: created.ID, ExpectedRevision: 1, Comparator: GreaterThanOrEqual, ThresholdCelsius: 30, Enabled: false,
	})
	if err != nil {
		t.Fatalf("update threshold rule: %v", err)
	}
	if updated.Revision != 2 || updated.Enabled || updated.Comparator != GreaterThanOrEqual || updated.ThresholdCelsius != 30 {
		t.Fatalf("updated rule = %+v", updated)
	}
	if _, err := fixture.repository.UpdateRule(ctx, fixture.orgID, UpdateRuleInput{
		ID: created.ID, ExpectedRevision: 1, Comparator: GreaterThan, ThresholdCelsius: 26, Enabled: true,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v, want ErrConflict", err)
	}
	if _, err := fixture.repository.UpdateRule(ctx, fixture.foreignOrg, UpdateRuleInput{
		ID: created.ID, ExpectedRevision: 2, Comparator: GreaterThan, ThresholdCelsius: 26, Enabled: true,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign update error = %v, want ErrNotFound", err)
	}
	foreignRules, err := fixture.repository.ListRules(ctx, fixture.foreignOrg, fixture.deviceID)
	if err != nil || len(foreignRules) != 0 {
		t.Fatalf("foreign rule list = %+v, %v; want empty", foreignRules, err)
	}
	if _, err := fixture.repository.CreateRule(ctx, fixture.orgID, CreateRuleInput{
		DeviceID: fixture.foreignDev, Comparator: GreaterThan, ThresholdCelsius: 25, Enabled: true,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign device create error = %v, want ErrNotFound", err)
	}

	for range MaxRulesPerDevice - 2 {
		if _, err := fixture.repository.CreateRule(ctx, fixture.orgID, CreateRuleInput{
			DeviceID: fixture.deviceID, Comparator: LessThan, ThresholdCelsius: 80, Enabled: false,
		}); err != nil {
			t.Fatalf("fill rule capacity: %v", err)
		}
	}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wait.Go(func() {
			_, createErr := fixture.repository.CreateRule(ctx, fixture.orgID, CreateRuleInput{
				DeviceID: fixture.deviceID, Comparator: LessThan, ThresholdCelsius: 10, Enabled: false,
			})
			results <- createErr
		})
	}
	wait.Wait()
	close(results)
	createdCount := 0
	limitCount := 0
	for createErr := range results {
		switch {
		case createErr == nil:
			createdCount++
		case errors.Is(createErr, ErrRuleLimitReached):
			limitCount++
		default:
			t.Fatalf("concurrent create error = %v", createErr)
		}
	}
	if createdCount != 1 || limitCount != 1 {
		t.Fatalf("concurrent creates: created=%d limit=%d, want one of each", createdCount, limitCount)
	}
	rulesList, err := fixture.repository.ListRules(ctx, fixture.orgID, fixture.deviceID)
	if err != nil || len(rulesList) != MaxRulesPerDevice {
		t.Fatalf("tenant rules = %d, %v; want %d", len(rulesList), err, MaxRulesPerDevice)
	}
}

func TestRepositoryEvaluationSnapshotsDedupesAndPaginatesAlerts(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	ruleA, err := fixture.repository.CreateRule(ctx, fixture.orgID, CreateRuleInput{
		DeviceID: fixture.deviceID, Comparator: GreaterThan, ThresholdCelsius: 25, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule A: %v", err)
	}
	ruleB, err := fixture.repository.CreateRule(ctx, fixture.orgID, CreateRuleInput{
		DeviceID: fixture.deviceID, Comparator: GreaterThanOrEqual, ThresholdCelsius: 30, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule B: %v", err)
	}
	base := time.Date(2026, 9, 24, 5, 0, 0, 0, time.UTC)
	firstMessage := uuid.New()
	first := accepted(fixture, firstMessage, base, base.Add(time.Second), 30)
	if err := fixture.projection.Consume(ctx, accepted(fixture, uuid.New(), base.Add(time.Minute), base.Add(2*time.Second), 25)); err != nil {
		t.Fatalf("persist comparator boundary non-match: %v", err)
	}
	if err := fixture.projection.Consume(ctx, first); err != nil {
		t.Fatalf("persist matching observation: %v", err)
	}
	replay := first
	replay.IngestionID = uuid.New()
	replay.ReceivedAt = base.Add(10 * time.Second)
	if err := fixture.projection.Consume(ctx, replay); err != nil {
		t.Fatalf("persist exact replay: %v", err)
	}
	if _, err := fixture.repository.UpdateRule(ctx, fixture.orgID, UpdateRuleInput{
		ID: ruleA.ID, ExpectedRevision: 1, Comparator: GreaterThan, ThresholdCelsius: 35, Enabled: true,
	}); err != nil {
		t.Fatalf("edit rule A: %v", err)
	}
	if _, err := fixture.repository.UpdateRule(ctx, fixture.orgID, UpdateRuleInput{
		ID: ruleB.ID, ExpectedRevision: 1, Comparator: GreaterThanOrEqual, ThresholdCelsius: 30, Enabled: false,
	}); err != nil {
		t.Fatalf("disable rule B: %v", err)
	}
	newestMessage := uuid.New()
	if err := fixture.projection.Consume(ctx, accepted(fixture, newestMessage, base.Add(2*time.Minute), base.Add(3*time.Second), 30)); err != nil {
		t.Fatalf("persist post-edit non-match: %v", err)
	}
	if err := fixture.projection.Consume(ctx, accepted(fixture, uuid.New(), base.Add(-time.Minute), base.Add(4*time.Second), 36)); err != nil {
		t.Fatalf("persist late matching observation: %v", err)
	}

	var alertCount int
	if err := fixture.pool.QueryRow(ctx, "SELECT count(*) FROM threshold_alerts WHERE device_id = $1", fixture.deviceID).Scan(&alertCount); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	if alertCount != 3 {
		t.Fatalf("alert count = %d, want three independent rule matches", alertCount)
	}
	var oldSnapshot Alert
	oldSnapshot, err = scanAlert(fixture.pool.QueryRow(ctx, alertSelect+" WHERE d.organization_id = $1 AND a.rule_id = $2 AND a.message_id = $3", fixture.orgID, ruleA.ID, firstMessage))
	if err != nil {
		t.Fatalf("read old alert snapshot: %v", err)
	}
	if oldSnapshot.ThresholdCelsius != 25 || oldSnapshot.TemperatureCelsius != 30 || oldSnapshot.Comparator != GreaterThan {
		t.Fatalf("alert snapshot changed after rule edit: %+v", oldSnapshot)
	}
	if _, err := fixture.pool.Exec(ctx, "DELETE FROM telemetry_observations WHERE device_id = $1 AND message_id = $2", fixture.deviceID, firstMessage); err != nil {
		t.Fatalf("prune source telemetry history row: %v", err)
	}
	if _, err := fixture.repository.GetAlert(ctx, fixture.orgID, oldSnapshot.ID); err != nil {
		t.Fatalf("alert detail depended on pruned history: %v", err)
	}
	if _, err := fixture.repository.GetAlert(ctx, fixture.foreignOrg, oldSnapshot.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign alert read error = %v, want ErrNotFound", err)
	}
	foreignPage, err := fixture.repository.ListAlerts(ctx, fixture.foreignOrg, 10, nil, nil)
	if err != nil || len(foreignPage.Alerts) != 0 {
		t.Fatalf("foreign alert page = %+v, %v; want empty", foreignPage, err)
	}

	var paged []Alert
	var cursor *AlertCursor
	for range 4 {
		page, pageErr := fixture.repository.ListAlerts(ctx, fixture.orgID, 1, nil, cursor)
		if pageErr != nil {
			t.Fatalf("list alert page: %v", pageErr)
		}
		paged = append(paged, page.Alerts...)
		cursor = page.NextCursor
		if cursor == nil {
			break
		}
	}
	if len(paged) != 3 {
		t.Fatalf("paged alerts = %d, want 3", len(paged))
	}
	filteredCursor := *cursorFor(fixture.orgID, fixture.deviceID)
	if _, err := fixture.repository.ListAlerts(ctx, fixture.orgID, 1, nil, &filteredCursor); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cursor with a different device filter error = %v, want ErrInvalidInput", err)
	}
	if _, err := fixture.repository.ListAlerts(ctx, fixture.foreignOrg, 1, nil, pagedCursorFor(fixture.orgID, paged[len(paged)-1])); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cursor from another organization error = %v, want ErrInvalidInput", err)
	}
}

func accepted(fixture *repositoryFixture, messageID uuid.UUID, observedAt, receivedAt time.Time, temperature float64) ingestion.AcceptedTelemetry {
	return ingestion.AcceptedTelemetry{
		IngestionID: uuid.New(), MessageID: messageID, OrganizationID: fixture.orgID,
		DeviceID: fixture.deviceID, ObservedAt: observedAt, ReceivedAt: receivedAt,
		TemperatureCelsius: temperature,
	}
}

func cursorFor(organizationID, deviceID uuid.UUID) *AlertCursor {
	return &AlertCursor{CreatedAt: time.Now().UTC(), ID: uuid.New(), OrganizationID: organizationID, DeviceFilter: &deviceID}
}

func pagedCursorFor(organizationID uuid.UUID, alert Alert) *AlertCursor {
	return &AlertCursor{CreatedAt: alert.CreatedAt, ID: alert.ID, OrganizationID: organizationID}
}
