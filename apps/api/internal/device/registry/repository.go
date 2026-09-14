// Package registry owns the device identity and organization relationship
// persistence boundary.
package registry

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound is returned when a record is absent from the requested scope.
	ErrNotFound = errors.New("registry record not found")
	// ErrConflict is returned for an existing organization slug or tenant-local
	// device key.
	ErrConflict = errors.New("registry record conflicts with existing data")
	// ErrInvalidInput is returned before a query for invalid caller input.
	ErrInvalidInput = errors.New("registry input is invalid")
	// ErrInvalidOrganization is returned when an organization does not exist.
	ErrInvalidOrganization = errors.New("registry organization is invalid")
)

const (
	// MaxPageSize bounds every tenant-scoped device list query.
	MaxPageSize      = 100
	minTextSize      = 1
	maxNameSize      = 200
	maxDeviceKeySize = 128
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Device is the consumer-safe internal record returned by the repository.
type Device struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	DeviceKey      string
	DisplayName    string
	CreatedAt      time.Time
}

// CreateDeviceInput contains the mutable fields accepted by device creation.
type CreateDeviceInput struct {
	DeviceKey   string
	DisplayName string
}

// Cursor is the stable continuation point for the tenant-scoped device list.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// DevicePage is a bounded list result. NextCursor is nil when the page is
// complete.
type DevicePage struct {
	Devices    []Device
	NextCursor *Cursor
}

// Repository owns organization and device persistence operations.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a repository over an owned PostgreSQL pool.
func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("registry repository requires a database pool")
	}
	return &Repository{pool: pool}, nil
}

// CreateOrganization creates a new organization and returns its generated ID.
func (r *Repository) CreateOrganization(ctx context.Context, slug string, displayName string) (uuid.UUID, error) {
	if err := validateOrganization(slug, displayName); err != nil {
		return uuid.Nil, err
	}

	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO organizations (slug, display_name)
		VALUES ($1, $2)
		RETURNING id
	`, slug, displayName).Scan(&id)
	if err != nil {
		return uuid.Nil, mapDatabaseError(err)
	}
	return id, nil
}

// EnsureOrganization creates the controlled development organization if it is
// absent and refuses to mutate an existing slug with a different name.
func (r *Repository) EnsureOrganization(ctx context.Context, slug string, displayName string) (uuid.UUID, error) {
	if err := validateOrganization(slug, displayName); err != nil {
		return uuid.Nil, err
	}

	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO organizations (slug, display_name)
		VALUES ($1, $2)
		ON CONFLICT (slug) DO NOTHING
		RETURNING id
	`, slug, displayName).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, mapDatabaseError(err)
	}

	var existingName string
	if err := r.pool.QueryRow(ctx, `
		SELECT id, display_name
		FROM organizations
		WHERE slug = $1
	`, slug).Scan(&id, &existingName); err != nil {
		return uuid.Nil, mapDatabaseError(err)
	}
	if existingName != displayName {
		return uuid.Nil, ErrConflict
	}
	return id, nil
}

// CreateDevice persists a device under the explicit organization scope.
func (r *Repository) CreateDevice(ctx context.Context, organizationID uuid.UUID, input CreateDeviceInput) (Device, error) {
	if organizationID == uuid.Nil {
		return Device{}, ErrInvalidOrganization
	}
	if err := validateDeviceInput(input); err != nil {
		return Device{}, err
	}

	var device Device
	err := r.pool.QueryRow(ctx, `
		INSERT INTO devices (organization_id, device_key, display_name)
		VALUES ($1, $2, $3)
		RETURNING id, organization_id, device_key, display_name, created_at
	`, organizationID, input.DeviceKey, input.DisplayName).Scan(
		&device.ID,
		&device.OrganizationID,
		&device.DeviceKey,
		&device.DisplayName,
		&device.CreatedAt,
	)
	if err != nil {
		mapped := mapDatabaseError(err)
		if errors.Is(mapped, ErrConflict) {
			return Device{}, ErrConflict
		}
		if errors.Is(mapped, ErrInvalidOrganization) {
			return Device{}, ErrInvalidOrganization
		}
		return Device{}, mapped
	}
	return device, nil
}

// GetDevice returns a device only inside the requested organization scope.
func (r *Repository) GetDevice(ctx context.Context, organizationID uuid.UUID, deviceID uuid.UUID) (Device, error) {
	if organizationID == uuid.Nil {
		return Device{}, ErrInvalidOrganization
	}
	if deviceID == uuid.Nil {
		return Device{}, ErrInvalidInput
	}

	var device Device
	err := r.pool.QueryRow(ctx, `
		SELECT id, organization_id, device_key, display_name, created_at
		FROM devices
		WHERE organization_id = $1 AND id = $2
	`, organizationID, deviceID).Scan(
		&device.ID,
		&device.OrganizationID,
		&device.DeviceKey,
		&device.DisplayName,
		&device.CreatedAt,
	)
	if err != nil {
		return Device{}, mapDatabaseError(err)
	}
	return device, nil
}

// ListDevices returns a deterministic, bounded tenant-scoped page.
func (r *Repository) ListDevices(ctx context.Context, organizationID uuid.UUID, pageSize int, cursor *Cursor) (DevicePage, error) {
	if organizationID == uuid.Nil {
		return DevicePage{}, ErrInvalidOrganization
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return DevicePage{}, fmt.Errorf("%w: page size must be between 1 and %d", ErrInvalidInput, MaxPageSize)
	}
	if err := validateCursor(cursor); err != nil {
		return DevicePage{}, err
	}

	query := `
		SELECT id, organization_id, device_key, display_name, created_at
		FROM devices
		WHERE organization_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`
	args := []any{organizationID, pageSize + 1}
	if cursor != nil {
		query = `
			SELECT id, organization_id, device_key, display_name, created_at
			FROM devices
			WHERE organization_id = $1
			  AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC
			LIMIT $4
		`
		args = []any{organizationID, cursor.CreatedAt, cursor.ID, pageSize + 1}
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return DevicePage{}, mapDatabaseError(err)
	}
	defer rows.Close()

	devices := make([]Device, 0, pageSize)
	for rows.Next() {
		var device Device
		if err := rows.Scan(
			&device.ID,
			&device.OrganizationID,
			&device.DeviceKey,
			&device.DisplayName,
			&device.CreatedAt,
		); err != nil {
			return DevicePage{}, fmt.Errorf("scan device: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return DevicePage{}, fmt.Errorf("read devices: %w", err)
	}

	page := DevicePage{Devices: devices}
	if len(devices) > pageSize {
		last := devices[pageSize-1]
		page.Devices = devices[:pageSize]
		page.NextCursor = &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

func validateCursor(cursor *Cursor) error {
	if cursor != nil && (cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero()) {
		return ErrInvalidInput
	}
	return nil
}

func validateOrganization(slug string, displayName string) error {
	if !utf8.ValidString(slug) || !slugPattern.MatchString(slug) || utf8.RuneCountInString(slug) > 63 {
		return fmt.Errorf("%w: organization slug", ErrInvalidInput)
	}
	if !utf8.ValidString(displayName) {
		return fmt.Errorf("%w: organization display name", ErrInvalidInput)
	}
	if size := utf8.RuneCountInString(strings.TrimSpace(displayName)); size < minTextSize || size > maxNameSize {
		return fmt.Errorf("%w: organization display name", ErrInvalidInput)
	}
	return nil
}

func validateDeviceInput(input CreateDeviceInput) error {
	if !utf8.ValidString(input.DeviceKey) || input.DeviceKey == "" || input.DeviceKey != strings.TrimSpace(input.DeviceKey) || utf8.RuneCountInString(input.DeviceKey) > maxDeviceKeySize {
		return fmt.Errorf("%w: device key", ErrInvalidInput)
	}
	if !utf8.ValidString(input.DisplayName) {
		return fmt.Errorf("%w: device display name", ErrInvalidInput)
	}
	if size := utf8.RuneCountInString(strings.TrimSpace(input.DisplayName)); size < minTextSize || size > maxNameSize {
		return fmt.Errorf("%w: device display name", ErrInvalidInput)
	}
	return nil
}

func mapDatabaseError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	if pgError, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgError.Code {
		case "23505":
			return ErrConflict
		case "23503":
			return ErrInvalidOrganization
		case "23514", "23502":
			return ErrInvalidInput
		}
	}
	return err
}
