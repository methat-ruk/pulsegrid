//go:build integration

package projection

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
)

type integrationFixture struct {
	pool       *pgxpool.Pool
	projection *Repository
	registry   *registry.Repository
	orgID      uuid.UUID
	deviceID   uuid.UUID
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
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
	projectionRepository, err := NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create projection repository: %v", err)
	}
	orgID, err := registryRepository.CreateOrganization(ctx, "projection-"+uuid.NewString(), "Projection Integration")
	if err != nil {
		pool.Close()
		t.Fatalf("create integration organization: %v", err)
	}
	device, err := registryRepository.CreateDevice(ctx, orgID, registry.CreateDeviceInput{DeviceKey: "projection-device", DisplayName: "Projection Device"})
	if err != nil {
		pool.Close()
		t.Fatalf("create integration device: %v", err)
	}
	fixture := &integrationFixture{pool: pool, projection: projectionRepository, registry: registryRepository, orgID: orgID, deviceID: device.ID}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM device_current_state WHERE device_id = $1", fixture.deviceID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM telemetry_observations WHERE device_id = $1", fixture.deviceID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM devices WHERE id = $1", fixture.deviceID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM organizations WHERE id = $1", fixture.orgID)
		pool.Close()
	})
	return fixture
}

func acceptedTelemetry(fixture *integrationFixture, messageID uuid.UUID, observedAt, receivedAt time.Time, temperature float64) ingestion.AcceptedTelemetry {
	return ingestion.AcceptedTelemetry{
		IngestionID:        uuid.New(),
		MessageID:          messageID,
		OrganizationID:     fixture.orgID,
		DeviceID:           fixture.deviceID,
		ObservedAt:         observedAt,
		ReceivedAt:         receivedAt,
		TemperatureCelsius: temperature,
	}
}

func TestProjectionPersistsLateReplayAndConflictSemantics(t *testing.T) {
	fixture := newIntegrationFixture(t)
	base := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	firstID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	newestID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	lateID := uuid.MustParse("33333333-3333-4333-8333-333333333333")

	if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(fixture, firstID, base, base.Add(time.Second), 20)); err != nil {
		t.Fatalf("persist first telemetry: %v", err)
	}
	if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(fixture, newestID, base.Add(10*time.Minute), base.Add(2*time.Second), 30)); err != nil {
		t.Fatalf("persist newest telemetry: %v", err)
	}
	if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(fixture, lateID, base.Add(5*time.Minute), base.Add(3*time.Second), 25)); err != nil {
		t.Fatalf("persist late telemetry: %v", err)
	}

	state, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read current state: %v", err)
	}
	if state.MessageID != newestID || state.TemperatureCelsius != 30 {
		t.Fatalf("current state = %+v, want newest observation", state)
	}
	if !state.LastSeenAt.Equal(base.Add(3 * time.Second)) {
		t.Fatalf("last seen = %s, want %s", state.LastSeenAt, base.Add(3*time.Second))
	}

	page, err := fixture.projection.ListTelemetry(context.Background(), fixture.orgID, fixture.deviceID, 100, nil)
	if err != nil {
		t.Fatalf("list telemetry: %v", err)
	}
	if len(page.Points) != 3 || page.Points[0].MessageID != newestID || page.Points[1].MessageID != lateID || page.Points[2].MessageID != firstID {
		t.Fatalf("telemetry page = %+v, want observed-time descending order", page)
	}

	replay := acceptedTelemetry(fixture, newestID, base.Add(10*time.Minute).Add(999*time.Nanosecond), base.Add(4*time.Second), 30)
	replay.MQTTDuplicate = true
	if err := fixture.projection.Consume(context.Background(), replay); err != nil {
		t.Fatalf("exact replay returned error: %v", err)
	}
	replayedState, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read state after replay: %v", err)
	}
	if !replayedState.LastSeenAt.Equal(state.LastSeenAt) || replayedState.MessageID != state.MessageID {
		t.Fatalf("state after replay = %+v, want unchanged state %+v", replayedState, state)
	}

	conflict := acceptedTelemetry(fixture, newestID, base.Add(10*time.Minute), base.Add(5*time.Second), 31)
	if err := fixture.projection.Consume(context.Background(), conflict); !errors.Is(err, ErrMessageConflict) {
		t.Fatalf("conflicting replay error = %v, want ErrMessageConflict", err)
	}
	var count int
	if err := fixture.pool.QueryRow(context.Background(), "SELECT count(*) FROM telemetry_observations WHERE device_id = $1", fixture.deviceID).Scan(&count); err != nil {
		t.Fatalf("count telemetry rows: %v", err)
	}
	if count != 3 {
		t.Fatalf("telemetry row count after replay/conflict = %d, want 3", count)
	}
}

func TestProjectionEqualObservedAtUsesMessageIDTieBreak(t *testing.T) {
	fixture := newIntegrationFixture(t)
	observedAt := time.Date(2026, 9, 22, 5, 0, 0, 0, time.UTC)
	lowID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	highID := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(fixture, highID, observedAt, observedAt, 90)); err != nil {
		t.Fatalf("persist high tie-break telemetry: %v", err)
	}
	if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(fixture, lowID, observedAt, observedAt.Add(time.Second), 10)); err != nil {
		t.Fatalf("persist low tie-break telemetry: %v", err)
	}
	state, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read equal-time current state: %v", err)
	}
	if state.MessageID != highID || state.TemperatureCelsius != 90 {
		t.Fatalf("equal-time state = %+v, want lexicographically greatest message ID", state)
	}
}

func TestProjectionConcurrentExactReplayCreatesOneRow(t *testing.T) {
	fixture := newIntegrationFixture(t)
	messageID := uuid.New()
	base := time.Date(2026, 9, 22, 6, 0, 0, 0, time.UTC)
	values := []ingestion.AcceptedTelemetry{
		acceptedTelemetry(fixture, messageID, base, base.Add(time.Second), 42),
		acceptedTelemetry(fixture, messageID, base, base.Add(2*time.Second), 42),
	}
	errorsCh := make(chan error, len(values))
	var waitGroup sync.WaitGroup
	for _, value := range values {
		waitGroup.Go(func() {
			errorsCh <- fixture.projection.Consume(context.Background(), value)
		})
	}
	waitGroup.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent exact replay error = %v", err)
		}
	}
	var count int
	if err := fixture.pool.QueryRow(context.Background(), "SELECT count(*) FROM telemetry_observations WHERE device_id = $1", fixture.deviceID).Scan(&count); err != nil {
		t.Fatalf("count concurrent replay rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("concurrent replay row count = %d, want 1", count)
	}
}

func TestProjectionRetentionBoundPreservesCurrentObservation(t *testing.T) {
	fixture := newIntegrationFixture(t)
	base := time.Date(2026, 9, 22, 7, 0, 0, 0, time.UTC)
	for index := range MaxHistorySize + 1 {
		value := acceptedTelemetry(
			fixture,
			uuid.New(),
			base.Add(time.Duration(index)*time.Second),
			base.Add(time.Duration(index)*time.Second),
			float64(index),
		)
		if err := fixture.projection.Consume(context.Background(), value); err != nil {
			t.Fatalf("persist retention row %d: %v", index, err)
		}
	}
	var count int
	if err := fixture.pool.QueryRow(context.Background(), "SELECT count(*) FROM telemetry_observations WHERE device_id = $1", fixture.deviceID).Scan(&count); err != nil {
		t.Fatalf("count retained rows: %v", err)
	}
	if count != MaxHistorySize {
		t.Fatalf("retained row count = %d, want %d", count, MaxHistorySize)
	}
	state, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read retained current state: %v", err)
	}
	if state.TemperatureCelsius != float64(MaxHistorySize) {
		t.Fatalf("retained current temperature = %v, want %d", state.TemperatureCelsius, MaxHistorySize)
	}
	var currentExists bool
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM telemetry_observations AS t
			JOIN device_current_state AS cs ON cs.observation_sequence = t.storage_sequence
			WHERE t.device_id = $1
		)
	`, fixture.deviceID).Scan(&currentExists); err != nil {
		t.Fatalf("verify retained current source: %v", err)
	}
	if !currentExists {
		t.Fatal("current-state source was pruned")
	}
}

func TestProjectionTenantScopeRejectsForeignWriteAndRead(t *testing.T) {
	fixture := newIntegrationFixture(t)
	foreignOrganization, err := fixture.registry.CreateOrganization(context.Background(), "projection-foreign-"+uuid.NewString(), "Foreign Projection")
	if err != nil {
		t.Fatalf("create foreign organization: %v", err)
	}
	defer func() {
		_, _ = fixture.pool.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", foreignOrganization)
	}()
	value := acceptedTelemetry(fixture, uuid.New(), time.Now().UTC(), time.Now().UTC(), 1)
	value.OrganizationID = foreignOrganization
	if err := fixture.projection.Consume(context.Background(), value); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign write error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.projection.GetCurrentState(context.Background(), foreignOrganization, fixture.deviceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign read error = %v, want ErrNotFound", err)
	}
	page, err := fixture.projection.ListTelemetry(context.Background(), foreignOrganization, fixture.deviceID, DefaultPageSize, nil)
	if err != nil {
		t.Fatalf("foreign history read error = %v", err)
	}
	if len(page.Points) != 0 {
		t.Fatalf("foreign history returned %d points, want empty", len(page.Points))
	}
}
