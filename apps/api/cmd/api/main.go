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
			logger.Error("graphql startup failed", "reason_code", "dependency_unavailable")
			fmt.Fprintln(os.Stderr, "GraphQL startup failed: development database is unavailable")
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
		return nil, nil, uuid.Nil, err
	}
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		return nil, nil, uuid.Nil, err
	}
	repository, err := registry.NewRepository(pool)
	if err != nil {
		pool.Close()
		return nil, nil, uuid.Nil, err
	}
	organizationID, err := repository.FindOrganizationBySlug(ctx, developmentOrganizationSlug)
	if err != nil {
		pool.Close()
		return nil, nil, uuid.Nil, err
	}
	return pool, repository, organizationID, nil
}
