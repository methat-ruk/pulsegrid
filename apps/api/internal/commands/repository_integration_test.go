//go:build integration

package commands

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/migrations"
)

type repositoryFixture struct {
	pool       *pgxpool.Pool
	repository *Repository
	clock      *testClock
	orgID      uuid.UUID
	deviceID   uuid.UUID
	otherID    uuid.UUID
	foreignOrg uuid.UUID
	foreignDev uuid.UUID
}

type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.at
}

func (clock *testClock) Set(at time.Time) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.at = at
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
	orgID, err := registryRepository.CreateOrganization(ctx, "commands-"+uuid.NewString(), "Commands Integration")
	if err != nil {
		pool.Close()
		t.Fatalf("create integration organization: %v", err)
	}
	device, err := registryRepository.CreateDevice(ctx, orgID, registry.CreateDeviceInput{DeviceKey: "primary-device", DisplayName: "Primary Device"})
	if err != nil {
		pool.Close()
		t.Fatalf("create integration device: %v", err)
	}
	otherDevice, err := registryRepository.CreateDevice(ctx, orgID, registry.CreateDeviceInput{DeviceKey: "other-device", DisplayName: "Other Device"})
	if err != nil {
		pool.Close()
		t.Fatalf("create second integration device: %v", err)
	}
	foreignOrg, err := registryRepository.CreateOrganization(ctx, "commands-foreign-"+uuid.NewString(), "Foreign Commands")
	if err != nil {
		pool.Close()
		t.Fatalf("create foreign organization: %v", err)
	}
	foreignDevice, err := registryRepository.CreateDevice(ctx, foreignOrg, registry.CreateDeviceInput{DeviceKey: "foreign-device", DisplayName: "Foreign Device"})
	if err != nil {
		pool.Close()
		t.Fatalf("create foreign device: %v", err)
	}
	clock := &testClock{at: time.Date(2026, 9, 29, 4, 0, 0, 123456789, time.UTC)}
	repository, err := NewRepositoryWithClock(pool, clock)
	if err != nil {
		pool.Close()
		t.Fatalf("create command repository: %v", err)
	}
	fixture := &repositoryFixture{
		pool: pool, repository: repository, clock: clock,
		orgID: orgID, deviceID: device.ID, otherID: otherDevice.ID,
		foreignOrg: foreignOrg, foreignDev: foreignDevice.ID,
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM commands WHERE organization_id = ANY($1)`, []uuid.UUID{fixture.orgID, fixture.foreignOrg})
		_, _ = pool.Exec(cleanupContext, `DELETE FROM devices WHERE id = ANY($1)`, []uuid.UUID{fixture.deviceID, fixture.otherID, fixture.foreignDev})
		_, _ = pool.Exec(cleanupContext, `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{fixture.orgID, fixture.foreignOrg})
		pool.Close()
	})
	return fixture
}

func TestRepositoryCreateIdempotencyAndTenantScope(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	key := uuid.New()
	input := CreateInput{DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: key, RequestID: "create-request-1"}
	created, err := fixture.repository.Create(ctx, fixture.orgID, input)
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if created.Status != StatusPending || created.Type != TypePing || created.ExpiresAt.Sub(created.CreatedAt) != CommandLifetime {
		t.Fatalf("created command = %+v", created)
	}
	if created.CreatedAt.Nanosecond()%int(time.Microsecond) != 0 {
		t.Fatalf("createdAt precision = %s, want microsecond precision", created.CreatedAt)
	}
	if created.DispatchAttempts != 0 || !created.NextDispatchAt.Equal(created.CreatedAt) {
		t.Fatalf("new command dispatch metadata = (%d, %s), want (0, %s)", created.DispatchAttempts, created.NextDispatchAt, created.CreatedAt)
	}

	duplicate, err := fixture.repository.Create(ctx, fixture.orgID, input)
	if err != nil || duplicate.ID != created.ID {
		t.Fatalf("duplicate create = (%+v, %v), want same command", duplicate, err)
	}
	if _, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
		DeviceID: fixture.otherID, Type: TypePing, IdempotencyKey: key,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting key reuse error = %v, want ErrConflict", err)
	}
	if _, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
		DeviceID: fixture.foreignDev, Type: TypePing, IdempotencyKey: key,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign device create error = %v, want ErrNotFound before key lookup", err)
	}
	if _, err := fixture.repository.Get(ctx, fixture.foreignOrg, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign command read error = %v, want ErrNotFound", err)
	}
	foreignPage, err := fixture.repository.List(ctx, fixture.foreignOrg, fixture.deviceID, DefaultPageSize, nil)
	if err != nil || len(foreignPage.Commands) != 0 {
		t.Fatalf("foreign command page = (%+v, %v), want empty", foreignPage, err)
	}

	fixture.clock.Set(created.CreatedAt.Add(time.Second))
	dispatched, err := fixture.repository.MarkDispatched(ctx, fixture.orgID, created.ID)
	if err != nil || dispatched.Status != StatusDispatched || dispatched.DispatchedAt == nil {
		t.Fatalf("dispatch = (%+v, %v)", dispatched, err)
	}
	acknowledged, err := fixture.repository.Acknowledge(ctx, fixture.orgID, created.ID)
	if err != nil || acknowledged.Status != StatusAcknowledged || acknowledged.AcknowledgedAt == nil {
		t.Fatalf("acknowledge = (%+v, %v)", acknowledged, err)
	}
	completed, err := fixture.repository.Complete(ctx, fixture.orgID, created.ID)
	if err != nil || completed.Status != StatusCompleted || completed.TerminalAt == nil {
		t.Fatalf("complete = (%+v, %v)", completed, err)
	}
	replayed, err := fixture.repository.Create(ctx, fixture.orgID, input)
	if err != nil || replayed.ID != created.ID || replayed.Status != StatusCompleted {
		t.Fatalf("replay after terminal = (%+v, %v), want stored completed command", replayed, err)
	}
	if _, err := fixture.repository.Fail(ctx, fixture.orgID, created.ID, FailureDeviceReported); !errors.Is(err, ErrConflict) {
		t.Fatalf("contradictory terminal result = %v, want ErrConflict", err)
	}

	page, err := fixture.repository.List(ctx, fixture.orgID, fixture.deviceID, 1, nil)
	if err != nil || len(page.Commands) != 1 || page.Commands[0].ID != created.ID || page.NextCursor != nil {
		t.Fatalf("list command = (%+v, %v)", page, err)
	}
}

func TestRepositoryOldInsertGetsCompatibleDispatchDefaults(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	now := normalizeTime(fixture.clock.Now())
	id := uuid.New()
	_, err := fixture.pool.Exec(ctx, `
		INSERT INTO commands (
			id, organization_id, device_id, type, status, idempotency_key,
			created_request_id, created_at, updated_at, expires_at
		)
		VALUES ($1, $2, $3, 'PING', 'PENDING', $4, '', $5, $5, $6)
	`,
		id, fixture.orgID, fixture.deviceID, uuid.New(), now, now.Add(CommandLifetime),
	)
	if err != nil {
		t.Fatalf("insert using the pre-008 command shape: %v", err)
	}
	var attempts int
	var nextDispatchAt time.Time
	if err := fixture.pool.QueryRow(ctx, `SELECT dispatch_attempts, next_dispatch_at FROM commands WHERE id = $1`, id).Scan(&attempts, &nextDispatchAt); err != nil {
		t.Fatalf("read default dispatch metadata: %v", err)
	}
	if attempts != 0 || nextDispatchAt.IsZero() {
		t.Fatalf("legacy insert dispatch metadata = (%d, %s)", attempts, nextDispatchAt)
	}
}

func TestMigration008BackfillsExistingActiveAndTerminalRows(t *testing.T) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	var retryColumnsExist bool
	if err := pool.QueryRow(ctx, `
		SELECT count(*) = 2 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'commands'
		  AND column_name IN ('dispatch_attempts', 'next_dispatch_at')
	`).Scan(&retryColumnsExist); err != nil {
		t.Fatalf("inspect migration baseline: %v", err)
	}
	if retryColumnsExist {
		t.Skip("migration 008 backfill is exercised only from the migration 007 baseline")
	}

	registryRepository, err := registry.NewRepository(pool)
	if err != nil {
		t.Fatalf("create registry repository: %v", err)
	}
	organizationID, err := registryRepository.CreateOrganization(ctx, "commands-migration-"+uuid.NewString(), "Migration Integration")
	if err != nil {
		t.Fatalf("create integration organization: %v", err)
	}
	device, err := registryRepository.CreateDevice(ctx, organizationID, registry.CreateDeviceInput{DeviceKey: "migration-device", DisplayName: "Migration Device"})
	if err != nil {
		t.Fatalf("create integration device: %v", err)
	}
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	createdAt := time.Date(2026, 9, 29, 4, 0, 0, 123456000, time.UTC)
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM commands WHERE id = ANY($1)`, ids)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM devices WHERE id = $1`, device.ID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM organizations WHERE id = $1`, organizationID)
	})
	for index, status := range []Status{StatusPending, StatusCompleted} {
		var dispatchedAt, acknowledgedAt, terminalAt any
		if status == StatusCompleted {
			dispatchedAt, acknowledgedAt, terminalAt = createdAt, createdAt, createdAt
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO commands (
				id, organization_id, device_id, type, status, idempotency_key,
				created_request_id, created_at, updated_at, expires_at,
				dispatched_at, acknowledged_at, terminal_at
			) VALUES ($1, $2, $3, 'PING', $4, $5, '', $6, $6, $7, $8, $9, $10)
		`, ids[index], organizationID, device.ID, status, uuid.New(), createdAt, createdAt.Add(CommandLifetime), dispatchedAt, acknowledgedAt, terminalAt)
		if err != nil {
			t.Fatalf("insert pre-008 %s command: %v", status, err)
		}
	}
	if err := migrations.Run(ctx, databaseConfiguration.URL, "up"); err != nil {
		t.Fatalf("apply migration 008: %v", err)
	}
	for _, id := range ids {
		var attempts int
		var nextDispatchAt time.Time
		if err := pool.QueryRow(ctx, `SELECT dispatch_attempts, next_dispatch_at FROM commands WHERE id = $1`, id).Scan(&attempts, &nextDispatchAt); err != nil {
			t.Fatalf("read backfilled command %s: %v", id, err)
		}
		if attempts != 0 || !nextDispatchAt.Equal(createdAt) {
			t.Fatalf("backfilled command %s metadata = (%d, %s), want (0, %s)", id, attempts, nextDispatchAt, createdAt)
		}
	}
}

func TestRepositoryClaimDispatchIsDurableBoundedAndConcurrentSafe(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	created, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
		DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	const requests = 8
	start := make(chan struct{})
	claims := make(chan *Command, requests)
	errorsChannel := make(chan error, requests)
	for range requests {
		go func() {
			<-start
			claimed, claimErr := fixture.repository.ClaimNextDispatch(ctx, fixture.orgID)
			claims <- claimed
			errorsChannel <- claimErr
		}()
	}
	close(start)
	var reservation *Command
	claimCount := 0
	for range requests {
		claimed, claimErr := <-claims, <-errorsChannel
		if claimErr != nil {
			t.Fatalf("concurrent reservation: %v", claimErr)
		}
		if claimed != nil {
			claimCount++
			reservation = claimed
		}
	}
	if claimCount != 1 || reservation == nil || reservation.ID != created.ID || reservation.DispatchAttempts != 1 {
		t.Fatalf("concurrent claims = %d, reservation = %+v", claimCount, reservation)
	}
	if !reservation.NextDispatchAt.Equal(normalizeTime(fixture.clock.Now().Add(10 * time.Second))) {
		t.Fatalf("first retry at = %s", reservation.NextDispatchAt)
	}

	for attempt, backoff := range []time.Duration{20 * time.Second, 40 * time.Second} {
		fixture.clock.Set(reservation.NextDispatchAt)
		reservation, err = fixture.repository.ClaimNextDispatch(ctx, fixture.orgID)
		if err != nil || reservation == nil || reservation.DispatchAttempts != attempt+2 {
			t.Fatalf("reservation %d = (%+v, %v)", attempt+2, reservation, err)
		}
		if !reservation.NextDispatchAt.Equal(fixture.clock.Now().Add(backoff)) {
			t.Fatalf("reservation %d next at = %s", attempt+2, reservation.NextDispatchAt)
		}
	}
	fixture.clock.Set(reservation.NextDispatchAt)
	reservation, err = fixture.repository.ClaimNextDispatch(ctx, fixture.orgID)
	if err != nil || reservation == nil || reservation.DispatchAttempts != MaxDispatchAttempts ||
		!reservation.NextDispatchAt.Equal(created.ExpiresAt) {
		t.Fatalf("fourth reservation = (%+v, %v)", reservation, err)
	}
	if extra, err := fixture.repository.ClaimNextDispatch(ctx, fixture.orgID); err != nil || extra != nil {
		t.Fatalf("reservation beyond bound = (%+v, %v)", extra, err)
	}
}

func TestRepositoryDoesNotRedispatchAcknowledgedCommand(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	created, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
		DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	reservation, err := fixture.repository.ClaimNextDispatch(ctx, fixture.orgID)
	if err != nil || reservation == nil || reservation.ID != created.ID {
		t.Fatalf("first reservation = (%+v, %v)", reservation, err)
	}
	acknowledged, err := fixture.repository.ApplyDeviceResponse(ctx, fixture.orgID, fixture.deviceID, created.ID, EventAck, "")
	if err != nil || acknowledged.Status != StatusAcknowledged {
		t.Fatalf("acknowledge command = (%+v, %v)", acknowledged, err)
	}
	fixture.clock.Set(reservation.NextDispatchAt)
	if retry, err := fixture.repository.ClaimNextDispatch(ctx, fixture.orgID); err != nil || retry != nil {
		t.Fatalf("acknowledged command retry = (%+v, %v), want no reservation", retry, err)
	}
}

func TestRepositoryDeviceResponseBindsOrganizationAndDevice(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	created, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
		DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if _, err := fixture.repository.ApplyDeviceResponse(ctx, fixture.orgID, fixture.otherID, created.ID, EventComplete, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("same-tenant wrong-device response error = %v, want not found", err)
	}
	if _, err := fixture.repository.ApplyDeviceResponse(ctx, fixture.foreignOrg, fixture.deviceID, created.ID, EventComplete, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign-tenant response error = %v, want not found", err)
	}
	if _, err := fixture.repository.ApplyDeviceResponse(ctx, fixture.orgID, fixture.deviceID, created.ID, EventFail, FailureDelivery); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("device-selected delivery failure = %v, want invalid input", err)
	}
	current, err := fixture.repository.Get(ctx, fixture.orgID, created.ID)
	if err != nil || current.Status != StatusPending {
		t.Fatalf("command after denied responses = (%+v, %v)", current, err)
	}
	completed, err := fixture.repository.ApplyDeviceResponse(ctx, fixture.orgID, fixture.deviceID, created.ID, EventComplete, "")
	if err != nil || completed.Status != StatusCompleted || completed.AcknowledgedAt == nil {
		t.Fatalf("valid device result = (%+v, %v)", completed, err)
	}
}

func TestRepositoryExpiryCanBeScopedToOneOrganization(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	local, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New()})
	if err != nil {
		t.Fatalf("create local command: %v", err)
	}
	foreign, err := fixture.repository.Create(ctx, fixture.foreignOrg, CreateInput{DeviceID: fixture.foreignDev, Type: TypePing, IdempotencyKey: uuid.New()})
	if err != nil {
		t.Fatalf("create foreign command: %v", err)
	}
	fixture.clock.Set(local.ExpiresAt)
	expired, err := fixture.repository.ExpireDueForOrganization(ctx, fixture.orgID, MaxExpireBatchSize)
	if err != nil || len(expired) != 1 || expired[0].ID != local.ID {
		t.Fatalf("scoped expiry = (%+v, %v)", expired, err)
	}
	currentForeign, err := fixture.repository.Get(ctx, fixture.foreignOrg, foreign.ID)
	if err != nil || currentForeign.Status != StatusPending {
		t.Fatalf("foreign command after scoped expiry = (%+v, %v)", currentForeign, err)
	}
}

func TestRepositoryConcurrentDuplicateCreateHasOneIdentity(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	input := CreateInput{DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New()}
	const requests = 12
	var wait sync.WaitGroup
	results := make(chan Command, requests)
	errorsSeen := make(chan error, requests)
	for range requests {
		wait.Go(func() {
			command, err := fixture.repository.Create(ctx, fixture.orgID, input)
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- command
		})
	}
	wait.Wait()
	close(results)
	close(errorsSeen)
	var commandID uuid.UUID
	count := 0
	for command := range results {
		if commandID == uuid.Nil {
			commandID = command.ID
		} else if command.ID != commandID {
			t.Fatalf("concurrent create returned IDs %s and %s", commandID, command.ID)
		}
		count++
	}
	for err := range errorsSeen {
		t.Fatalf("concurrent create error: %v", err)
	}
	if count != requests {
		t.Fatalf("successful idempotent creates = %d, want %d", count, requests)
	}
	var rowCount int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM commands WHERE organization_id = $1 AND idempotency_key = $2`, fixture.orgID, input.IdempotencyKey).Scan(&rowCount); err != nil {
		t.Fatalf("count command rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("command row count = %d, want 1", rowCount)
	}
}

func TestRepositoryExpiryAndResultRaceUsesOneTerminalState(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	created, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
		DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	fixture.clock.Set(created.ExpiresAt)
	type completionResult struct {
		command Command
		err     error
	}
	type expiryResult struct {
		commands []Command
		err      error
	}
	completed := make(chan completionResult, 1)
	expired := make(chan expiryResult, 1)
	go func() {
		command, err := fixture.repository.Complete(ctx, fixture.orgID, created.ID)
		completed <- completionResult{command: command, err: err}
	}()
	go func() {
		commands, err := fixture.repository.ExpireDue(ctx, 10)
		expired <- expiryResult{commands: commands, err: err}
	}()
	completion := <-completed
	expiration := <-expired
	if !errors.Is(completion.err, ErrTimedOut) || completion.command.Status != StatusTimedOut || completion.command.TerminalAt == nil {
		t.Fatalf("completion race result = (%+v, %v), want persisted timeout and ErrTimedOut", completion.command, completion.err)
	}
	if expiration.err != nil || len(expiration.commands) > 1 {
		t.Fatalf("expiry race result = (%+v, %v), want no error and at most one row", expiration.commands, expiration.err)
	}
	if len(expiration.commands) == 1 &&
		(expiration.commands[0].ID != created.ID || expiration.commands[0].Status != StatusTimedOut) {
		t.Fatalf("expiry race row = %+v, want the command in TIMED_OUT", expiration.commands[0])
	}
	current, err := fixture.repository.Get(ctx, fixture.orgID, created.ID)
	if err != nil {
		t.Fatalf("read command after race: %v", err)
	}
	if current.Status != StatusTimedOut || current.TerminalAt == nil || !current.TerminalAt.Equal(created.ExpiresAt) {
		t.Fatalf("state after exact-deadline race = %+v, want persisted timeout", current)
	}
}

func TestRepositoryTransitionRechecksDeadlineAfterRowLockWait(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	created, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
		DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	fixture.clock.Set(created.ExpiresAt.Add(-time.Microsecond))

	locker, err := fixture.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock connection: %v", err)
	}
	defer locker.Release()
	tx, err := locker.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var blockerPID int32
	if err := locker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("read lock connection pid: %v", err)
	}
	var lockedID uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT id FROM commands WHERE organization_id = $1 AND id = $2 FOR UPDATE
	`, fixture.orgID, created.ID).Scan(&lockedID); err != nil {
		t.Fatalf("lock command row: %v", err)
	}
	if lockedID != created.ID {
		t.Fatalf("locked command = %s, want %s", lockedID, created.ID)
	}

	type transitionResult struct {
		command Command
		err     error
	}
	transitionCtx, transitionCancel := context.WithTimeout(ctx, 5*time.Second)
	defer transitionCancel()
	completed := make(chan transitionResult, 1)
	go func() {
		command, err := fixture.repository.Complete(transitionCtx, fixture.orgID, created.ID)
		completed <- transitionResult{command: command, err: err}
	}()
	if err := waitForCommandRowLockWait(transitionCtx, fixture.pool, blockerPID); err != nil {
		t.Fatalf("observe transition blocked by test lock: %v", err)
	}

	fixture.clock.Set(created.ExpiresAt)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("release command row lock: %v", err)
	}
	transition := <-completed
	if !errors.Is(transition.err, ErrTimedOut) || transition.command.Status != StatusTimedOut ||
		transition.command.TerminalAt == nil || !transition.command.TerminalAt.Equal(created.ExpiresAt) {
		t.Fatalf("transition after lock wait = (%+v, %v), want TIMED_OUT at the post-lock deadline", transition.command, transition.err)
	}

	current, err := fixture.repository.Get(ctx, fixture.orgID, created.ID)
	if err != nil {
		t.Fatalf("read command after lock-wait transition: %v", err)
	}
	if current.Status != StatusTimedOut || current.TerminalAt == nil || !current.TerminalAt.Equal(created.ExpiresAt) {
		t.Fatalf("stored state after lock-wait transition = %+v, want TIMED_OUT at deadline", current)
	}
}

func waitForCommandRowLockWait(ctx context.Context, pool *pgxpool.Pool, blockerPID int32) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity AS waiter
				WHERE waiter.datname = current_database()
				  AND waiter.pid <> pg_backend_pid()
				  AND waiter.wait_event_type = 'Lock'
				  AND $1 = ANY(pg_blocking_pids(waiter.pid))
			)
		`, blockerPID).Scan(&blocked)
		if err != nil {
			return fmt.Errorf("inspect PostgreSQL lock wait: %w", err)
		}
		if blocked {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("transition did not wait on the held command lock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestRepositoryExpireDueHonorsBatchBound(t *testing.T) {
	fixture := newRepositoryFixture(t)
	ctx := context.Background()
	created := make([]Command, 0, 3)
	for range 3 {
		command, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
			DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New(),
		})
		if err != nil {
			t.Fatalf("create due command: %v", err)
		}
		created = append(created, command)
		fixture.clock.Set(fixture.clock.Now().Add(time.Second))
	}
	fixture.clock.Set(created[len(created)-1].ExpiresAt)
	expired, err := fixture.repository.ExpireDue(ctx, 2)
	if err != nil || len(expired) != 2 {
		t.Fatalf("first expiry batch = (%d commands, %v), want two", len(expired), err)
	}
	remaining, err := fixture.repository.ExpireDue(ctx, 2)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("second expiry batch = (%d commands, %v), want one", len(remaining), err)
	}
	if _, err := fixture.repository.ExpireDue(ctx, MaxExpireBatchSize+1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized expiry batch error = %v, want ErrInvalidInput", err)
	}
}
