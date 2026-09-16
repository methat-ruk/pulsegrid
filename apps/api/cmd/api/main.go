package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/methat-ruk/pulsegrid/apps/api/graph"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/database"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/databaseconfig"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/httpserver"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/logging"
)

const developmentOrganizationSlug = "pulsegrid-dev"

const (
	startupConfigurationInvalid      = "configuration_invalid"
	startupDatabaseUnavailable       = "database_unavailable"
	startupDatabaseSchemaUnavailable = "database_schema_unavailable"
	startupOrganizationMissing       = "development_organization_missing"
	startupRepositoryUnavailable     = "repository_initialization_failed"
)

type startupFailure struct {
	reasonCode string
	message    string
	cause      error
}

func (e *startupFailure) Error() string { return e.message }

func (e *startupFailure) Unwrap() error { return e.cause }

func newStartupFailure(reasonCode string, message string, cause error) error {
	return &startupFailure{reasonCode: reasonCode, message: message, cause: cause}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %s\n", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Environment, cfg.LogLevel, os.Stdout)
	serverOptions := httpserver.Options{}
	var poolClose func()
	if cfg.IdentityMode == config.IdentityDevelopment {
		startupContext, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
		pool, repository, organizationID, startupErr := openDevelopmentGraphQL(startupContext)
		cancelStartup()
		if startupErr != nil {
			reasonCode, message := startupFailureDetails(startupErr)
			logger.Error("graphql startup failed", "reason_code", reasonCode)
			fmt.Fprintf(os.Stderr, "GraphQL startup failed: %s\n", message)
			os.Exit(1)
		}
		poolClose = pool.Close
		graphqlHandler, handlerErr := graph.NewHandler(repository, organizationID, logger)
		if handlerErr != nil {
			poolClose()
			logger.Error("graphql startup failed", "reason_code", "handler_initialization_failed")
			fmt.Fprintln(os.Stderr, "GraphQL startup failed: handler initialization failed")
			os.Exit(1)
		}
		serverOptions = httpserver.Options{
			GraphQLHandler:  graphqlHandler,
			ContextEnricher: graph.NewDevelopmentContextEnricher(organizationID),
			ReadinessCheck: func(probeContext context.Context) error {
				return pool.Ping(probeContext)
			},
		}
	}
	if poolClose != nil {
		defer poolClose()
	}

	server := httpserver.New(cfg, logger, serverOptions)
	listenErr := server.Listen(ctx, cfg.ShutdownTimeout)
	if ctx.Err() != nil {
		waitContext, cancelWait := context.WithTimeout(context.Background(), cfg.ShutdownTimeout+time.Second)
		defer cancelWait()
		select {
		case <-server.StoppedSignal():
		case <-waitContext.Done():
			logger.Error("http server shutdown did not complete", "reason_code", "shutdown_incomplete")
			os.Exit(1)
		}
	}

	if listenErr != nil && !errors.Is(listenErr, context.Canceled) {
		logger.Error("http server exited", "reason_code", "listen_failed", "error", listenErr.Error())
		os.Exit(1)
	}
}

func openDevelopmentGraphQL(ctx context.Context) (*pgxpool.Pool, *registry.Repository, uuid.UUID, error) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		return nil, nil, uuid.Nil, newStartupFailure(startupConfigurationInvalid, "development database configuration is invalid", err)
	}
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		return nil, nil, uuid.Nil, newStartupFailure(startupDatabaseUnavailable, "development database is unavailable", err)
	}
	repository, err := registry.NewRepository(pool)
	if err != nil {
		pool.Close()
		return nil, nil, uuid.Nil, newStartupFailure(startupRepositoryUnavailable, "development registry is unavailable", err)
	}
	organizationID, err := repository.FindOrganizationBySlug(ctx, developmentOrganizationSlug)
	if err != nil {
		pool.Close()
		return nil, nil, uuid.Nil, classifyOrganizationLookupFailure(err)
	}
	return pool, repository, organizationID, nil
}

func classifyOrganizationLookupFailure(err error) error {
	if errors.Is(err, registry.ErrNotFound) {
		return newStartupFailure(startupOrganizationMissing, "seeded pulsegrid-dev organization is missing; run the development seed command", err)
	}

	if pgError, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgError.Code {
		case "3F000", "42P01", "42703":
			return newStartupFailure(startupDatabaseSchemaUnavailable, "development database schema is unavailable; run migrations", err)
		}
	}

	return newStartupFailure(startupDatabaseUnavailable, "development database is unavailable", err)
}

func startupFailureDetails(err error) (string, string) {
	if failure, ok := errors.AsType[*startupFailure](err); ok {
		return failure.reasonCode, failure.message
	}
	return startupDatabaseUnavailable, "development database is unavailable"
}
