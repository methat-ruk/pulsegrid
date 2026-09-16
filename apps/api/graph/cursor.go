package graph

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
)

const cursorVersion = "v1"

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
