package ingestion

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type parsedTopic struct {
	TenantSlug string
	DeviceID   uuid.UUID
}

var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type telemetryPayload struct {
	MessageID          uuid.UUID
	ObservedAt         time.Time
	TemperatureCelsius float64
}

func parseTopic(raw string) (parsedTopic, error) {
	if raw == "" || !utf8.ValidString(raw) {
		return parsedTopic{}, errors.New("topic is empty or invalid UTF-8")
	}
	parts := strings.Split(raw, "/")
	if len(parts) != 7 || parts[0] != "pulsegrid" || parts[1] != "v1" || parts[2] != "tenants" || parts[4] != "devices" || parts[6] != "telemetry" {
		return parsedTopic{}, errors.New("topic shape is not telemetry v1")
	}
	if parts[3] == "" || strings.ContainsAny(parts[3], "+#") || !tenantSlugPattern.MatchString(parts[3]) || len(parts[3]) > 63 || parts[5] == "" || strings.ContainsAny(parts[5], "+#") {
		return parsedTopic{}, errors.New("topic identifiers are invalid")
	}
	deviceID, err := uuid.Parse(parts[5])
	if err != nil || deviceID == uuid.Nil || deviceID.String() != parts[5] {
		return parsedTopic{}, errors.New("topic device ID is not a canonical lowercase UUID")
	}
	return parsedTopic{TenantSlug: parts[3], DeviceID: deviceID}, nil
}

func decodeTelemetry(raw []byte) (telemetryPayload, error) {
	fields, err := decodeObject(raw)
	if err != nil {
		return telemetryPayload{}, err
	}
	expected := map[string]struct{}{
		"schemaVersion":      {},
		"messageId":          {},
		"observedAt":         {},
		"temperatureCelsius": {},
	}
	for key := range fields {
		if _, ok := expected[key]; !ok {
			return telemetryPayload{}, errors.New("payload contains an unknown field")
		}
	}
	for key := range expected {
		if _, ok := fields[key]; !ok {
			return telemetryPayload{}, errors.New("payload is missing a required field")
		}
	}

	var schemaVersion int
	if err := json.Unmarshal(fields["schemaVersion"], &schemaVersion); err != nil || schemaVersion != TelemetrySchemaVersion {
		return telemetryPayload{}, errors.New("payload schema version is unsupported")
	}

	var rawMessageID string
	if err := json.Unmarshal(fields["messageId"], &rawMessageID); err != nil {
		return telemetryPayload{}, errors.New("payload message ID is not a string")
	}
	messageID, err := uuid.Parse(rawMessageID)
	if err != nil || messageID == uuid.Nil || messageID.String() != rawMessageID {
		return telemetryPayload{}, errors.New("payload message ID is not a canonical lowercase UUID")
	}

	var rawObservedAt string
	if err := json.Unmarshal(fields["observedAt"], &rawObservedAt); err != nil || !strings.HasSuffix(rawObservedAt, "Z") {
		return telemetryPayload{}, errors.New("payload observedAt is not a UTC RFC3339 timestamp")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, rawObservedAt)
	if err != nil || observedAt.IsZero() {
		return telemetryPayload{}, errors.New("payload observedAt is not a valid RFC3339 timestamp")
	}

	temperature, err := decodeFiniteNumber(fields["temperatureCelsius"])
	if err != nil {
		return telemetryPayload{}, errors.New("payload temperature is not a finite JSON number")
	}

	return telemetryPayload{
		MessageID:          messageID,
		ObservedAt:         observedAt.UTC(),
		TemperatureCelsius: temperature,
	}, nil
}

func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, errors.New("payload is not a JSON object")
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, errors.New("payload is not a JSON object")
	}

	fields := make(map[string]json.RawMessage, 4)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, errors.New("payload object key is invalid")
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("payload object key is invalid")
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("payload contains a duplicate field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("payload field value is invalid")
		}
		fields[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, errors.New("payload object is not closed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("payload contains trailing data")
	}
	return fields, nil
}

func decodeFiniteNumber(raw []byte) (float64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return 0, err
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, errors.New("value is not a number")
	}
	parsed, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, errors.New("number is not finite")
	}
	return parsed, nil
}

func ensureDecoderEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("value contains trailing data")
	}
	return nil
}

func validUTF8(raw []byte) bool {
	return utf8.Valid(raw)
}

func payloadReason(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "schema version"):
		return ReasonSchemaVersionUnsupported
	case strings.Contains(message, "message ID"):
		return ReasonMessageIDInvalid
	case strings.Contains(message, "observedAt"):
		return ReasonObservedAtInvalid
	case strings.Contains(message, "temperature"):
		return ReasonTemperatureInvalid
	default:
		return ReasonPayloadMalformed
	}
}
