//go:build integration

package commands

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
	ctx := context.Background()
	created, err := fixture.repository.Create(ctx, fixture.orgID, CreateInput{
		DeviceID: fixture.deviceID, Type: TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	fixture.clock.Set(created.ExpiresAt)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, _ = fixture.repository.Complete(ctx, fixture.orgID, created.ID)
	}()
	go func() {
		defer wait.Done()
		_, _ = fixture.repository.ExpireDue(ctx, 10)
	}()
	wait.Wait()
	current, err := fixture.repository.Get(ctx, fixture.orgID, created.ID)
	if err != nil {
		t.Fatalf("read command after race: %v", err)
	}
	if current.Status != StatusTimedOut || current.TerminalAt == nil {
		t.Fatalf("state after exact-deadline race = %+v, want persisted timeout", current)
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
