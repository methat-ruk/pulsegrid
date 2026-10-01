package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	MaxExpireBatchSize  = 100
	MaxDispatchAttempts = 4
	MinimumDispatchTime = 5 * time.Second
)

type Clock interface {
	Now() time.Time
}

type ClockFunc func() time.Time

func (clock ClockFunc) Now() time.Time { return clock() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type Repository struct {
	pool  *pgxpool.Pool
	clock Clock
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	return NewRepositoryWithClock(pool, systemClock{})
}

func NewRepositoryWithClock(pool *pgxpool.Pool, clock Clock) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("command repository requires a database pool")
	}
	if clock == nil {
		return nil, errors.New("command repository requires a clock")
	}
	return &Repository{pool: pool, clock: clock}, nil
}

func (r *Repository) ValidateSchema(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var available bool
	err := r.pool.QueryRow(ctx, `
		SELECT NOT EXISTS (
			SELECT 1
			FROM (VALUES
				('commands'::text, 'id'::text),
				('commands'::text, 'organization_id'::text),
				('commands'::text, 'device_id'::text),
				('commands'::text, 'type'::text),
				('commands'::text, 'status'::text),
				('commands'::text, 'idempotency_key'::text),
				('commands'::text, 'created_request_id'::text),
				('commands'::text, 'created_at'::text),
				('commands'::text, 'updated_at'::text),
				('commands'::text, 'expires_at'::text),
				('commands'::text, 'dispatched_at'::text),
				('commands'::text, 'acknowledged_at'::text),
				('commands'::text, 'terminal_at'::text),
				('commands'::text, 'failure_code'::text),
				('commands'::text, 'dispatch_attempts'::text),
				('commands'::text, 'next_dispatch_at'::text)
			) AS required(table_name, column_name)
			WHERE NOT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = current_schema()
				  AND table_name = required.table_name
				  AND column_name = required.column_name
			)
		)
	`).Scan(&available)
	if err != nil {
		return fmt.Errorf("validate command schema: %w", err)
	}
	if !available {
		return ErrSchemaUnavailable
	}
	return nil
}

func (r *Repository) Create(ctx context.Context, organizationID uuid.UUID, input CreateInput) (Command, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !validCreateInput(organizationID, input) {
		return Command{}, ErrInvalidInput
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Command{}, fmt.Errorf("begin command creation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var deviceID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM devices
		WHERE organization_id = $1 AND id = $2
		FOR KEY SHARE
	`, organizationID, input.DeviceID).Scan(&deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, ErrNotFound
	}
	if err != nil {
		return Command{}, fmt.Errorf("validate command device scope: %w", err)
	}

	if existing, found, lookupErr := commandByIdempotencyKey(ctx, tx, organizationID, input.IdempotencyKey); lookupErr != nil {
		return Command{}, lookupErr
	} else if found {
		return finishDuplicate(ctx, tx, existing, input)
	}

	createdAt := normalizeTime(r.clock.Now())
	command := Command{
		ID:        uuid.New(),
		DeviceID:  deviceID,
		Type:      input.Type,
		Status:    StatusPending,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		ExpiresAt: createdAt.Add(CommandLifetime),
	}
	command, err = scanCommand(tx.QueryRow(ctx, `
		INSERT INTO commands (
			id, organization_id, device_id, type, status, idempotency_key,
			created_request_id, created_at, updated_at, expires_at, dispatch_attempts,
			next_dispatch_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0, $8)
		ON CONFLICT ON CONSTRAINT commands_idempotency_unique DO NOTHING
		RETURNING `+commandColumns,
		command.ID, organizationID, command.DeviceID, command.Type, command.Status,
		input.IdempotencyKey, input.RequestID, command.CreatedAt, command.UpdatedAt, command.ExpiresAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		// Under READ COMMITTED the follow-up statement sees the transaction that
		// won the unique-key race after ON CONFLICT waited for its commit.
		existing, found, lookupErr := commandByIdempotencyKey(ctx, tx, organizationID, input.IdempotencyKey)
		if lookupErr != nil {
			return Command{}, lookupErr
		}
		if !found {
			return Command{}, ErrConflict
		}
		return finishDuplicate(ctx, tx, existing, input)
	}
	if err != nil {
		return Command{}, fmt.Errorf("insert command intent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, fmt.Errorf("commit command intent: %w", err)
	}
	return command, nil
}

func (r *Repository) Get(ctx context.Context, organizationID, commandID uuid.UUID) (Command, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil || commandID == uuid.Nil {
		return Command{}, ErrInvalidInput
	}
	command, err := scanCommand(r.pool.QueryRow(ctx, `
		SELECT `+commandColumns+`
		FROM commands
		WHERE organization_id = $1 AND id = $2
	`, organizationID, commandID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, ErrNotFound
	}
	if err != nil {
		return Command{}, fmt.Errorf("read command: %w", err)
	}
	return command, nil
}

func (r *Repository) List(ctx context.Context, organizationID, deviceID uuid.UUID, pageSize int, cursor *Cursor) (Page, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil || deviceID == uuid.Nil || pageSize < 1 || pageSize > MaxPageSize ||
		(cursor != nil && (cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero())) {
		return Page{}, ErrInvalidInput
	}
	var cursorTime *time.Time
	var cursorID *uuid.UUID
	if cursor != nil {
		createdAt := normalizeTime(cursor.CreatedAt)
		cursorTime, cursorID = &createdAt, &cursor.ID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+commandColumns+`
		FROM commands
		WHERE organization_id = $1 AND device_id = $2
		  AND ($3::timestamptz IS NULL OR (created_at, id) < ($3, $4))
		ORDER BY created_at DESC, id DESC
		LIMIT $5
	`, organizationID, deviceID, cursorTime, cursorID, pageSize+1)
	if err != nil {
		return Page{}, fmt.Errorf("list device commands: %w", err)
	}
	defer rows.Close()

	commands := make([]Command, 0, pageSize+1)
	for rows.Next() {
		command, scanErr := scanCommand(rows)
		if scanErr != nil {
			return Page{}, fmt.Errorf("scan device command: %w", scanErr)
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate device commands: %w", err)
	}
	page := Page{Commands: commands}
	if len(commands) > pageSize {
		page.Commands = commands[:pageSize]
		last := page.Commands[len(page.Commands)-1]
		page.NextCursor = &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

func (r *Repository) MarkDispatched(ctx context.Context, organizationID, commandID uuid.UUID) (Command, error) {
	return r.apply(ctx, organizationID, commandID, EventDispatch, "")
}

func (r *Repository) Acknowledge(ctx context.Context, organizationID, commandID uuid.UUID) (Command, error) {
	return r.apply(ctx, organizationID, commandID, EventAck, "")
}

func (r *Repository) Complete(ctx context.Context, organizationID, commandID uuid.UUID) (Command, error) {
	return r.apply(ctx, organizationID, commandID, EventComplete, "")
}

func (r *Repository) Fail(ctx context.Context, organizationID, commandID uuid.UUID, failureCode FailureCode) (Command, error) {
	return r.apply(ctx, organizationID, commandID, EventFail, failureCode)
}

// ApplyDeviceResponse applies an outcome only when tenant, device, and command
// identity all match inside the same row-lock transaction.
func (r *Repository) ApplyDeviceResponse(ctx context.Context, organizationID, deviceID, commandID uuid.UUID, event Event, failureCode FailureCode) (Command, error) {
	if (event != EventAck && event != EventComplete && event != EventFail) ||
		(event == EventFail && failureCode != FailureDeviceReported) ||
		(event != EventFail && failureCode != "") {
		return Command{}, ErrInvalidInput
	}
	return r.applyScoped(ctx, organizationID, &deviceID, commandID, event, failureCode)
}

func (r *Repository) apply(ctx context.Context, organizationID, commandID uuid.UUID, event Event, failureCode FailureCode) (Command, error) {
	return r.applyScoped(ctx, organizationID, nil, commandID, event, failureCode)
}

func (r *Repository) applyScoped(ctx context.Context, organizationID uuid.UUID, deviceID *uuid.UUID, commandID uuid.UUID, event Event, failureCode FailureCode) (Command, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil || commandID == uuid.Nil || (deviceID != nil && *deviceID == uuid.Nil) {
		return Command{}, ErrInvalidInput
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Command{}, fmt.Errorf("begin command transition: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	query := `
		SELECT ` + commandColumns + `
		FROM commands
		WHERE organization_id = $1 AND id = $2
		FOR UPDATE
	`
	args := []any{organizationID, commandID}
	if deviceID != nil {
		query = `
			SELECT ` + commandColumns + `
			FROM commands
			WHERE organization_id = $1 AND device_id = $2 AND id = $3
			FOR UPDATE
		`
		args = []any{organizationID, *deviceID, commandID}
	}
	command, err := scanCommand(tx.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, ErrNotFound
	}
	if err != nil {
		return Command{}, fmt.Errorf("lock command for transition: %w", err)
	}

	// Read time only after FOR UPDATE succeeds so a lock wait cannot let an
	// event accepted before the deadline commit after a later timeout winner.
	now := normalizeTime(r.clock.Now())
	updated, changed, transitionErr := command.Apply(event, now, failureCode)
	if transitionErr != nil && !errors.Is(transitionErr, ErrTimedOut) {
		return Command{}, transitionErr
	}
	if changed {
		updated, err = scanCommand(tx.QueryRow(ctx, `
			UPDATE commands
			SET status = $3,
			    updated_at = $4,
			    dispatched_at = $5,
			    acknowledged_at = $6,
			    terminal_at = $7,
			    failure_code = $8
			WHERE organization_id = $1 AND id = $2
			RETURNING `+commandColumns,
			organizationID, commandID, updated.Status, updated.UpdatedAt,
			updated.DispatchedAt, updated.AcknowledgedAt, updated.TerminalAt, updated.FailureCode,
		))
		if err != nil {
			return Command{}, fmt.Errorf("persist command transition: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, fmt.Errorf("commit command transition: %w", err)
	}
	return updated, transitionErr
}

// ClaimNextDispatch durably reserves one due publish before the MQTT side
// effect. A consumed slot survives restart, including a crash before publish.
func (r *Repository) ClaimNextDispatch(ctx context.Context, organizationID uuid.UUID) (*Command, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if organizationID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin dispatch reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	now := normalizeTime(r.clock.Now())
	command, err := scanCommand(tx.QueryRow(ctx, `
		SELECT `+commandColumns+`
		FROM commands
		WHERE organization_id = $1
		  AND status IN ('PENDING', 'DISPATCHED')
		  AND dispatch_attempts < $2
		  AND next_dispatch_at <= $3
		  AND expires_at > $3 + interval '5 seconds'
		ORDER BY next_dispatch_at ASC, created_at ASC, id ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`, organizationID, MaxDispatchAttempts, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select due command for dispatch: %w", err)
	}

	// Recheck after acquiring the row lock; waiting cannot grant an expired
	// command a publish slot.
	now = normalizeTime(r.clock.Now())
	if (command.Status != StatusPending && command.Status != StatusDispatched) ||
		command.DispatchAttempts >= MaxDispatchAttempts || command.NextDispatchAt.After(now) ||
		!now.Add(MinimumDispatchTime).Before(command.ExpiresAt) {
		return nil, nil
	}
	attempt := command.DispatchAttempts + 1
	nextAt := command.ExpiresAt
	switch attempt {
	case 1:
		nextAt = now.Add(10 * time.Second)
	case 2:
		nextAt = now.Add(20 * time.Second)
	case 3:
		nextAt = now.Add(40 * time.Second)
	}
	command, err = scanCommand(tx.QueryRow(ctx, `
		UPDATE commands
		SET dispatch_attempts = $3, next_dispatch_at = $4
		WHERE organization_id = $1 AND id = $2
		RETURNING `+commandColumns,
		organizationID, command.ID, attempt, nextAt,
	))
	if err != nil {
		return nil, fmt.Errorf("persist dispatch reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit dispatch reservation: %w", err)
	}
	return &command, nil
}

// ExpireDue marks at most limit active commands whose immutable deadlines have
// passed. SKIP LOCKED lets a future set of bounded workers divide the due set.
func (r *Repository) ExpireDue(ctx context.Context, limit int) ([]Command, error) {
	return r.expireDue(ctx, nil, limit)
}

// ExpireDueForOrganization bounds the local command runtime's mutation scope
// to the server-selected tenant.
func (r *Repository) ExpireDueForOrganization(ctx context.Context, organizationID uuid.UUID, limit int) ([]Command, error) {
	if organizationID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	return r.expireDue(ctx, &organizationID, limit)
}

func (r *Repository) expireDue(ctx context.Context, organizationID *uuid.UUID, limit int) ([]Command, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit < 1 || limit > MaxExpireBatchSize {
		return nil, ErrInvalidInput
	}
	now := normalizeTime(r.clock.Now())
	scopeFilter := ""
	args := []any{now, limit}
	if organizationID != nil {
		scopeFilter = "organization_id = $3 AND"
		args = append(args, *organizationID)
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		WITH due AS (
			SELECT id
			FROM commands
			WHERE %s status IN ('PENDING', 'DISPATCHED', 'ACKNOWLEDGED')
			  AND expires_at <= $1
			ORDER BY expires_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE commands AS command
		SET status = 'TIMED_OUT', updated_at = $1, terminal_at = $1
		FROM due
		WHERE command.id = due.id
		RETURNING `+qualifiedCommandColumns("command")+`
	`, scopeFilter), args...)
	if err != nil {
		return nil, fmt.Errorf("expire due commands: %w", err)
	}
	defer rows.Close()
	commands := make([]Command, 0, limit)
	for rows.Next() {
		command, scanErr := scanCommand(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan expired command: %w", scanErr)
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired commands: %w", err)
	}
	return commands, nil
}

const commandColumns = `id, device_id, type, status, created_at, updated_at,
       expires_at, dispatched_at, acknowledged_at, terminal_at, failure_code,
       dispatch_attempts, next_dispatch_at`

func qualifiedCommandColumns(alias string) string {
	columns := strings.Split(commandColumns, ", ")
	for index, column := range columns {
		columns[index] = alias + "." + strings.TrimSpace(column)
	}
	return strings.Join(columns, ", ")
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCommand(row rowScanner) (Command, error) {
	var command Command
	var commandType string
	var status string
	var dispatchedAt pgtype.Timestamptz
	var acknowledgedAt pgtype.Timestamptz
	var terminalAt pgtype.Timestamptz
	var failureCode pgtype.Text
	var nextDispatchAt pgtype.Timestamptz
	err := row.Scan(
		&command.ID, &command.DeviceID, &commandType, &status,
		&command.CreatedAt, &command.UpdatedAt, &command.ExpiresAt,
		&dispatchedAt, &acknowledgedAt, &terminalAt, &failureCode,
		&command.DispatchAttempts, &nextDispatchAt,
	)
	if err != nil {
		return Command{}, err
	}
	command.Type = Type(commandType)
	command.Status = Status(status)
	if dispatchedAt.Valid {
		command.DispatchedAt = timePointer(dispatchedAt.Time.UTC())
	}
	if acknowledgedAt.Valid {
		command.AcknowledgedAt = timePointer(acknowledgedAt.Time.UTC())
	}
	if terminalAt.Valid {
		command.TerminalAt = timePointer(terminalAt.Time.UTC())
	}
	if failureCode.Valid {
		value := FailureCode(failureCode.String)
		command.FailureCode = &value
	}
	if nextDispatchAt.Valid {
		command.NextDispatchAt = nextDispatchAt.Time.UTC()
	}
	command.CreatedAt = command.CreatedAt.UTC()
	command.UpdatedAt = command.UpdatedAt.UTC()
	command.ExpiresAt = command.ExpiresAt.UTC()
	return command, nil
}

func commandByIdempotencyKey(ctx context.Context, tx pgx.Tx, organizationID, idempotencyKey uuid.UUID) (Command, bool, error) {
	command, err := scanCommand(tx.QueryRow(ctx, `
		SELECT `+commandColumns+`
		FROM commands
		WHERE organization_id = $1 AND idempotency_key = $2
	`, organizationID, idempotencyKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, false, nil
	}
	if err != nil {
		return Command{}, false, fmt.Errorf("read command idempotency record: %w", err)
	}
	return command, true, nil
}

func finishDuplicate(ctx context.Context, tx pgx.Tx, existing Command, input CreateInput) (Command, error) {
	if existing.DeviceID != input.DeviceID || existing.Type != input.Type {
		return Command{}, ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, fmt.Errorf("finish duplicate command lookup: %w", err)
	}
	return existing, nil
}

func validCreateInput(organizationID uuid.UUID, input CreateInput) bool {
	if organizationID == uuid.Nil || input.DeviceID == uuid.Nil || input.IdempotencyKey == uuid.Nil ||
		input.Type != TypePing || !utf8.ValidString(input.RequestID) || len(input.RequestID) > MaxRequestIDSize {
		return false
	}
	for _, character := range input.RequestID {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && !strings.ContainsRune("._-", character) {
			return false
		}
	}
	return true
}
