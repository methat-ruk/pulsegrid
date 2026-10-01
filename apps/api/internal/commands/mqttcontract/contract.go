// Package mqttcontract owns the versioned command and device-response wire
// contract shared by the API transport and the standalone device simulator.
package mqttcontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	SchemaVersion      = 1
	MaxPayloadBytes    = 1024
	CommandTypePing    = "PING"
	CommandLifetime    = 2 * time.Minute
	DeviceFailureCode  = "DEVICE_REPORTED_FAILURE"
	CommandTopicSuffix = "commands"
	ResponseSuffix     = "command-responses"
)

type Outcome string

const (
	OutcomeAck       Outcome = "ACK"
	OutcomeCompleted Outcome = "COMPLETED"
	OutcomeFailed    Outcome = "FAILED"
)

type Command struct {
	SchemaVersion int       `json:"schemaVersion"`
	CommandID     uuid.UUID `json:"commandId"`
	Type          string    `json:"type"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type Response struct {
	SchemaVersion int       `json:"schemaVersion"`
	CommandID     uuid.UUID `json:"commandId"`
	Outcome       Outcome   `json:"outcome"`
	FailureCode   string    `json:"failureCode,omitempty"`
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var (
	ErrPayloadInvalid = errors.New("command MQTT payload is invalid")
	ErrTopicInvalid   = errors.New("command MQTT topic is invalid")
)

func CommandTopic(tenant string, deviceID uuid.UUID) (string, error) {
	if !validIdentity(tenant, deviceID) {
		return "", ErrTopicInvalid
	}
	return fmt.Sprintf("pulsegrid/v1/tenants/%s/devices/%s/%s", tenant, deviceID, CommandTopicSuffix), nil
}

func ResponseTopic(tenant string, deviceID uuid.UUID) (string, error) {
	if !validIdentity(tenant, deviceID) {
		return "", ErrTopicInvalid
	}
	return fmt.Sprintf("pulsegrid/v1/tenants/%s/devices/%s/%s", tenant, deviceID, ResponseSuffix), nil
}

func ResponseFilter(tenant string) (string, error) {
	if !validTenant(tenant) {
		return "", ErrTopicInvalid
	}
	return fmt.Sprintf("pulsegrid/v1/tenants/%s/devices/+/%s", tenant, ResponseSuffix), nil
}

func ParseDeviceTopic(topic, suffix string) (string, uuid.UUID, error) {
	parts := strings.Split(topic, "/")
	if len(parts) != 7 || parts[0] != "pulsegrid" || parts[1] != "v1" ||
		parts[2] != "tenants" || parts[4] != "devices" || parts[6] != suffix ||
		!validTenant(parts[3]) {
		return "", uuid.Nil, ErrTopicInvalid
	}
	deviceID, err := uuid.Parse(parts[5])
	if err != nil || deviceID == uuid.Nil || deviceID.String() != parts[5] {
		return "", uuid.Nil, ErrTopicInvalid
	}
	return parts[3], deviceID, nil
}

func EncodeCommand(commandID uuid.UUID, kind string, createdAt, expiresAt time.Time) ([]byte, error) {
	command := Command{
		SchemaVersion: SchemaVersion,
		CommandID:     commandID,
		Type:          kind,
		CreatedAt:     createdAt.UTC().Round(0),
		ExpiresAt:     expiresAt.UTC().Round(0),
	}
	if err := validateCommand(command); err != nil {
		return nil, err
	}
	return encodeBounded(command)
}

func DecodeCommand(raw []byte) (Command, error) {
	fields, err := decodeObject(raw, "schemaVersion", "commandId", "type", "createdAt", "expiresAt")
	if err != nil {
		return Command{}, err
	}
	var command Command
	if err := decodeField(fields, "schemaVersion", &command.SchemaVersion); err != nil {
		return Command{}, ErrPayloadInvalid
	}
	if err := decodeCanonicalUUID(fields, "commandId", &command.CommandID); err != nil {
		return Command{}, err
	}
	if err := decodeField(fields, "type", &command.Type); err != nil {
		return Command{}, ErrPayloadInvalid
	}
	if err := decodeUTCTime(fields, "createdAt", &command.CreatedAt); err != nil {
		return Command{}, err
	}
	if err := decodeUTCTime(fields, "expiresAt", &command.ExpiresAt); err != nil {
		return Command{}, err
	}
	if err := validateCommand(command); err != nil {
		return Command{}, err
	}
	return command, nil
}

func EncodeResponse(commandID uuid.UUID, outcome Outcome, failureCode string) ([]byte, error) {
	response := Response{SchemaVersion: SchemaVersion, CommandID: commandID, Outcome: outcome, FailureCode: failureCode}
	if err := validateResponse(response); err != nil {
		return nil, err
	}
	return encodeBounded(response)
}

func DecodeResponse(raw []byte) (Response, error) {
	fields, err := decodeObject(raw, "schemaVersion", "commandId", "outcome", "failureCode")
	if err != nil {
		return Response{}, err
	}
	var response Response
	if err := decodeField(fields, "schemaVersion", &response.SchemaVersion); err != nil {
		return Response{}, ErrPayloadInvalid
	}
	if err := decodeCanonicalUUID(fields, "commandId", &response.CommandID); err != nil {
		return Response{}, err
	}
	if err := decodeField(fields, "outcome", &response.Outcome); err != nil {
		return Response{}, ErrPayloadInvalid
	}
	rawFailure, failureCodePresent := fields["failureCode"]
	if failureCodePresent {
		if bytes.Equal(bytes.TrimSpace(rawFailure), []byte("null")) || decodeField(fields, "failureCode", &response.FailureCode) != nil {
			return Response{}, ErrPayloadInvalid
		}
	}
	if (response.Outcome == OutcomeAck || response.Outcome == OutcomeCompleted) && failureCodePresent {
		return Response{}, ErrPayloadInvalid
	}
	if response.Outcome == OutcomeFailed && !failureCodePresent {
		return Response{}, ErrPayloadInvalid
	}
	if err := validateResponse(response); err != nil {
		return Response{}, err
	}
	return response, nil
}

func validateCommand(command Command) error {
	if command.SchemaVersion != SchemaVersion || command.CommandID == uuid.Nil ||
		command.Type != CommandTypePing || command.CreatedAt.IsZero() ||
		command.ExpiresAt.IsZero() || !command.ExpiresAt.Equal(command.CreatedAt.Add(CommandLifetime)) ||
		!command.CreatedAt.Equal(command.CreatedAt.UTC()) || !command.ExpiresAt.Equal(command.ExpiresAt.UTC()) {
		return ErrPayloadInvalid
	}
	return nil
}

func validateResponse(response Response) error {
	if response.SchemaVersion != SchemaVersion || response.CommandID == uuid.Nil {
		return ErrPayloadInvalid
	}
	switch response.Outcome {
	case OutcomeAck, OutcomeCompleted:
		if response.FailureCode != "" {
			return ErrPayloadInvalid
		}
	case OutcomeFailed:
		if response.FailureCode != DeviceFailureCode {
			return ErrPayloadInvalid
		}
	default:
		return ErrPayloadInvalid
	}
	return nil
}

func encodeBounded(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > MaxPayloadBytes {
		return nil, ErrPayloadInvalid
	}
	return encoded, nil
}

func decodeObject(raw []byte, required ...string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > MaxPayloadBytes || !utf8.Valid(raw) {
		return nil, ErrPayloadInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrPayloadInvalid
	}
	expected := make(map[string]struct{}, len(required))
	for _, key := range required {
		expected[key] = struct{}{}
	}
	fields := make(map[string]json.RawMessage, len(required))
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		key, ok := keyToken.(string)
		if tokenErr != nil || !ok {
			return nil, ErrPayloadInvalid
		}
		if _, allowed := expected[key]; !allowed {
			return nil, ErrPayloadInvalid
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, ErrPayloadInvalid
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrPayloadInvalid
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(fields) < len(required)-1 {
		return nil, ErrPayloadInvalid
	}
	if extra, err := decoder.Token(); err != io.EOF || extra != nil {
		return nil, ErrPayloadInvalid
	}
	for _, key := range required {
		if key == "failureCode" {
			continue
		}
		if _, ok := fields[key]; !ok {
			return nil, ErrPayloadInvalid
		}
	}
	return fields, nil
}

func decodeField(fields map[string]json.RawMessage, key string, target any) error {
	raw, ok := fields[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrPayloadInvalid
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return ErrPayloadInvalid
	}
	return nil
}

func decodeCanonicalUUID(fields map[string]json.RawMessage, key string, target *uuid.UUID) error {
	var value string
	if err := decodeField(fields, key, &value); err != nil {
		return err
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value {
		return ErrPayloadInvalid
	}
	*target = parsed
	return nil
}

func decodeUTCTime(fields map[string]json.RawMessage, key string, target *time.Time) error {
	var value string
	if err := decodeField(fields, key, &value); err != nil || !strings.HasSuffix(value, "Z") {
		return ErrPayloadInvalid
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.IsZero() || parsed.Location() != time.UTC {
		return ErrPayloadInvalid
	}
	*target = parsed.Round(0)
	return nil
}

func validIdentity(tenant string, deviceID uuid.UUID) bool {
	return validTenant(tenant) && deviceID != uuid.Nil
}

func validTenant(tenant string) bool {
	return len(tenant) >= 1 && len(tenant) <= 63 && slugPattern.MatchString(tenant)
}
