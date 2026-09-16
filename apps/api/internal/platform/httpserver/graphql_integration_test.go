//go:build integration

package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/graph"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
)

func TestGraphQLFiberCompositionUsesRealTenantScopedRepository(t *testing.T) {
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
	defer pool.Close()
	repository, err := registry.NewRepository(pool)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	organizationA, err := repository.CreateOrganization(ctx, "graphql-a-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "GraphQL A")
	if err != nil {
		t.Fatalf("create organization A: %v", err)
	}
	organizationB, err := repository.CreateOrganization(ctx, "graphql-b-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "GraphQL B")
	if err != nil {
		t.Fatalf("create organization B: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM devices WHERE organization_id IN ($1, $2)", organizationA, organizationB)
		_, _ = pool.Exec(context.Background(), "DELETE FROM organizations WHERE id IN ($1, $2)", organizationA, organizationB)
	}()
	deviceB, err := repository.CreateDevice(ctx, organizationB, registry.CreateDeviceInput{DeviceKey: "private-b", DisplayName: "Private B"})
	if err != nil {
		t.Fatalf("create organization B device: %v", err)
	}
	graphqlHandler, err := graph.NewHandler(repository, organizationA, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("create GraphQL handler: %v", err)
	}
	server := New(config.Config{Environment: config.Test, HTTPHost: "127.0.0.1", HTTPPort: 0}, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		GraphQLHandler:  graphqlHandler,
		ContextEnricher: graph.NewDevelopmentContextEnricher(organizationA),
	})

	requestBody, err := json.Marshal(map[string]string{
		"query": "query { device(id: \"" + deviceB.ID.String() + "\") { id } }",
	})
	if err != nil {
		t.Fatalf("encode cross-tenant query: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://example.test"+GraphQLPath, strings.NewReader(string(requestBody)))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.app.Test(request)
	if err != nil {
		t.Fatalf("Fiber request returned error: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cross-tenant status = %d", response.StatusCode)
	}
	var crossTenant struct {
		Data struct {
			Device *struct {
				ID string `json:"id"`
			} `json:"device"`
		} `json:"data"`
		Errors []any `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&crossTenant); err != nil {
		t.Fatalf("decode cross-tenant response: %v", err)
	}
	if len(crossTenant.Errors) != 0 || crossTenant.Data.Device != nil {
		t.Fatalf("cross-tenant response = %+v, want null without error", crossTenant)
	}

	requestBody, err = json.Marshal(map[string]string{
		"query": "mutation { createDevice(input: { deviceKey: \"private-a\", displayName: \"Private A\" }) { id } }",
	})
	if err != nil {
		t.Fatalf("encode create query: %v", err)
	}
	request = httptest.NewRequest(http.MethodPost, "http://example.test"+GraphQLPath, strings.NewReader(string(requestBody)))
	request.Header.Set("Content-Type", "application/json")
	response, err = server.app.Test(request)
	if err != nil {
		t.Fatalf("Fiber mutation returned error: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("mutation status = %d", response.StatusCode)
	}
	var mutation struct {
		Data struct {
			CreateDevice struct {
				ID string `json:"id"`
			} `json:"createDevice"`
		} `json:"data"`
		Errors []any `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&mutation); err != nil {
		t.Fatalf("decode mutation response: %v", err)
	}
	if len(mutation.Errors) != 0 || mutation.Data.CreateDevice.ID == "" {
		t.Fatalf("mutation response = %+v", mutation)
	}
	createdID, err := uuid.Parse(mutation.Data.CreateDevice.ID)
	if err != nil {
		t.Fatalf("created id = %q: %v", mutation.Data.CreateDevice.ID, err)
	}
	created, err := repository.GetDevice(ctx, organizationA, createdID)
	if err != nil {
		t.Fatalf("read created device: %v", err)
	}
	if created.OrganizationID != organizationA || created.DeviceKey != "private-a" {
		t.Fatalf("created device = %+v", created)
	}

	requestBody, err = json.Marshal(map[string]string{
		"query": "query { devices(first: 100) { edges { node { id deviceKey } } } }",
	})
	if err != nil {
		t.Fatalf("encode list query: %v", err)
	}
	request = httptest.NewRequest(http.MethodPost, "http://example.test"+GraphQLPath, strings.NewReader(string(requestBody)))
	request.Header.Set("Content-Type", "application/json")
	response, err = server.app.Test(request)
	if err != nil {
		t.Fatalf("Fiber list request returned error: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", response.StatusCode)
	}
	var list struct {
		Data struct {
			Devices struct {
				Edges []struct {
					Node struct {
						ID        string `json:"id"`
						DeviceKey string `json:"deviceKey"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"devices"`
		} `json:"data"`
		Errors []any `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(list.Errors) != 0 {
		t.Fatalf("list errors = %+v", list.Errors)
	}
	foundCreated := false
	for _, edge := range list.Data.Devices.Edges {
		if edge.Node.ID == deviceB.ID.String() {
			t.Fatalf("list exposed organization B device %s", deviceB.ID)
		}
		if edge.Node.ID == createdID.String() && edge.Node.DeviceKey == "private-a" {
			foundCreated = true
		}
	}
	if !foundCreated {
		t.Fatalf("list did not include organization A device %s: %+v", createdID, list.Data.Devices.Edges)
	}
}
