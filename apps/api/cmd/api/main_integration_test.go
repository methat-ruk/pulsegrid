//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
)

func TestOpenDevelopmentGraphQLResolvesSeededOrganization(t *testing.T) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seedPool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}
	seedRepository, err := registry.NewRepository(seedPool)
	if err != nil {
		seedPool.Close()
		t.Fatalf("create seed repository: %v", err)
	}
	seedID, err := seedRepository.EnsureOrganization(ctx, developmentOrganizationSlug, "Pulsegrid Development")
	if err != nil {
		seedPool.Close()
		t.Fatalf("ensure development organization: %v", err)
	}
	defer func() {
		_, _ = seedPool.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", seedID)
		seedPool.Close()
	}()

	runtimePool, _, resolvedID, err := openDevelopmentGraphQL(ctx)
	if err != nil {
		t.Fatalf("open development GraphQL dependencies: %v", err)
	}
	defer runtimePool.Close()
	if resolvedID != seedID {
		t.Fatalf("resolved organization id = %s, want %s", resolvedID, seedID)
	}
}
