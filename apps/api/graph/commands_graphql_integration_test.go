//go:build integration

package graph

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/commands"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
)

func TestGraphQLCommandsRealPostgresContractAndTenantScope(t *testing.T) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	if databaseConfiguration.Environment != config.Test {
		t.Fatalf("integration environment = %q, want test", databaseConfiguration.Environment)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	registryRepository, err := registry.NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create registry repository: %v", err)
	}
	commandRepository, err := commands.NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create command repository: %v", err)
	}
	organizationA, err := registryRepository.CreateOrganization(ctx, "command-api-"+uuid.NewString(), "Command API A")
	if err != nil {
		pool.Close()
		t.Fatalf("create organization A: %v", err)
	}
	deviceA, err := registryRepository.CreateDevice(ctx, organizationA, registry.CreateDeviceInput{DeviceKey: "command-device-a", DisplayName: "Device A"})
	if err != nil {
		pool.Close()
		t.Fatalf("create device A: %v", err)
	}
	organizationB, err := registryRepository.CreateOrganization(ctx, "command-api-b-"+uuid.NewString(), "Command API B")
	if err != nil {
		pool.Close()
		t.Fatalf("create organization B: %v", err)
	}
	deviceB, err := registryRepository.CreateDevice(ctx, organizationB, registry.CreateDeviceInput{DeviceKey: "command-device-b", DisplayName: "Device B"})
	if err != nil {
		pool.Close()
		t.Fatalf("create device B: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM commands WHERE organization_id = ANY($1)`, []uuid.UUID{organizationA, organizationB})
		_, _ = pool.Exec(cleanupContext, `DELETE FROM devices WHERE id = ANY($1)`, []uuid.UUID{deviceA.ID, deviceB.ID})
		_, _ = pool.Exec(cleanupContext, `DELETE FROM organizations WHERE id = ANY($1)`, []uuid.UUID{organizationA, organizationB})
		pool.Close()
	})

	handler, err := NewHandlerWithCommands(
		registryRepository, nil, nil, commandRepository, organizationA,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("create command GraphQL handler: %v", err)
	}
	key := uuid.New()
	createBody := `{ "query": "mutation { createCommand(input: { deviceId: \"` + deviceA.ID.String() + `\", type: PING, idempotencyKey: \"` + key.String() + `\" }) { id deviceId type status createdAt expiresAt } }" }`
	created := doGraphQLForOrganization(t, handler, organizationA, createBody)
	if len(created.Errors) != 0 {
		t.Fatalf("create command response = %+v", created)
	}
	var createdData struct {
		CreateCommand struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			DeviceID string `json:"deviceId"`
		} `json:"createCommand"`
	}
	if err := json.Unmarshal(created.Data, &createdData); err != nil {
		t.Fatalf("decode created command: %v", err)
	}
	if createdData.CreateCommand.ID == "" || createdData.CreateCommand.Status != string(commands.StatusPending) || createdData.CreateCommand.DeviceID != deviceA.ID.String() {
		t.Fatalf("created command = %+v", createdData.CreateCommand)
	}
	duplicate := doGraphQLForOrganization(t, handler, organizationA, createBody)
	var duplicateData struct {
		CreateCommand struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"createCommand"`
	}
	if err := json.Unmarshal(duplicate.Data, &duplicateData); err != nil {
		t.Fatalf("decode duplicate command: %v", err)
	}
	if len(duplicate.Errors) != 0 || duplicateData.CreateCommand.ID != createdData.CreateCommand.ID {
		t.Fatalf("duplicate create = (%+v, %+v), want same command", duplicate, duplicateData)
	}
	secondIntent, err := commandRepository.Create(ctx, organizationA, commands.CreateInput{
		DeviceID: deviceA.ID, Type: commands.TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create second command for pagination: %v", err)
	}
	firstPage := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { deviceCommands(deviceId: \"`+deviceA.ID.String()+`\", first: 1) { edges { cursor node { id } } pageInfo { endCursor hasNextPage } } }" }`)
	var firstPageData struct {
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
	if len(firstPage.Errors) != 0 {
		t.Fatalf("first command page = %+v", firstPage)
	}
	if err := json.Unmarshal(firstPage.Data, &firstPageData); err != nil {
		t.Fatalf("decode first command page: %v", err)
	}
	if len(firstPageData.DeviceCommands.Edges) != 1 || !firstPageData.DeviceCommands.PageInfo.HasNextPage || firstPageData.DeviceCommands.PageInfo.EndCursor == nil {
		t.Fatalf("first command page data = %+v", firstPageData.DeviceCommands)
	}
	secondPage := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { deviceCommands(deviceId: \"`+deviceA.ID.String()+`\", first: 1, after: \"`+*firstPageData.DeviceCommands.PageInfo.EndCursor+`\") { edges { node { id } } pageInfo { hasNextPage } } }" }`)
	var secondPageData struct {
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
	if len(secondPage.Errors) != 0 {
		t.Fatalf("second command page = %+v", secondPage)
	}
	if err := json.Unmarshal(secondPage.Data, &secondPageData); err != nil {
		t.Fatalf("decode second command page: %v", err)
	}
	if len(secondPageData.DeviceCommands.Edges) != 1 || secondPageData.DeviceCommands.PageInfo.HasNextPage {
		t.Fatalf("second command page data = %+v", secondPageData.DeviceCommands)
	}
	firstID := firstPageData.DeviceCommands.Edges[0].Node.ID
	secondID := secondPageData.DeviceCommands.Edges[0].Node.ID
	if firstID == secondID || !((firstID == createdData.CreateCommand.ID && secondID == secondIntent.ID.String()) ||
		(firstID == secondIntent.ID.String() && secondID == createdData.CreateCommand.ID)) {
		t.Fatalf("command pages did not contain both unique intents: first=%v second=%v", firstID, secondID)
	}

	foreignIntent, err := commandRepository.Create(ctx, organizationB, commands.CreateInput{
		DeviceID: deviceB.ID, Type: commands.TypePing, IdempotencyKey: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create foreign tenant command fixture: %v", err)
	}
	foreignRead := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { command(id: \"`+foreignIntent.ID.String()+`\") { id } deviceCommands(deviceId: \"`+deviceB.ID.String()+`\") { edges { node { id } } } }" }`)
	if len(foreignRead.Errors) != 0 || !strings.Contains(string(foreignRead.Data), `"command":null`) || !strings.Contains(string(foreignRead.Data), `"edges":[]`) {
		t.Fatalf("foreign read response = %+v", foreignRead)
	}
	foreignKey := uuid.New()
	foreignCreate := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "mutation { createCommand(input: { deviceId: \"`+deviceB.ID.String()+`\", type: PING, idempotencyKey: \"`+foreignKey.String()+`\" }) { id } }" }`)
	assertErrorCode(t, foreignCreate, errorCodeBadUserInput)
	var deniedRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM commands WHERE organization_id = $1 AND idempotency_key = $2`, organizationA, foreignKey).Scan(&deniedRows); err != nil {
		t.Fatalf("verify denied create had no stored effect: %v", err)
	}
	if deniedRows != 0 {
		t.Fatalf("foreign create stored %d command rows, want none", deniedRows)
	}

	badCursor := encodeForOtherScope(t, organizationA, deviceB.ID)
	wrongCursor := doGraphQLForOrganization(t, handler, organizationA, `{ "query": "query { deviceCommands(deviceId: \"`+deviceA.ID.String()+`\", after: \"`+badCursor+`\") { edges { node { id } } } }" }`)
	assertErrorCode(t, wrongCursor, errorCodeBadUserInput)

	_, err = pool.Exec(ctx, `
		INSERT INTO commands (
			id, organization_id, device_id, type, status, idempotency_key,
			created_at, updated_at, expires_at
		) VALUES ($1, $2, $3, 'PING', 'PENDING', $4, $5, $5, $6)
	`, uuid.New(), organizationA, deviceB.ID, uuid.New(), time.Now().UTC(), time.Now().UTC().Add(commands.CommandLifetime))
	if pgError, ok := err.(*pgconn.PgError); !ok || pgError.Code != "23503" {
		t.Fatalf("cross-tenant composite FK error = %v, want foreign-key violation", err)
	}
}

func TestGraphQLCommandReplayAfterDroppedResponseAndRepositoryRestart(t *testing.T) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	if databaseConfiguration.Environment != config.Test {
		t.Fatalf("integration environment = %q, want test", databaseConfiguration.Environment)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open initial integration database pool: %v", err)
	}
	registryRepository, err := registry.NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create initial registry repository: %v", err)
	}
	commandRepository, err := commands.NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create initial command repository: %v", err)
	}
	organizationID, err := registryRepository.CreateOrganization(ctx, "command-replay-"+uuid.NewString(), "Command Replay")
	if err != nil {
		pool.Close()
		t.Fatalf("create replay organization: %v", err)
	}
	device, err := registryRepository.CreateDevice(ctx, organizationID, registry.CreateDeviceInput{
		DeviceKey: "command-replay-device", DisplayName: "Replay Device",
	})
	if err != nil {
		pool.Close()
		t.Fatalf("create replay device: %v", err)
	}
	cleanupPool := pool
	t.Cleanup(func() {
		if cleanupPool == nil {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = cleanupPool.Exec(cleanupContext, `DELETE FROM commands WHERE organization_id = $1`, organizationID)
		_, _ = cleanupPool.Exec(cleanupContext, `DELETE FROM devices WHERE id = $1`, device.ID)
		_, _ = cleanupPool.Exec(cleanupContext, `DELETE FROM organizations WHERE id = $1`, organizationID)
		cleanupPool.Close()
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := NewHandlerWithCommands(registryRepository, nil, nil, commandRepository, organizationID, logger)
	if err != nil {
		t.Fatalf("create initial GraphQL handler: %v", err)
	}
	key := uuid.New()
	createBody := `{ "query": "mutation { createCommand(input: { deviceId: \"` + device.ID.String() + `\", type: PING, idempotencyKey: \"` + key.String() + `\" }) { id deviceId status createdAt updatedAt expiresAt } }" }`

	// Discard the successful response to model a committed request whose result did not reach the caller.
	_ = doGraphQLForOrganization(t, handler, organizationID, createBody)
	var committed struct {
		ID        uuid.UUID
		Status    string
		CreatedAt time.Time
		UpdatedAt time.Time
		ExpiresAt time.Time
	}
	if err := pool.QueryRow(ctx, `
		SELECT id, status, created_at, updated_at, expires_at
		FROM commands WHERE organization_id = $1 AND idempotency_key = $2
	`, organizationID, key).Scan(&committed.ID, &committed.Status, &committed.CreatedAt, &committed.UpdatedAt, &committed.ExpiresAt); err != nil {
		t.Fatalf("read command committed before response loss: %v", err)
	}
	if committed.ID == uuid.Nil || committed.Status != string(commands.StatusPending) {
		t.Fatalf("committed command = %+v, want one persisted PENDING row", committed)
	}

	pool.Close()
	pool, err = database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open replacement integration database pool: %v", err)
	}
	cleanupPool = pool
	registryRepository, err = registry.NewRepository(pool)
	if err != nil {
		t.Fatalf("create replacement registry repository: %v", err)
	}
	commandRepository, err = commands.NewRepository(pool)
	if err != nil {
		t.Fatalf("create replacement command repository: %v", err)
	}
	handler, err = NewHandlerWithCommands(registryRepository, nil, nil, commandRepository, organizationID, logger)
	if err != nil {
		t.Fatalf("create replacement GraphQL handler: %v", err)
	}

	replayed := doGraphQLForOrganization(t, handler, organizationID, createBody)
	if len(replayed.Errors) != 0 {
		t.Fatalf("replay response after restart = %+v", replayed)
	}
	var replayData struct {
		CreateCommand struct {
			ID        string `json:"id"`
			DeviceID  string `json:"deviceId"`
			Status    string `json:"status"`
			CreatedAt string `json:"createdAt"`
			UpdatedAt string `json:"updatedAt"`
			ExpiresAt string `json:"expiresAt"`
		} `json:"createCommand"`
	}
	if err := json.Unmarshal(replayed.Data, &replayData); err != nil {
		t.Fatalf("decode replay after restart: %v", err)
	}
	command := replayData.CreateCommand
	if command.ID != committed.ID.String() || command.DeviceID != device.ID.String() || command.Status != committed.Status ||
		command.CreatedAt != committed.CreatedAt.UTC().Format(time.RFC3339Nano) ||
		command.UpdatedAt != committed.UpdatedAt.UTC().Format(time.RFC3339Nano) ||
		command.ExpiresAt != committed.ExpiresAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("replayed command = %+v, want original committed identity, timestamps, and state %+v", command, committed)
	}
	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM commands WHERE organization_id = $1 AND idempotency_key = $2`, organizationID, key).Scan(&rowCount); err != nil {
		t.Fatalf("count replayed command rows after restart: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("rows for replayed tenant/key = %d, want one", rowCount)
	}
}
