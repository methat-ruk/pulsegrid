package mqttcontract

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCommandContractRoundTripAndRejectsMalformedPayloads(t *testing.T) {
	id := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	created := time.Date(2026, 10, 1, 3, 0, 0, 123456000, time.UTC)
	expires := created.Add(CommandLifetime)
	payload, err := EncodeCommand(id, CommandTypePing, created, expires)
	if err != nil {
		t.Fatalf("EncodeCommand: %v", err)
	}
	command, err := DecodeCommand(payload)
	if err != nil {
		t.Fatalf("DecodeCommand: %v", err)
	}
	if command.CommandID != id || command.Type != CommandTypePing || !command.CreatedAt.Equal(created) || !command.ExpiresAt.Equal(expires) {
		t.Fatalf("decoded command = %#v", command)
	}

	bad := []string{
		`{"schemaVersion":1,"schemaVersion":1,"commandId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","type":"PING","createdAt":"2026-10-01T03:00:00.123456Z","expiresAt":"2026-10-01T03:02:00.123456Z"}`,
		strings.Replace(string(payload), `"type":"PING"`, `"type":"PING","unexpected":true`, 1),
		strings.Replace(string(payload), "\"type\":\"PING\"", "\"type\":\"PONG\"", 1),
		strings.Replace(string(payload), "\"schemaVersion\":1", "\"schemaVersion\":2", 1),
		strings.Replace(string(payload), "\"commandId\":\"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\"", "\"commandId\":\"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA\"", 1),
		strings.Replace(string(payload), "\"expiresAt\":\"2026-10-01T03:02:00.123456Z\"", "\"expiresAt\":\"2026-10-01T03:03:00.123456Z\"", 1),
		strings.Replace(string(payload), "\"createdAt\":\"2026-10-01T03:00:00.123456Z\"", "\"createdAt\":\"2026-10-01T03:00:00.123456+00:00\"", 1),
		string(payload) + ` {}`,
		`null`,
	}
	for index, raw := range bad {
		t.Run(time.Duration(index).String(), func(t *testing.T) {
			if _, err := DecodeCommand([]byte(raw)); !errors.Is(err, ErrPayloadInvalid) {
				t.Fatalf("DecodeCommand error = %v, want ErrPayloadInvalid", err)
			}
		})
	}
	limitPayload := append([]byte(nil), payload[:len(payload)-1]...)
	limitPayload = append(limitPayload, strings.Repeat(" ", MaxPayloadBytes-len(payload))...)
	limitPayload = append(limitPayload, '}')
	if len(limitPayload) != MaxPayloadBytes {
		t.Fatalf("boundary payload size = %d, want %d", len(limitPayload), MaxPayloadBytes)
	}
	if _, err := DecodeCommand(limitPayload); err != nil {
		t.Fatalf("DecodeCommand rejected valid %d-byte boundary: %v", MaxPayloadBytes, err)
	}
	invalidUTF8 := append([]byte(nil), payload...)
	invalidUTF8[1] = 0xff
	if _, err := DecodeCommand(invalidUTF8); !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("invalid UTF-8 error = %v, want ErrPayloadInvalid", err)
	}
	if _, err := DecodeCommand([]byte(strings.Repeat("x", MaxPayloadBytes+1))); !errors.Is(err, ErrPayloadInvalid) {
		t.Fatalf("oversized payload error = %v", err)
	}
}

func TestResponseContractRequiresOutcomeSpecificFailureCode(t *testing.T) {
	id := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	for _, test := range []struct {
		outcome Outcome
		code    string
		valid   bool
	}{
		{OutcomeAck, "", true},
		{OutcomeCompleted, "", true},
		{OutcomeFailed, DeviceFailureCode, true},
		{OutcomeAck, DeviceFailureCode, false},
		{OutcomeCompleted, DeviceFailureCode, false},
		{OutcomeFailed, "", false},
		{Outcome("UNKNOWN"), "", false},
	} {
		payload, err := EncodeResponse(id, test.outcome, test.code)
		if (err == nil) != test.valid {
			t.Fatalf("EncodeResponse(%q, %q) error = %v", test.outcome, test.code, err)
		}
		if !test.valid {
			continue
		}
		decoded, err := DecodeResponse(payload)
		if err != nil || decoded.Outcome != test.outcome || decoded.FailureCode != test.code {
			t.Fatalf("DecodeResponse = %#v, %v", decoded, err)
		}
	}
	for _, raw := range []string{
		`{"schemaVersion":1,"commandId":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","outcome":"ACK","extra":true}`,
		`{"schemaVersion":1,"commandId":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","outcome":"ACK","failureCode":""}`,
		`{"schemaVersion":1,"commandId":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","outcome":"COMPLETED","failureCode":""}`,
		`{"schemaVersion":1,"commandId":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","outcome":"ACK","failureCode":null}`,
		`{"schemaVersion":1,"commandId":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","outcome":"FAILED"}`,
		`{"schemaVersion":1,"commandId":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","outcome":"ACK","outcome":"COMPLETED"}`,
	} {
		if _, err := DecodeResponse([]byte(raw)); !errors.Is(err, ErrPayloadInvalid) {
			t.Fatalf("DecodeResponse(%s) error = %v", raw, err)
		}
	}
}

func TestCommandTopicsRequireCanonicalDeviceIdentity(t *testing.T) {
	id := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	command, err := CommandTopic("pulsegrid-dev", id)
	if err != nil || command != "pulsegrid/v1/tenants/pulsegrid-dev/devices/cccccccc-cccc-4ccc-8ccc-cccccccccccc/commands" {
		t.Fatalf("CommandTopic = %q, %v", command, err)
	}
	response, err := ResponseTopic("pulsegrid-dev", id)
	if err != nil || response != "pulsegrid/v1/tenants/pulsegrid-dev/devices/cccccccc-cccc-4ccc-8ccc-cccccccccccc/command-responses" {
		t.Fatalf("ResponseTopic = %q, %v", response, err)
	}
	filter, err := ResponseFilter("pulsegrid-dev")
	if err != nil || filter != "pulsegrid/v1/tenants/pulsegrid-dev/devices/+/command-responses" {
		t.Fatalf("ResponseFilter = %q, %v", filter, err)
	}
	for _, topic := range []string{
		"pulsegrid/v1/tenants/pulsegrid-dev/devices/+/command-responses",
		"pulsegrid/v1/tenants/pulsegrid-dev/devices/CCCCCCCC-CCCC-4CCC-8CCC-CCCCCCCCCCCC/command-responses",
		"pulsegrid/v1/tenants/bad//devices/cccccccc-cccc-4ccc-8ccc-cccccccccccc/command-responses",
	} {
		if _, _, err := ParseDeviceTopic(topic, ResponseSuffix); !errors.Is(err, ErrTopicInvalid) {
			t.Fatalf("ParseDeviceTopic(%q) error = %v", topic, err)
		}
	}
}
