package graph

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands"
)

type fakeCommandRepository struct {
	mu             sync.Mutex
	organizationID uuid.UUID
	deviceIDs      map[uuid.UUID]bool
	commands       map[uuid.UUID]commands.Command
	commandOrgs    map[uuid.UUID]uuid.UUID
	keys           map[uuid.UUID]uuid.UUID
	page           commands.Page
	createErr      error
	getErr         error
	listErr        error
	lastOrg        uuid.UUID
	lastInput      commands.CreateInput
	lastPageSize   int
	lastDeviceID   uuid.UUID
	lastCursor     *commands.Cursor
}

func (fake *fakeCommandRepository) Create(_ context.Context, organizationID uuid.UUID, input commands.CreateInput) (commands.Command, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastOrg = organizationID
	fake.lastInput = input
	if fake.createErr != nil {
		return commands.Command{}, fake.createErr
	}
	if organizationID != fake.organizationID || !fake.deviceIDs[input.DeviceID] {
		return commands.Command{}, commands.ErrNotFound
	}
	if priorID, ok := fake.keys[input.IdempotencyKey]; ok {
		prior := fake.commands[priorID]
		if prior.DeviceID != input.DeviceID || prior.Type != input.Type {
			return commands.Command{}, commands.ErrConflict
		}
		return prior, nil
	}
	now := time.Date(2026, 9, 29, 4, 0, 0, 123456000, time.UTC)
	command := commands.Command{
		ID: uuid.New(), DeviceID: input.DeviceID, Type: input.Type, Status: commands.StatusPending,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(commands.CommandLifetime),
	}
	fake.commands[command.ID] = command
	fake.commandOrgs[command.ID] = organizationID
	fake.keys[input.IdempotencyKey] = command.ID
	return command, nil
}

func (fake *fakeCommandRepository) Get(_ context.Context, organizationID, commandID uuid.UUID) (commands.Command, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastOrg = organizationID
	if fake.getErr != nil {
		return commands.Command{}, fake.getErr
	}
	command, ok := fake.commands[commandID]
	if !ok || fake.commandOrgs[commandID] != organizationID || organizationID != fake.organizationID {
		return commands.Command{}, commands.ErrNotFound
	}
	return command, nil
}

func (fake *fakeCommandRepository) List(_ context.Context, organizationID, deviceID uuid.UUID, pageSize int, cursor *commands.Cursor) (commands.Page, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.lastOrg = organizationID
	fake.lastDeviceID = deviceID
	fake.lastPageSize = pageSize
	fake.lastCursor = cursor
	if fake.listErr != nil {
		return commands.Page{}, fake.listErr
	}
	if organizationID != fake.organizationID || !fake.deviceIDs[deviceID] {
		return commands.Page{Commands: []commands.Command{}}, nil
	}
	start := 0
	if cursor != nil {
		for index, command := range fake.page.Commands {
			if command.ID == cursor.ID && command.CreatedAt.Equal(cursor.CreatedAt) {
				start = index + 1
				break
			}
		}
	}
	if start > len(fake.page.Commands) {
		start = len(fake.page.Commands)
	}
	end := start + pageSize
	end = min(end, len(fake.page.Commands))
	page := commands.Page{Commands: append([]commands.Command(nil), fake.page.Commands[start:end]...)}
	if end < len(fake.page.Commands) {
		last := page.Commands[len(page.Commands)-1]
		page.NextCursor = &commands.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

func TestGraphQLCommandCreateAndTenantScopedReads(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	foreignDeviceID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	commandID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	createdAt := time.Date(2026, 9, 29, 4, 0, 0, 123456000, time.UTC)
	command := commands.Command{
		ID: commandID, DeviceID: deviceID, Type: commands.TypePing, Status: commands.StatusPending,
		CreatedAt: createdAt, UpdatedAt: createdAt, ExpiresAt: createdAt.Add(commands.CommandLifetime),
	}
	second := command
	second.ID = uuid.MustParse("55555555-5555-4555-8555-555555555555")
	second.CreatedAt = createdAt.Add(-time.Second)
	second.UpdatedAt = second.CreatedAt
	second.ExpiresAt = second.CreatedAt.Add(commands.CommandLifetime)
	commandRepository := &fakeCommandRepository{
		organizationID: organizationID,
		deviceIDs:      map[uuid.UUID]bool{deviceID: true},
		commands:       map[uuid.UUID]commands.Command{commandID: command},
		commandOrgs:    map[uuid.UUID]uuid.UUID{commandID: organizationID},
		keys:           make(map[uuid.UUID]uuid.UUID),
		page:           commands.Page{Commands: []commands.Command{command}, NextCursor: &commands.Cursor{CreatedAt: command.CreatedAt, ID: command.ID}},
	}
	foreignCommandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	commandRepository.commands[foreignCommandID] = commands.Command{
		ID: foreignCommandID, DeviceID: foreignDeviceID, Type: commands.TypePing, Status: commands.StatusPending,
		CreatedAt: createdAt, UpdatedAt: createdAt, ExpiresAt: createdAt.Add(commands.CommandLifetime),
	}
	commandRepository.commandOrgs[foreignCommandID] = uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	commandRepository.page.Commands = append(commandRepository.page.Commands, second)
	handler, err := NewHandlerWithCommands(
		&fakeRepository{organizationID: organizationID}, nil, nil, commandRepository,
		organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewHandlerWithCommands returned error: %v", err)
	}

	key := "66666666-6666-4666-8666-666666666666"
	created := doGraphQL(t, handler, `{ "query": "mutation { createCommand(input: { deviceId: \"`+deviceID.String()+`\", type: PING, idempotencyKey: \"`+key+`\" }) { id deviceId type status createdAt expiresAt } }" }`, "")
	if len(created.Errors) != 0 {
		t.Fatalf("create command errors = %+v", created.Errors)
	}
	if commandRepository.lastOrg != organizationID || commandRepository.lastInput.RequestID != "test-request-001" || commandRepository.lastInput.IdempotencyKey.String() != key {
		t.Fatalf("create authority/input = (%s, %+v)", commandRepository.lastOrg, commandRepository.lastInput)
	}
	var createdData struct {
		CreateCommand struct {
			ID        string    `json:"id"`
			Status    string    `json:"status"`
			CreatedAt time.Time `json:"createdAt"`
			ExpiresAt time.Time `json:"expiresAt"`
		} `json:"createCommand"`
	}
	decodeData(t, created, &createdData)
	if createdData.CreateCommand.Status != string(commands.StatusPending) || createdData.CreateCommand.ID == "" {
		t.Fatalf("created command = %+v", createdData.CreateCommand)
	}
	if strings.Contains(string(created.Data), "idempotencyKey") || strings.Contains(string(created.Data), "organizationId") {
		t.Fatalf("internal command fields leaked: %s", created.Data)
	}

	query := doGraphQL(t, handler, `{ "query": "query { command(id: \"`+commandID.String()+`\") { id deviceId type status failureCode } deviceCommands(deviceId: \"`+deviceID.String()+`\", first: 1) { edges { cursor node { id status } } pageInfo { endCursor hasNextPage } } }" }`, "")
	if len(query.Errors) != 0 {
		t.Fatalf("command query errors = %+v", query.Errors)
	}
	var queryData struct {
		Command *struct {
			ID string `json:"id"`
		} `json:"command"`
		DeviceCommands struct {
			Edges []struct {
				Cursor string `json:"cursor"`
				Node   struct {
					ID string `json:"id"`
				} `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				EndCursor   *string `json:"endCursor"`
				HasNextPage bool    `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"deviceCommands"`
	}
	decodeData(t, query, &queryData)
	if queryData.Command == nil || queryData.Command.ID != commandID.String() || len(queryData.DeviceCommands.Edges) != 1 {
		t.Fatalf("command query data = %+v", queryData)
	}
	if !queryData.DeviceCommands.PageInfo.HasNextPage || queryData.DeviceCommands.PageInfo.EndCursor == nil ||
		*queryData.DeviceCommands.PageInfo.EndCursor != queryData.DeviceCommands.Edges[0].Cursor {
		t.Fatalf("command page info = %+v", queryData.DeviceCommands.PageInfo)
	}
	continued := doGraphQL(t, handler, `{ "query": "query { deviceCommands(deviceId: \"`+deviceID.String()+`\", first: 1, after: \"`+*queryData.DeviceCommands.PageInfo.EndCursor+`\") { edges { node { id } } pageInfo { hasNextPage } } }" }`, "")
	if len(continued.Errors) != 0 || commandRepository.lastCursor == nil || commandRepository.lastCursor.ID != commandID {
		t.Fatalf("command continuation = (%+v, cursor=%+v)", continued, commandRepository.lastCursor)
	}
	var continuedData struct {
		DeviceCommands struct {
			Edges []struct {
				Node struct {
					ID string `json:"id"`
				} `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"deviceCommands"`
	}
	decodeData(t, continued, &continuedData)
	if len(continuedData.DeviceCommands.Edges) != 1 || continuedData.DeviceCommands.Edges[0].Node.ID != second.ID.String() || continuedData.DeviceCommands.PageInfo.HasNextPage {
		t.Fatalf("continued page = %+v", continuedData.DeviceCommands)
	}

	foreignRead := doGraphQL(t, handler, `{ "query": "query { command(id: \"`+foreignCommandID.String()+`\") { id } deviceCommands(deviceId: \"`+foreignDeviceID.String()+`\") { edges { node { id } } } }" }`, "")
	if len(foreignRead.Errors) != 0 || !strings.Contains(string(foreignRead.Data), `"command":null`) || !strings.Contains(string(foreignRead.Data), `"edges":[]`) {
		t.Fatalf("foreign command reads = %+v", foreignRead)
	}

	foreignCreate := doGraphQL(t, handler, `{ "query": "mutation { createCommand(input: { deviceId: \"`+foreignDeviceID.String()+`\", type: PING, idempotencyKey: \"77777777-7777-4777-8777-777777777777\" }) { id } }" }`, "")
	assertErrorCode(t, foreignCreate, errorCodeBadUserInput)
	if strings.Contains(foreignCreate.Errors[0].Message, foreignDeviceID.String()) {
		t.Fatalf("foreign device leaked in error: %+v", foreignCreate.Errors[0])
	}
}

func TestGraphQLCommandRejectsInvalidScopeAndMapsConflict(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	commandRepository := &fakeCommandRepository{
		organizationID: organizationID,
		deviceIDs:      map[uuid.UUID]bool{deviceID: true},
		commands:       make(map[uuid.UUID]commands.Command),
		commandOrgs:    make(map[uuid.UUID]uuid.UUID),
		keys:           make(map[uuid.UUID]uuid.UUID),
	}
	handler, err := NewHandlerWithCommands(
		&fakeRepository{organizationID: organizationID}, nil, nil, commandRepository,
		organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewHandlerWithCommands returned error: %v", err)
	}
	invalidKey := doGraphQL(t, handler, `{ "query": "mutation { createCommand(input: { deviceId: \"`+deviceID.String()+`\", type: PING, idempotencyKey: \"not-a-uuid\" }) { id } }" }`, "")
	assertErrorCode(t, invalidKey, errorCodeBadUserInput)
	unknownPayloadRequest := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"mutation { createCommand(input: { deviceId: \"`+deviceID.String()+`\", type: PING, idempotencyKey: \"88888888-8888-4888-8888-888888888888\", payload: \"{}\" }) { id } }"}`))
	unknownPayloadRequest.Header.Set("Content-Type", "application/json")
	unknownPayloadRequest = unknownPayloadRequest.WithContext(WithRequestContext(unknownPayloadRequest.Context(), "strict-command-input", Principal{OrganizationID: organizationID}))
	unknownPayloadResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownPayloadResponse, unknownPayloadRequest)
	if unknownPayloadResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown payload status = %d, body=%s", unknownPayloadResponse.Code, unknownPayloadResponse.Body.String())
	}
	var unknownPayload graphqlResponse
	if err := json.NewDecoder(unknownPayloadResponse.Body).Decode(&unknownPayload); err != nil {
		t.Fatalf("decode unknown payload response: %v", err)
	}
	assertErrorCode(t, unknownPayload, "GRAPHQL_VALIDATION_FAILED")
	if commandRepository.lastInput.IdempotencyKey != uuid.Nil {
		t.Fatal("unknown payload reached the command repository")
	}

	commandRepository.createErr = commands.ErrConflict
	conflict := doGraphQL(t, handler, `{ "query": "mutation { createCommand(input: { deviceId: \"`+deviceID.String()+`\", type: PING, idempotencyKey: \"88888888-8888-4888-8888-888888888888\" }) { id } }" }`, "")
	assertErrorCode(t, conflict, errorCodeConflict)
	if conflict.Errors[0].Message != "command conflicts with current state" {
		t.Fatalf("command conflict message = %q", conflict.Errors[0].Message)
	}

	badCursor := encodeForOtherScope(t, organizationID, uuid.MustParse("33333333-3333-4333-8333-333333333333"))
	wrongScope := doGraphQL(t, handler, `{ "query": "query { deviceCommands(deviceId: \"`+deviceID.String()+`\", after: \"`+badCursor+`\") { edges { node { id } } } }" }`, "")
	assertErrorCode(t, wrongScope, errorCodeBadUserInput)
	if commandRepository.lastCursor != nil {
		t.Fatal("wrong-scope cursor reached the command repository")
	}

	pageResponse := doGraphQL(t, handler, `{ "query": "query { deviceCommands(deviceId: \"`+deviceID.String()+`\", first: 0) { edges { node { id } } } }" }`, "")
	assertErrorCode(t, pageResponse, errorCodeBadUserInput)
	if commandRepository.lastPageSize != 0 {
		t.Fatalf("invalid page size reached repository: %d", commandRepository.lastPageSize)
	}
}

func encodeForOtherScope(t *testing.T, organizationID, deviceID uuid.UUID) string {
	t.Helper()
	cursor, err := encodeCommandCursor(commands.Cursor{
		CreatedAt: time.Date(2026, 9, 29, 4, 0, 0, 0, time.UTC),
		ID:        uuid.MustParse("99999999-9999-4999-8999-999999999999"),
	}, organizationID, deviceID)
	if err != nil {
		t.Fatalf("encode wrong-scope cursor: %v", err)
	}
	return cursor
}

var _ CommandRepository = (*fakeCommandRepository)(nil)
