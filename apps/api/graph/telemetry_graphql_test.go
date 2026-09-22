package graph

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/graph/model"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

type fakeTelemetryRepository struct {
	mu             sync.Mutex
	organizationID uuid.UUID
	deviceID       uuid.UUID
	state          projection.CurrentState
	stateErr       error
	page           projection.TelemetryPage
	pageErr        error
	lastOrg        uuid.UUID
	lastPageSize   int
	lastCursor     *projection.Cursor
}

func (f *fakeTelemetryRepository) GetCurrentState(_ context.Context, organizationID, deviceID uuid.UUID) (projection.CurrentState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOrg = organizationID
	if f.stateErr != nil {
		return projection.CurrentState{}, f.stateErr
	}
	if organizationID != f.organizationID || deviceID != f.deviceID {
		return projection.CurrentState{}, projection.ErrNotFound
	}
	return f.state, nil
}

func (f *fakeTelemetryRepository) ListTelemetry(_ context.Context, organizationID, deviceID uuid.UUID, pageSize int, cursor *projection.Cursor) (projection.TelemetryPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOrg = organizationID
	f.lastPageSize = pageSize
	f.lastCursor = cursor
	if f.pageErr != nil {
		return projection.TelemetryPage{}, f.pageErr
	}
	if organizationID != f.organizationID || deviceID != f.deviceID {
		return projection.TelemetryPage{}, nil
	}
	return f.page, nil
}

func TestGraphQLTelemetryContractAndTenantScope(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	messageID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	observedAt := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	telemetryRepository := &fakeTelemetryRepository{
		organizationID: organizationID,
		deviceID:       deviceID,
		state: projection.CurrentState{
			DeviceID:           deviceID,
			MessageID:          messageID,
			ObservedAt:         observedAt,
			ReceivedAt:         observedAt.Add(time.Second),
			TemperatureCelsius: 23.5,
			LastSeenAt:         observedAt.Add(2 * time.Second),
		},
		page: projection.TelemetryPage{
			Points: []projection.TelemetryPoint{{
				MessageID:          messageID,
				ObservedAt:         observedAt,
				ReceivedAt:         observedAt.Add(time.Second),
				TemperatureCelsius: 23.5,
			}},
		},
	}
	deviceRepository := &fakeRepository{
		organizationID: organizationID,
		devices: []registry.Device{{
			ID:             deviceID,
			OrganizationID: organizationID,
			DeviceKey:      "sensor-1",
			DisplayName:    "Temperature",
			CreatedAt:      observedAt,
		}},
	}
	handler, err := NewHandlerWithTelemetry(deviceRepository, telemetryRepository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandlerWithTelemetry returned error: %v", err)
	}

	response := doGraphQL(t, handler, `{ "query": "query { deviceCurrentState(deviceId: \"22222222-2222-4222-8222-222222222222\") { messageId observedAt receivedAt temperatureCelsius lastSeenAt } deviceTelemetry(deviceId: \"22222222-2222-4222-8222-222222222222\", first: 2) { edges { cursor node { messageId observedAt receivedAt temperatureCelsius } } pageInfo { endCursor hasNextPage } } }" }`, "")
	if len(response.Errors) != 0 {
		t.Fatalf("telemetry GraphQL errors = %+v", response.Errors)
	}
	var data struct {
		DeviceCurrentState *model.DeviceCurrentState `json:"deviceCurrentState"`
		DeviceTelemetry    struct {
			Edges []struct {
				Cursor string               `json:"cursor"`
				Node   model.TelemetryPoint `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				EndCursor   *string `json:"endCursor"`
				HasNextPage bool    `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"deviceTelemetry"`
	}
	decodeData(t, response, &data)
	if data.DeviceCurrentState == nil || data.DeviceCurrentState.MessageID != messageID.String() || data.DeviceCurrentState.TemperatureCelsius != 23.5 || data.DeviceCurrentState.LastSeenAt.IsZero() {
		t.Fatalf("current state data = %+v", data.DeviceCurrentState)
	}
	if len(data.DeviceTelemetry.Edges) != 1 || data.DeviceTelemetry.Edges[0].Node.MessageID != messageID.String() || data.DeviceTelemetry.Edges[0].Cursor == "" || data.DeviceTelemetry.PageInfo.HasNextPage {
		t.Fatalf("telemetry data = %+v", data.DeviceTelemetry)
	}
	if telemetryRepository.lastOrg != organizationID || telemetryRepository.lastPageSize != 2 {
		t.Fatalf("telemetry repository scope/page = %s/%d", telemetryRepository.lastOrg, telemetryRepository.lastPageSize)
	}

	foreign := doGraphQL(t, handler, `{ "query": "query { deviceCurrentState(deviceId: \"44444444-4444-4444-8444-444444444444\") { messageId } deviceTelemetry(deviceId: \"44444444-4444-4444-8444-444444444444\") { edges { node { messageId } } } }" }`, "")
	if len(foreign.Errors) != 0 || string(foreign.Data) == "null" {
		t.Fatalf("foreign telemetry response = %+v", foreign)
	}
	if string(foreign.Data) == `{"deviceCurrentState":{"messageId":"`+messageID.String()+`"}` {
		t.Fatal("foreign telemetry response leaked current state")
	}
}

func TestGraphQLTelemetryBoundsAndCursorType(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	telemetryRepository := &fakeTelemetryRepository{organizationID: organizationID, deviceID: deviceID}
	deviceRepository := &fakeRepository{organizationID: organizationID}
	handler, err := NewHandlerWithTelemetry(deviceRepository, telemetryRepository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandlerWithTelemetry returned error: %v", err)
	}

	overLimit := doGraphQL(t, handler, `{ "query": "query { deviceTelemetry(deviceId: \"22222222-2222-4222-8222-222222222222\", first: 101) { edges { cursor } } }" }`, "")
	assertErrorCode(t, overLimit, errorCodeBadUserInput)
	if telemetryRepository.lastPageSize != 0 {
		t.Fatalf("repository called for over-limit query with page size %d", telemetryRepository.lastPageSize)
	}

	deviceCursor := base64.RawURLEncoding.EncodeToString([]byte("v1|2026-09-22T04:00:00Z|33333333-3333-4333-8333-333333333333"))
	wrongCursor := doGraphQL(t, handler, `{ "query": "query { deviceTelemetry(deviceId: \"22222222-2222-4222-8222-222222222222\", after: \"`+deviceCursor+`\") { edges { cursor } } }" }`, "")
	assertErrorCode(t, wrongCursor, errorCodeBadUserInput)
	if telemetryRepository.lastCursor != nil {
		t.Fatalf("repository called with wrong cursor type: %+v", telemetryRepository.lastCursor)
	}

}

func TestGraphQLTelemetryRepositoryFailureDoesNotLeakDetails(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	telemetryRepository := &fakeTelemetryRepository{
		organizationID: organizationID,
		deviceID:       deviceID,
		stateErr:       errors.New("sql: password=secret telemetry failure"),
	}
	handler, err := NewHandlerWithTelemetry(&fakeRepository{organizationID: organizationID}, telemetryRepository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandlerWithTelemetry returned error: %v", err)
	}
	response := doGraphQL(t, handler, `{ "query": "query { deviceCurrentState(deviceId: \"22222222-2222-4222-8222-222222222222\") { messageId } }" }`, "")
	assertErrorCode(t, response, errorCodeInternal)
	if len(response.Errors) > 0 && (response.Errors[0].Message == "sql: password=secret telemetry failure" || strings.Contains(response.Errors[0].Message, "secret")) {
		t.Fatalf("telemetry repository error leaked: %+v", response.Errors[0])
	}
}

var _ TelemetryRepository = (*fakeTelemetryRepository)(nil)
