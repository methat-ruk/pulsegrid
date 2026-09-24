//go:build integration

package projection

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

type fakeRuleEvaluator struct {
	callCount atomic.Int32
	err       error
}

func (f *fakeRuleEvaluator) Evaluate(context.Context, pgx.Tx, ingestion.AcceptedTelemetry) error {
	f.callCount.Add(1)
	return f.err
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
		_, _ = pool.Exec(cleanupContext, "DELETE FROM threshold_alerts WHERE device_id = $1", fixture.deviceID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM threshold_rules WHERE device_id = $1", fixture.deviceID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM device_current_state WHERE device_id = $1", fixture.deviceID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM telemetry_observations WHERE device_id = $1", fixture.deviceID)
		_, _ = pool.Exec(cleanupContext, "DELETE FROM telemetry_observation_keys WHERE device_id = $1", fixture.deviceID)
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

func TestProjectionRuleFailureRollsBackTelemetryAndState(t *testing.T) {
	fixture := newIntegrationFixture(t)
	evaluationFailure := errors.New("rule storage is unavailable")
	evaluator := &fakeRuleEvaluator{err: evaluationFailure}
	consumer, err := NewRepositoryWithEvaluator(fixture.pool, evaluator)
	if err != nil {
		t.Fatalf("create projection with rule evaluator: %v", err)
	}
	messageID := uuid.New()
	observedAt := time.Date(2026, 9, 24, 7, 0, 0, 0, time.UTC)
	if err := consumer.Consume(context.Background(), acceptedTelemetry(fixture, messageID, observedAt, observedAt.Add(time.Second), 31)); !errors.Is(err, evaluationFailure) {
		t.Fatalf("Consume error = %v, want evaluation failure", err)
	}
	for _, table := range []string{"telemetry_observation_keys", "telemetry_observations", "device_current_state"} {
		var count int
		query := "SELECT count(*) FROM " + table + " WHERE device_id = $1"
		if err := fixture.pool.QueryRow(context.Background(), query, fixture.deviceID).Scan(&count); err != nil {
			t.Fatalf("count %s after rollback: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count after rule failure = %d, want 0", table, count)
		}
	}
	if evaluator.callCount.Load() != 1 {
		t.Fatalf("rule evaluator calls = %d, want 1", evaluator.callCount.Load())
	}
}

func TestProjectionExactReplaySkipsRuleEvaluation(t *testing.T) {
	fixture := newIntegrationFixture(t)
	evaluator := &fakeRuleEvaluator{}
	consumer, err := NewRepositoryWithEvaluator(fixture.pool, evaluator)
	if err != nil {
		t.Fatalf("create projection with rule evaluator: %v", err)
	}
	observedAt := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	accepted := acceptedTelemetry(fixture, uuid.New(), observedAt, observedAt.Add(time.Second), 31)
	if err := consumer.Consume(context.Background(), accepted); err != nil {
		t.Fatalf("consume initial observation: %v", err)
	}
	replay := accepted
	replay.IngestionID = uuid.New()
	replay.ReceivedAt = observedAt.Add(5 * time.Second)
	if err := consumer.Consume(context.Background(), replay); err != nil {
		t.Fatalf("consume exact replay: %v", err)
	}
	if evaluator.callCount.Load() != 1 {
		t.Fatalf("rule evaluator calls = %d, want one for initial observation only", evaluator.callCount.Load())
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

func TestProjectionConcurrentConflictingReuseKeepsOneCanonicalObservation(t *testing.T) {
	fixture := newIntegrationFixture(t)
	messageID := uuid.New()
	base := time.Date(2026, 9, 22, 6, 30, 0, 0, time.UTC)
	values := []ingestion.AcceptedTelemetry{
		acceptedTelemetry(fixture, messageID, base, base.Add(time.Second), 41),
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

	var acceptedCount, conflictCount int
	for err := range errorsCh {
		switch {
		case err == nil:
			acceptedCount++
		case errors.Is(err, ErrMessageConflict):
			conflictCount++
		default:
			t.Fatalf("concurrent conflicting reuse error = %v", err)
		}
	}
	if acceptedCount != 1 || conflictCount != 1 {
		t.Fatalf("concurrent conflict outcomes = accepted:%d conflict:%d, want one each", acceptedCount, conflictCount)
	}

	var count int
	if err := fixture.pool.QueryRow(context.Background(), "SELECT count(*) FROM telemetry_observations WHERE device_id = $1", fixture.deviceID).Scan(&count); err != nil {
		t.Fatalf("count concurrent conflicting rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("concurrent conflicting row count = %d, want 1", count)
	}
	state, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read state after concurrent conflict: %v", err)
	}
	if state.MessageID != messageID || (state.TemperatureCelsius != 41 && state.TemperatureCelsius != 42) {
		t.Fatalf("state after concurrent conflict = %+v", state)
	}
}

func TestProjectionConcurrentDistinctMessagesSelectMaximumTuple(t *testing.T) {
	fixture := newIntegrationFixture(t)
	observedAt := time.Date(2026, 9, 22, 6, 45, 0, 0, time.UTC)
	lowID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	highID := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	values := []ingestion.AcceptedTelemetry{
		acceptedTelemetry(fixture, lowID, observedAt, observedAt.Add(time.Second), 11),
		acceptedTelemetry(fixture, highID, observedAt, observedAt.Add(2*time.Second), 99),
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
			t.Fatalf("concurrent distinct telemetry error = %v", err)
		}
	}
	state, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read state after concurrent distinct writes: %v", err)
	}
	if state.MessageID != highID || state.TemperatureCelsius != 99 {
		t.Fatalf("concurrent distinct state = %+v, want maximum tuple", state)
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

func TestProjectionRetentionPreservesOlderCurrentSource(t *testing.T) {
	fixture := newIntegrationFixture(t)
	base := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	currentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(fixture, currentID, base.Add(24*time.Hour), base, 100)); err != nil {
		t.Fatalf("persist older-storage current telemetry: %v", err)
	}
	for index := range MaxHistorySize {
		value := acceptedTelemetry(
			fixture,
			uuid.New(),
			base.Add(-time.Duration(index+1)*time.Second),
			base.Add(time.Duration(index+1)*time.Second),
			float64(index),
		)
		if err := fixture.projection.Consume(context.Background(), value); err != nil {
			t.Fatalf("persist late telemetry %d: %v", index, err)
		}
	}

	state, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read older-storage current state: %v", err)
	}
	if state.MessageID != currentID || state.TemperatureCelsius != 100 {
		t.Fatalf("older-storage current state = %+v, want original current", state)
	}
	var retainedCount int
	if err := fixture.pool.QueryRow(context.Background(), "SELECT count(*) FROM telemetry_observations WHERE device_id = $1", fixture.deviceID).Scan(&retainedCount); err != nil {
		t.Fatalf("count older-storage retained rows: %v", err)
	}
	if retainedCount != MaxHistorySize {
		t.Fatalf("older-storage retained row count = %d, want %d", retainedCount, MaxHistorySize)
	}
	var currentSequence, newestSequence int64
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT cs.observation_sequence, MAX(t.storage_sequence)
		FROM device_current_state AS cs
		JOIN telemetry_observations AS t ON t.device_id = cs.device_id
		WHERE cs.device_id = $1
		GROUP BY cs.observation_sequence
	`, fixture.deviceID).Scan(&currentSequence, &newestSequence); err != nil {
		t.Fatalf("read older-storage retention sequences: %v", err)
	}
	if currentSequence >= newestSequence {
		t.Fatalf("current source sequence = %d, newest sequence = %d; current source was not older", currentSequence, newestSequence)
	}
}

func TestProjectionReplayAfterHistoryPruneRetainsIdentityAuthority(t *testing.T) {
	fixture := newIntegrationFixture(t)
	base := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	originalID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(fixture, originalID, base, base, 12)); err != nil {
		t.Fatalf("persist original telemetry: %v", err)
	}
	for index := 1; index <= MaxHistorySize; index++ {
		if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(
			fixture,
			uuid.New(),
			base.Add(time.Duration(index)*time.Second),
			base.Add(time.Duration(index)*time.Second),
			float64(index),
		)); err != nil {
			t.Fatalf("persist pruning telemetry %d: %v", index, err)
		}
	}
	var historyContainsOriginal bool
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM telemetry_observations
			WHERE device_id = $1 AND message_id = $2
		)
	`, fixture.deviceID, originalID).Scan(&historyContainsOriginal); err != nil {
		t.Fatalf("check pruned history identity: %v", err)
	}
	if historyContainsOriginal {
		t.Fatal("original observation remained in bounded history")
	}

	beforeReplay, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read state before pruned replay: %v", err)
	}
	replay := acceptedTelemetry(fixture, originalID, base.Add(999*time.Nanosecond), base.Add(2*time.Hour), 12)
	if err := fixture.projection.Consume(context.Background(), replay); err != nil {
		t.Fatalf("exact replay after prune returned error: %v", err)
	}
	afterReplay, err := fixture.projection.GetCurrentState(context.Background(), fixture.orgID, fixture.deviceID)
	if err != nil {
		t.Fatalf("read state after pruned replay: %v", err)
	}
	if afterReplay.MessageID != beforeReplay.MessageID || !afterReplay.LastSeenAt.Equal(beforeReplay.LastSeenAt) {
		t.Fatalf("state after pruned replay = %+v, want unchanged state %+v", afterReplay, beforeReplay)
	}

	conflict := acceptedTelemetry(fixture, originalID, base, base.Add(3*time.Hour), 13)
	if err := fixture.projection.Consume(context.Background(), conflict); !errors.Is(err, ErrMessageConflict) {
		t.Fatalf("conflicting replay after prune error = %v, want ErrMessageConflict", err)
	}
	var count int
	if err := fixture.pool.QueryRow(context.Background(), "SELECT count(*) FROM telemetry_observations WHERE device_id = $1", fixture.deviceID).Scan(&count); err != nil {
		t.Fatalf("count history after pruned replay: %v", err)
	}
	if count != MaxHistorySize {
		t.Fatalf("history count after pruned replay = %d, want %d", count, MaxHistorySize)
	}
}

func TestProjectionCancellationRollsBackBlockedInsert(t *testing.T) {
	fixture := newIntegrationFixture(t)
	lockContext := context.Background()
	lockTx, err := fixture.pool.Begin(lockContext)
	if err != nil {
		t.Fatalf("begin schema lock transaction: %v", err)
	}
	if _, err := lockTx.Exec(lockContext, "LOCK TABLE telemetry_observation_keys IN ACCESS EXCLUSIVE MODE"); err != nil {
		_ = lockTx.Rollback(lockContext)
		t.Fatalf("lock telemetry identity table: %v", err)
	}

	value := acceptedTelemetry(
		fixture,
		uuid.New(),
		time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 22, 10, 0, 1, 0, time.UTC),
		55,
	)
	consumeContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	errCh := make(chan error, 1)
	go func() { errCh <- fixture.projection.Consume(consumeContext, value) }()
	err = <-errCh
	cancel()
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled)) {
		_ = lockTx.Rollback(lockContext)
		t.Fatalf("blocked consume error = %v, want cancellation", err)
	}
	if err := lockTx.Rollback(lockContext); err != nil {
		t.Fatalf("release schema lock transaction: %v", err)
	}

	var keyCount, historyCount int
	if err := fixture.pool.QueryRow(context.Background(), "SELECT count(*) FROM telemetry_observation_keys WHERE device_id = $1", fixture.deviceID).Scan(&keyCount); err != nil {
		t.Fatalf("count canceled identity rows: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), "SELECT count(*) FROM telemetry_observations WHERE device_id = $1", fixture.deviceID).Scan(&historyCount); err != nil {
		t.Fatalf("count canceled history rows: %v", err)
	}
	if keyCount != 0 || historyCount != 0 {
		t.Fatalf("canceled consume left rows: identity=%d history=%d", keyCount, historyCount)
	}
}

func TestProjectionPaginationHasNoGapsOrDuplicates(t *testing.T) {
	fixture := newIntegrationFixture(t)
	base := time.Date(2026, 9, 22, 11, 0, 0, 0, time.UTC)
	for index := range 5 {
		if err := fixture.projection.Consume(context.Background(), acceptedTelemetry(
			fixture,
			uuid.New(),
			base.Add(time.Duration(index)*time.Minute),
			base.Add(time.Duration(index)*time.Minute),
			float64(index),
		)); err != nil {
			t.Fatalf("persist pagination telemetry %d: %v", index, err)
		}
	}

	var all []TelemetryPoint
	var cursor *Cursor
	for pageNumber := range 3 {
		page, err := fixture.projection.ListTelemetry(context.Background(), fixture.orgID, fixture.deviceID, 2, cursor)
		if err != nil {
			t.Fatalf("read pagination page %d: %v", pageNumber+1, err)
		}
		all = append(all, page.Points...)
		cursor = page.NextCursor
		if cursor == nil {
			break
		}
	}
	if len(all) != 5 {
		t.Fatalf("paginated point count = %d, want 5", len(all))
	}
	seen := make(map[uuid.UUID]struct{}, len(all))
	for index, point := range all {
		if _, exists := seen[point.MessageID]; exists {
			t.Fatalf("paginated point %s repeated at index %d", point.MessageID, index)
		}
		seen[point.MessageID] = struct{}{}
		if index > 0 && !all[index-1].ObservedAt.After(point.ObservedAt) {
			t.Fatalf("pagination order at index %d = %s then %s", index, all[index-1].ObservedAt, point.ObservedAt)
		}
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
