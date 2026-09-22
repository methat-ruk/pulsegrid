package graph

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

func TestCursorRoundTripIsOpaqueAndCanonical(t *testing.T) {
	want := registry.Cursor{
		CreatedAt: time.Date(2026, 9, 16, 8, 15, 0, 123456789, time.UTC),
		ID:        uuid.MustParse("22222222-2222-4222-8222-222222222222"),
	}
	encoded, err := encodeCursor(want)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	if encoded == "" || strings.Contains(encoded, want.ID.String()) || strings.Contains(encoded, "v1|") {
		t.Fatalf("cursor is not opaque: %q", encoded)
	}
	got, err := decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if *got != want {
		t.Fatalf("decoded cursor = %+v, want %+v", *got, want)
	}
}

func TestCursorRejectsMalformedVersionsTimestampsAndUUIDs(t *testing.T) {
	for _, raw := range []string{
		"",
		"not-base64",
		encodeRawCursorForTest(t, "v2|2026-09-16T08:15:00Z|22222222-2222-4222-8222-222222222222"),
		encodeRawCursorForTest(t, "v1|2026-09-16T08:15:00+07:00|22222222-2222-4222-8222-222222222222"),
		encodeRawCursorForTest(t, "v1|not-time|22222222-2222-4222-8222-222222222222"),
		encodeRawCursorForTest(t, "v1|2026-09-16T08:15:00Z|not-uuid"),
	} {
		if _, err := decodeCursor(raw); err == nil || !errors.Is(err, registry.ErrInvalidInput) {
			t.Fatalf("decodeCursor(%q) error = %v, want ErrInvalidInput", raw, err)
		}
	}
	if _, err := decodeCursor(strings.Repeat("A", maxCursorSize+1)); err == nil {
		t.Fatal("decodeCursor accepted oversized cursor")
	}
}

func TestTelemetryCursorRoundTripUsesSeparateNamespace(t *testing.T) {
	want := projection.Cursor{
		ObservedAt: time.Date(2026, 9, 22, 8, 15, 0, 123456789, time.UTC),
		MessageID:  uuid.MustParse("33333333-3333-4333-8333-333333333333"),
	}
	encoded, err := encodeTelemetryCursor(want)
	if err != nil {
		t.Fatalf("encode telemetry cursor: %v", err)
	}
	if encoded == "" || strings.Contains(encoded, want.MessageID.String()) || strings.Contains(encoded, "telemetry-v1|") {
		t.Fatalf("telemetry cursor is not opaque: %q", encoded)
	}
	got, err := decodeTelemetryCursor(encoded)
	if err != nil {
		t.Fatalf("decode telemetry cursor: %v", err)
	}
	if *got != want {
		t.Fatalf("decoded telemetry cursor = %+v, want %+v", *got, want)
	}
}

func TestTelemetryCursorRejectsDeviceNamespaceAndMalformedValues(t *testing.T) {
	for _, raw := range []string{
		encodeRawCursorForTest(t, "v1|2026-09-22T08:15:00Z|33333333-3333-4333-8333-333333333333"),
		encodeRawCursorForTest(t, "telemetry-v2|2026-09-22T08:15:00Z|33333333-3333-4333-8333-333333333333"),
		encodeRawCursorForTest(t, "telemetry-v1|2026-09-22T08:15:00+07:00|33333333-3333-4333-8333-333333333333"),
		encodeRawCursorForTest(t, "telemetry-v1|not-time|33333333-3333-4333-8333-333333333333"),
		encodeRawCursorForTest(t, "telemetry-v1|2026-09-22T08:15:00Z|not-uuid"),
	} {
		if _, err := decodeTelemetryCursor(raw); err == nil || !errors.Is(err, registry.ErrInvalidInput) {
			t.Fatalf("decodeTelemetryCursor(%q) error = %v, want ErrInvalidInput", raw, err)
		}
	}
}

func encodeRawCursorForTest(t *testing.T, raw string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}
