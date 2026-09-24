//go:build integration

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/rules"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
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

func TestOpenDevelopmentGraphQLRejectsMissingTelemetrySchema(t *testing.T) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	repository, err := projection.NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create projection repository: %v", err)
	}
	if err := repository.ValidateSchema(ctx); err == nil {
		pool.Close()
		t.Skip("integration database has the telemetry schema; run this test against a pre-005 database")
	} else if !errors.Is(err, projection.ErrSchemaUnavailable) {
		pool.Close()
		t.Fatalf("validate integration schema: %v", err)
	}
	pool.Close()
	_, _, _, err = openDevelopmentGraphQL(ctx)
	if err == nil {
		t.Fatal("openDevelopmentGraphQL succeeded without the telemetry schema")
	}
	code, message := startupFailureDetails(err)
	if code != startupDatabaseSchemaUnavailable || message != "development database schema is unavailable; run migrations" {
		t.Fatalf("pre-telemetry startup failure = (%q, %q), want schema-unavailable", code, message)
	}
}

func TestOpenDevelopmentGraphQLRejectsMissingThresholdRulesSchema(t *testing.T) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		t.Fatalf("load integration database configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	rulesRepository, err := rules.NewRepository(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("create threshold rules repository: %v", err)
	}
	if err := rulesRepository.ValidateSchema(ctx); err == nil {
		pool.Close()
		t.Skip("integration database has migration 006; run this test after rolling back only that migration")
	} else if !errors.Is(err, rules.ErrSchemaUnavailable) {
		pool.Close()
		t.Fatalf("validate threshold rules schema: %v", err)
	}
	pool.Close()
	_, _, _, err = openDevelopmentGraphQL(ctx)
	if err == nil {
		t.Fatal("openDevelopmentGraphQL succeeded without the threshold rules schema")
	}
	code, message := startupFailureDetails(err)
	if code != startupDatabaseSchemaUnavailable || message != "development database schema is unavailable; run migrations" {
		t.Fatalf("pre-006 startup failure = (%q, %q), want schema-unavailable", code, message)
	}
}
