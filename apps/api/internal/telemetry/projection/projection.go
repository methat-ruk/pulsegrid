// Package projection owns durable telemetry history and the derived current
// state read model. It is the application consumer for MVP-005's accepted
// telemetry boundary and keeps PostgreSQL details out of transport/GraphQL.
package projection

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
)

const (
	MaxHistorySize    = 1000
	MaxPageSize       = 100
	DefaultPageSize   = 50
	retainedOtherRows = MaxHistorySize - 1
)

var (
	ErrInvalidInput      = errors.New("telemetry projection input is invalid")
	ErrNotFound          = errors.New("telemetry projection record not found")
	ErrMessageConflict   = errors.New("telemetry message ID conflicts with existing observation")
	ErrStorageConflict   = errors.New("telemetry storage identity conflicts with existing observation")
	ErrSchemaUnavailable = errors.New("telemetry projection schema is unavailable")
)

// CurrentState is the consumer-safe current state projection for one device.
// ReceivedAt is the delivery time of the selected measurement; LastSeenAt is
// the greatest delivery time of any newly stored logical observation.
type CurrentState struct {
	DeviceID           uuid.UUID
	MessageID          uuid.UUID
	ObservedAt         time.Time
	ReceivedAt         time.Time
	TemperatureCelsius float64
	LastSeenAt         time.Time
}

// TelemetryPoint is the consumer-safe history record exposed to GraphQL.
type TelemetryPoint struct {
	MessageID          uuid.UUID
	ObservedAt         time.Time
	ReceivedAt         time.Time
	TemperatureCelsius float64
}

// Cursor is the stable continuation point for recent telemetry. History is
// ordered by observed time and message ID, both descending at the API edge.
type Cursor struct {
	ObservedAt time.Time
	MessageID  uuid.UUID
}

// TelemetryPage is a bounded recent-history result.
type TelemetryPage struct {
	Points     []TelemetryPoint
	NextCursor *Cursor
}

// Repository owns telemetry persistence and tenant-scoped reads.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository constructs the projection repository over an existing pool.
func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("telemetry projection requires a database pool")
	}
	return &Repository{pool: pool}, nil
}

// ValidateSchema verifies the tables and columns required by the projection
// boundary before the API starts accepting GraphQL or MQTT traffic.
func (r *Repository) ValidateSchema(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var available bool
	err := r.pool.QueryRow(ctx, `
		SELECT NOT EXISTS (
			SELECT 1
			FROM (VALUES
				('telemetry_observation_keys'::text, 'device_id'::text),
				('telemetry_observation_keys'::text, 'message_id'::text),
				('telemetry_observation_keys'::text, 'first_ingestion_id'::text),
				('telemetry_observation_keys'::text, 'observed_at'::text),
				('telemetry_observation_keys'::text, 'temperature_celsius'::text),
				('telemetry_observations'::text, 'storage_sequence'::text),
				('telemetry_observations'::text, 'ingestion_id'::text),
				('telemetry_observations'::text, 'device_id'::text),
				('telemetry_observations'::text, 'message_id'::text),
				('telemetry_observations'::text, 'observed_at'::text),
				('telemetry_observations'::text, 'received_at'::text),
				('telemetry_observations'::text, 'temperature_celsius'::text),
				('telemetry_observations'::text, 'mqtt_duplicate'::text),
				('device_current_state'::text, 'device_id'::text),
				('device_current_state'::text, 'observation_sequence'::text),
				('device_current_state'::text, 'message_id'::text),
				('device_current_state'::text, 'observed_at'::text),
				('device_current_state'::text, 'received_at'::text),
				('device_current_state'::text, 'temperature_celsius'::text),
				('device_current_state'::text, 'last_seen_at'::text)
			) AS required(table_name, column_name)
			WHERE NOT EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_schema = current_schema()
				  AND table_name = required.table_name
				  AND column_name = required.column_name
			)
		)
	`).Scan(&available)
	if err != nil {
		return fmt.Errorf("validate telemetry projection schema: %w", err)
	}
	if !available {
		return ErrSchemaUnavailable
	}
	return nil
}

// Consume persists one accepted logical observation and updates its derived
// state atomically. Exact replays are successful idempotent no-ops.
func (r *Repository) Consume(ctx context.Context, accepted ingestion.AcceptedTelemetry) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateAccepted(accepted); err != nil {
		return err
	}
	observedAt := normalizeTime(accepted.ObservedAt)
	receivedAt := normalizeTime(accepted.ReceivedAt)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin telemetry projection transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	var deviceExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM devices
			WHERE id = $1 AND organization_id = $2
		)
	`, accepted.DeviceID, accepted.OrganizationID).Scan(&deviceExists); err != nil {
		return fmt.Errorf("verify telemetry device authority: %w", err)
	}
	if !deviceExists {
		return ErrNotFound
	}

	var firstIngestionID uuid.UUID
	insertErr := tx.QueryRow(ctx, `
		INSERT INTO telemetry_observation_keys (
			device_id,
			message_id,
			first_ingestion_id,
			observed_at,
			temperature_celsius
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (device_id, message_id) DO NOTHING
		RETURNING first_ingestion_id
	`, accepted.DeviceID, accepted.MessageID, accepted.IngestionID, observedAt, accepted.TemperatureCelsius).Scan(&firstIngestionID)
	if errors.Is(insertErr, pgx.ErrNoRows) {
		var existingObservedAt time.Time
		var existingTemperature float64
		if err := tx.QueryRow(ctx, `
			SELECT observed_at, temperature_celsius
			FROM telemetry_observation_keys
			WHERE device_id = $1 AND message_id = $2
		`, accepted.DeviceID, accepted.MessageID).Scan(&existingObservedAt, &existingTemperature); err != nil {
			return fmt.Errorf("read existing telemetry observation key: %w", err)
		}
		if !sameLogicalObservation(existingObservedAt, existingTemperature, observedAt, accepted.TemperatureCelsius) {
			return ErrMessageConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit telemetry replay: %w", err)
		}
		committed = true
		return nil
	}
	if insertErr != nil {
		if isUniqueViolation(insertErr) {
			return ErrStorageConflict
		}
		return fmt.Errorf("insert telemetry observation key: %w", insertErr)
	}

	var storageSequence int64
	insertErr = tx.QueryRow(ctx, `
		INSERT INTO telemetry_observations (
			ingestion_id,
			device_id,
			message_id,
			observed_at,
			received_at,
			temperature_celsius,
			mqtt_duplicate
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING storage_sequence
	`, accepted.IngestionID, accepted.DeviceID, accepted.MessageID, observedAt, receivedAt, accepted.TemperatureCelsius, accepted.MQTTDuplicate).Scan(&storageSequence)
	if insertErr != nil {
		if isUniqueViolation(insertErr) {
			return ErrStorageConflict
		}
		return fmt.Errorf("insert telemetry observation: %w", insertErr)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO device_current_state (
			device_id,
			observation_sequence,
			message_id,
			observed_at,
			received_at,
			temperature_celsius,
			last_seen_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $5)
		ON CONFLICT (device_id) DO UPDATE SET
			observation_sequence = CASE
				WHEN EXCLUDED.observed_at > device_current_state.observed_at
					OR (
						EXCLUDED.observed_at = device_current_state.observed_at
						AND EXCLUDED.message_id > device_current_state.message_id
					)
				THEN EXCLUDED.observation_sequence
				ELSE device_current_state.observation_sequence
			END,
			message_id = CASE
				WHEN EXCLUDED.observed_at > device_current_state.observed_at
					OR (
						EXCLUDED.observed_at = device_current_state.observed_at
						AND EXCLUDED.message_id > device_current_state.message_id
					)
				THEN EXCLUDED.message_id
				ELSE device_current_state.message_id
			END,
			observed_at = CASE
				WHEN EXCLUDED.observed_at > device_current_state.observed_at
					OR (
						EXCLUDED.observed_at = device_current_state.observed_at
						AND EXCLUDED.message_id > device_current_state.message_id
					)
				THEN EXCLUDED.observed_at
				ELSE device_current_state.observed_at
			END,
			received_at = CASE
				WHEN EXCLUDED.observed_at > device_current_state.observed_at
					OR (
						EXCLUDED.observed_at = device_current_state.observed_at
						AND EXCLUDED.message_id > device_current_state.message_id
					)
				THEN EXCLUDED.received_at
				ELSE device_current_state.received_at
			END,
			temperature_celsius = CASE
				WHEN EXCLUDED.observed_at > device_current_state.observed_at
					OR (
						EXCLUDED.observed_at = device_current_state.observed_at
						AND EXCLUDED.message_id > device_current_state.message_id
					)
				THEN EXCLUDED.temperature_celsius
				ELSE device_current_state.temperature_celsius
			END,
			last_seen_at = GREATEST(device_current_state.last_seen_at, EXCLUDED.last_seen_at)
	`, accepted.DeviceID, storageSequence, accepted.MessageID, observedAt, receivedAt, accepted.TemperatureCelsius); err != nil {
		return fmt.Errorf("update device current state: %w", err)
	}

	var currentSequence int64
	if err := tx.QueryRow(ctx, `
		SELECT observation_sequence
		FROM device_current_state
		WHERE device_id = $1
		FOR UPDATE
	`, accepted.DeviceID).Scan(&currentSequence); err != nil {
		return fmt.Errorf("lock device current state: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM telemetry_observations
		WHERE device_id = $1
		  AND storage_sequence <> $2
		  AND storage_sequence NOT IN (
			SELECT storage_sequence
			FROM telemetry_observations
			WHERE device_id = $1
			  AND storage_sequence <> $2
			ORDER BY storage_sequence DESC
			LIMIT $3
		  )
	`, accepted.DeviceID, currentSequence, retainedOtherRows); err != nil {
		return fmt.Errorf("enforce telemetry history bound: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit telemetry projection: %w", err)
	}
	committed = true
	return nil
}

// GetCurrentState returns state only inside the requested organization scope.
func (r *Repository) GetCurrentState(ctx context.Context, organizationID, deviceID uuid.UUID) (CurrentState, error) {
	if err := validateScope(organizationID, deviceID); err != nil {
		return CurrentState{}, err
	}
	var state CurrentState
	err := r.pool.QueryRow(ctx, `
		SELECT cs.device_id, cs.message_id, cs.observed_at, cs.received_at,
		       cs.temperature_celsius, cs.last_seen_at
		FROM device_current_state AS cs
		JOIN devices AS d ON d.id = cs.device_id
		WHERE d.organization_id = $1 AND cs.device_id = $2
	`, organizationID, deviceID).Scan(
		&state.DeviceID,
		&state.MessageID,
		&state.ObservedAt,
		&state.ReceivedAt,
		&state.TemperatureCelsius,
		&state.LastSeenAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CurrentState{}, ErrNotFound
	}
	if err != nil {
		return CurrentState{}, fmt.Errorf("read device current state: %w", err)
	}
	return state, nil
}

// ListTelemetry returns a bounded, tenant-scoped history page ordered by
// observed time and message ID descending.
func (r *Repository) ListTelemetry(ctx context.Context, organizationID, deviceID uuid.UUID, pageSize int, cursor *Cursor) (TelemetryPage, error) {
	if err := validateScope(organizationID, deviceID); err != nil {
		return TelemetryPage{}, err
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return TelemetryPage{}, fmt.Errorf("%w: page size must be between 1 and %d", ErrInvalidInput, MaxPageSize)
	}
	if cursor != nil {
		if cursor.MessageID == uuid.Nil || cursor.ObservedAt.IsZero() {
			return TelemetryPage{}, ErrInvalidInput
		}
		cursor = &Cursor{ObservedAt: normalizeTime(cursor.ObservedAt), MessageID: cursor.MessageID}
	}

	query := `
		SELECT t.message_id, t.observed_at, t.received_at, t.temperature_celsius
		FROM telemetry_observations AS t
		JOIN devices AS d ON d.id = t.device_id
		WHERE d.organization_id = $1 AND t.device_id = $2
		ORDER BY t.observed_at DESC, t.message_id DESC
		LIMIT $3
	`
	args := []any{organizationID, deviceID, pageSize + 1}
	if cursor != nil {
		query = `
			SELECT t.message_id, t.observed_at, t.received_at, t.temperature_celsius
			FROM telemetry_observations AS t
			JOIN devices AS d ON d.id = t.device_id
			WHERE d.organization_id = $1
			  AND t.device_id = $2
			  AND (t.observed_at, t.message_id) < ($3, $4)
			ORDER BY t.observed_at DESC, t.message_id DESC
			LIMIT $5
		`
		args = []any{organizationID, deviceID, cursor.ObservedAt, cursor.MessageID, pageSize + 1}
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return TelemetryPage{}, fmt.Errorf("list telemetry history: %w", err)
	}
	defer rows.Close()

	points := make([]TelemetryPoint, 0, pageSize)
	for rows.Next() {
		var point TelemetryPoint
		if err := rows.Scan(&point.MessageID, &point.ObservedAt, &point.ReceivedAt, &point.TemperatureCelsius); err != nil {
			return TelemetryPage{}, fmt.Errorf("scan telemetry history: %w", err)
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return TelemetryPage{}, fmt.Errorf("read telemetry history: %w", err)
	}

	page := TelemetryPage{Points: points}
	if len(points) > pageSize {
		last := points[pageSize-1]
		page.Points = points[:pageSize]
		page.NextCursor = &Cursor{ObservedAt: last.ObservedAt, MessageID: last.MessageID}
	}
	return page, nil
}

func validateAccepted(accepted ingestion.AcceptedTelemetry) error {
	if accepted.IngestionID == uuid.Nil || accepted.MessageID == uuid.Nil || accepted.OrganizationID == uuid.Nil || accepted.DeviceID == uuid.Nil {
		return ErrInvalidInput
	}
	if accepted.ObservedAt.IsZero() || accepted.ReceivedAt.IsZero() {
		return ErrInvalidInput
	}
	if math.IsNaN(accepted.TemperatureCelsius) || math.IsInf(accepted.TemperatureCelsius, 0) {
		return ErrInvalidInput
	}
	return nil
}

func validateScope(organizationID, deviceID uuid.UUID) error {
	if organizationID == uuid.Nil || deviceID == uuid.Nil {
		return ErrInvalidInput
	}
	return nil
}

func normalizeTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func sameLogicalObservation(existingObservedAt time.Time, existingTemperature float64, observedAt time.Time, temperature float64) bool {
	return normalizeTime(existingObservedAt).Equal(normalizeTime(observedAt)) && existingTemperature == temperature
}

func isUniqueViolation(err error) bool {
	pgError, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgError.Code == "23505"
}
