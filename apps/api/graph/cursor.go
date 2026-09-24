package graph

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/rules"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

const cursorVersion = "v1"
const telemetryCursorVersion = "telemetry-v1"
const alertCursorVersion = "alerts-v1"

func encodeCursor(cursor registry.Cursor) (string, error) {
	if cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return "", newPublicError(errorCodeInternal, "device cursor is unavailable", registry.ErrInvalidInput)
	}
	payload := strings.Join([]string{
		cursorVersion,
		cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		cursor.ID.String(),
	}, "|")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	if len(encoded) > maxCursorSize {
		return "", newPublicError(errorCodeInternal, "device cursor is unavailable", registry.ErrInvalidInput)
	}
	return encoded, nil
}

func decodeCursor(raw string) (*registry.Cursor, error) {
	if raw == "" || len(raw) > maxCursorSize {
		return nil, protocolError("after must be a valid cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) > maxCursorSize {
		return nil, protocolError("after must be a valid cursor")
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 || parts[0] != cursorVersion {
		return nil, protocolError("after must be a valid cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil || createdAt.UTC().Format(time.RFC3339Nano) != parts[1] {
		return nil, protocolError("after must be a valid cursor")
	}
	deviceID, err := uuid.Parse(parts[2])
	if err != nil || deviceID == uuid.Nil || deviceID.String() != parts[2] {
		return nil, protocolError("after must be a valid cursor")
	}
	return &registry.Cursor{CreatedAt: createdAt.UTC(), ID: deviceID}, nil
}

func encodeTelemetryCursor(cursor projection.Cursor) (string, error) {
	if cursor.MessageID == uuid.Nil || cursor.ObservedAt.IsZero() {
		return "", newPublicError(errorCodeInternal, "telemetry cursor is unavailable", registry.ErrInvalidInput)
	}
	payload := strings.Join([]string{
		telemetryCursorVersion,
		cursor.ObservedAt.UTC().Format(time.RFC3339Nano),
		cursor.MessageID.String(),
	}, "|")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	if len(encoded) > maxCursorSize {
		return "", newPublicError(errorCodeInternal, "telemetry cursor is unavailable", registry.ErrInvalidInput)
	}
	return encoded, nil
}

func decodeTelemetryCursor(raw string) (*projection.Cursor, error) {
	if raw == "" || len(raw) > maxCursorSize {
		return nil, protocolError("after must be a valid telemetry cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) > maxCursorSize {
		return nil, protocolError("after must be a valid telemetry cursor")
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 3 || parts[0] != telemetryCursorVersion {
		return nil, protocolError("after must be a valid telemetry cursor")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil || observedAt.UTC().Format(time.RFC3339Nano) != parts[1] {
		return nil, protocolError("after must be a valid telemetry cursor")
	}
	messageID, err := uuid.Parse(parts[2])
	if err != nil || messageID == uuid.Nil || messageID.String() != parts[2] {
		return nil, protocolError("after must be a valid telemetry cursor")
	}
	return &projection.Cursor{ObservedAt: observedAt.UTC(), MessageID: messageID}, nil
}

func encodeAlertCursor(cursor rules.AlertCursor) (string, error) {
	if cursor.ID == uuid.Nil || cursor.OrganizationID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return "", newPublicError(errorCodeInternal, "alert cursor is unavailable", rules.ErrInvalidInput)
	}
	deviceFilter := "-"
	if cursor.DeviceFilter != nil {
		if *cursor.DeviceFilter == uuid.Nil {
			return "", newPublicError(errorCodeInternal, "alert cursor is unavailable", rules.ErrInvalidInput)
		}
		deviceFilter = cursor.DeviceFilter.String()
	}
	payload := strings.Join([]string{
		alertCursorVersion,
		cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		cursor.ID.String(),
		cursor.OrganizationID.String(),
		deviceFilter,
	}, "|")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	if len(encoded) > maxCursorSize {
		return "", newPublicError(errorCodeInternal, "alert cursor is unavailable", rules.ErrInvalidInput)
	}
	return encoded, nil
}

func decodeAlertCursor(raw string) (*rules.AlertCursor, error) {
	if raw == "" || len(raw) > maxCursorSize {
		return nil, protocolError("after must be a valid alert cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) > maxCursorSize {
		return nil, protocolError("after must be a valid alert cursor")
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 5 || parts[0] != alertCursorVersion {
		return nil, protocolError("after must be a valid alert cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil || createdAt.UTC().Format(time.RFC3339Nano) != parts[1] {
		return nil, protocolError("after must be a valid alert cursor")
	}
	alertID, err := uuid.Parse(parts[2])
	if err != nil || alertID == uuid.Nil || alertID.String() != parts[2] {
		return nil, protocolError("after must be a valid alert cursor")
	}
	organizationID, err := uuid.Parse(parts[3])
	if err != nil || organizationID == uuid.Nil || organizationID.String() != parts[3] {
		return nil, protocolError("after must be a valid alert cursor")
	}
	var deviceFilter *uuid.UUID
	if parts[4] != "-" {
		parsedDeviceID, parseErr := uuid.Parse(parts[4])
		if parseErr != nil || parsedDeviceID == uuid.Nil || parsedDeviceID.String() != parts[4] {
			return nil, protocolError("after must be a valid alert cursor")
		}
		deviceFilter = &parsedDeviceID
	}
	return &rules.AlertCursor{
		CreatedAt:      createdAt.UTC(),
		ID:             alertID,
		OrganizationID: organizationID,
		DeviceFilter:   deviceFilter,
	}, nil
}
