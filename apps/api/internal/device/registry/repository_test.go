package registry

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNewRepositoryRejectsNilPool(t *testing.T) {
	if _, err := NewRepository(nil); err == nil {
		t.Fatal("NewRepository accepted a nil pool")
	}
}

func TestValidateOrganization(t *testing.T) {
	tests := []struct {
		name        string
		slug        string
		displayName string
		wantErr     bool
	}{
		{name: "valid", slug: "pulsegrid-dev", displayName: "Pulsegrid Development"},
		{name: "uppercase slug", slug: "Pulsegrid", displayName: "Pulsegrid", wantErr: true},
		{name: "leading hyphen", slug: "-pulsegrid", displayName: "Pulsegrid", wantErr: true},
		{name: "blank name", slug: "pulsegrid", displayName: "   ", wantErr: true},
		{name: "long slug", slug: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", displayName: "Pulsegrid", wantErr: true},
		{name: "invalid display name encoding", slug: "pulsegrid", displayName: string([]byte{0xff}), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateOrganization(test.slug, test.displayName)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateOrganization error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestValidateDeviceInput(t *testing.T) {
	tests := []struct {
		name  string
		input CreateDeviceInput
		valid bool
	}{
		{name: "valid", input: CreateDeviceInput{DeviceKey: "sensor-01", DisplayName: "Temperature"}, valid: true},
		{name: "blank key", input: CreateDeviceInput{DeviceKey: "", DisplayName: "Temperature"}},
		{name: "surrounding whitespace key", input: CreateDeviceInput{DeviceKey: " sensor-01", DisplayName: "Temperature"}},
		{name: "blank display name", input: CreateDeviceInput{DeviceKey: "sensor-01", DisplayName: "  "}},
		{name: "long key", input: CreateDeviceInput{DeviceKey: string(make([]byte, maxDeviceKeySize+1)), DisplayName: "Temperature"}},
		{name: "invalid key encoding", input: CreateDeviceInput{DeviceKey: string([]byte{0xff}), DisplayName: "Temperature"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateDeviceInput(test.input)
			if (err == nil) != test.valid {
				t.Fatalf("validateDeviceInput error = %v, valid %v", err, test.valid)
			}
			if !test.valid && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestMapDatabaseError(t *testing.T) {
	tests := []struct {
		code string
		want error
	}{
		{code: "23505", want: ErrConflict},
		{code: "23503", want: ErrInvalidOrganization},
		{code: "23514", want: ErrInvalidInput},
		{code: "23502", want: ErrInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			err := mapDatabaseError(&pgconn.PgError{Code: test.code})
			if !errors.Is(err, test.want) {
				t.Fatalf("mapped error = %v, want %v", err, test.want)
			}
		})
	}
	if !errors.Is(mapDatabaseError(pgx.ErrNoRows), ErrNotFound) {
		t.Fatal("pgx.ErrNoRows did not map to ErrNotFound")
	}
}

func TestListCursorRejectsNilUUID(t *testing.T) {
	if err := validateCursor(&Cursor{ID: uuid.Nil}); err == nil {
		t.Fatal("validateCursor accepted a nil UUID")
	}
}
