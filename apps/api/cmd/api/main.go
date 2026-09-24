package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/methat-ruk/pulsegrid/apps/api/internal/rules"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/ingestion"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/mqtttransport"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

const developmentOrganizationSlug = "pulsegrid-dev"

const (
	startupConfigurationInvalid      = "configuration_invalid"
	startupDatabaseUnavailable       = "database_unavailable"
	startupDatabaseSchemaUnavailable = "database_schema_unavailable"
	startupOrganizationMissing       = "development_organization_missing"
	startupRepositoryUnavailable     = "repository_initialization_failed"
	startupMQTTUnavailable           = "mqtt_ingestion_unavailable"
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
	needsRegistry := cfg.IdentityMode == config.IdentityDevelopment || cfg.MQTTIngestionEnabled()
	var pool *pgxpool.Pool
	var repository *registry.Repository
	var telemetryRepository *projection.Repository
	var rulesRepository *rules.Repository
	var organizationID uuid.UUID
	if needsRegistry {
		startupContext, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
		var startupErr error
		pool, repository, telemetryRepository, rulesRepository, startupErr = openDevelopmentDependencies(startupContext)
		if startupErr == nil && cfg.IdentityMode == config.IdentityDevelopment {
			organizationID, startupErr = resolveDevelopmentOrganization(startupContext, repository)
		}
		cancelStartup()
		if startupErr != nil {
			reasonCode, message := startupFailureDetails(startupErr)
			logger.Error("API dependency startup failed", "reason_code", reasonCode)
			fmt.Fprintf(os.Stderr, "API dependency startup failed: %s\n", message)
			os.Exit(1)
		}
		defer pool.Close()
	}

	if cfg.IdentityMode == config.IdentityDevelopment {
		graphqlHandler, handlerErr := graph.NewHandlerWithRules(repository, telemetryRepository, rulesRepository, organizationID, logger)
		if handlerErr != nil {
			pool.Close()
			logger.Error("graphql startup failed", "reason_code", "handler_initialization_failed")
			fmt.Fprintln(os.Stderr, "GraphQL startup failed: handler initialization failed")
			os.Exit(1)
		}
		serverOptions.GraphQLHandler = graphqlHandler
		serverOptions.ContextEnricher = graph.NewDevelopmentContextEnricher(organizationID)
	}

	var mqttRuntime *mqtttransport.Transport
	if cfg.MQTTIngestionEnabled() {
		resolver := repositoryResolver{repository: repository}
		consumer := ingestion.AcceptedTelemetryConsumer(telemetryRepository)
		handler, handlerErr := ingestion.NewHandler(resolver, consumer, ingestion.HandlerConfig{})
		if handlerErr != nil {
			if pool != nil {
				pool.Close()
			}
			logger.Error("MQTT ingestion startup failed", "reason_code", startupMQTTUnavailable)
			fmt.Fprintln(os.Stderr, "MQTT ingestion startup failed: handler initialization failed")
			os.Exit(1)
		}
		processor := telemetryProcessor(handler, logger)
		var transportErr error
		mqttRuntime, transportErr = mqtttransport.New(mqtttransport.DefaultConfig(cfg.MQTTBrokerURL), mqtttransport.PahoClientFactory, processor, logger)
		if transportErr == nil {
			transportErr = mqttRuntime.Start(ctx)
		}
		if transportErr != nil {
			if pool != nil {
				pool.Close()
			}
			logger.Error("MQTT ingestion startup failed", "reason_code", startupMQTTUnavailable)
			fmt.Fprintln(os.Stderr, "MQTT ingestion startup failed: broker or subscription unavailable")
			os.Exit(1)
		}
	}

	serverOptions.ReadinessCheck = readinessCheck(pool, mqttRuntime)

	var mqttStopDone chan error
	if mqttRuntime != nil {
		mqttStopDone = make(chan error, 1)
		go func() {
			<-ctx.Done()
			shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
			defer cancelShutdown()
			mqttStopDone <- mqttRuntime.Stop(shutdownContext)
		}()
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
		if mqttStopDone != nil {
			select {
			case stopErr := <-mqttStopDone:
				if stopErr != nil {
					logger.Error("MQTT ingestion shutdown failed", "reason_code", "shutdown_incomplete")
					os.Exit(1)
				}
			case <-waitContext.Done():
				logger.Error("MQTT ingestion shutdown did not complete", "reason_code", "shutdown_incomplete")
				os.Exit(1)
			}
		}
	}

	if listenErr != nil && !errors.Is(listenErr, context.Canceled) {
		if mqttRuntime != nil && ctx.Err() == nil {
			shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
			_ = mqttRuntime.Stop(shutdownContext)
			cancelShutdown()
		}
		logger.Error("http server exited", "reason_code", "listen_failed", "error", listenErr.Error())
		os.Exit(1)
	}
}

func openDevelopmentGraphQL(ctx context.Context) (*pgxpool.Pool, *registry.Repository, uuid.UUID, error) {
	pool, repository, _, _, err := openDevelopmentDependencies(ctx)
	if err != nil {
		return nil, nil, uuid.Nil, err
	}
	organizationID, err := resolveDevelopmentOrganization(ctx, repository)
	if err != nil {
		pool.Close()
		return nil, nil, uuid.Nil, err
	}
	return pool, repository, organizationID, nil
}

func openDevelopmentDependencies(ctx context.Context) (*pgxpool.Pool, *registry.Repository, *projection.Repository, *rules.Repository, error) {
	databaseConfiguration, err := databaseconfig.Load()
	if err != nil {
		return nil, nil, nil, nil, newStartupFailure(startupConfigurationInvalid, "development database configuration is invalid", err)
	}
	pool, err := database.Open(ctx, databaseConfiguration.URL)
	if err != nil {
		return nil, nil, nil, nil, newStartupFailure(startupDatabaseUnavailable, "development database is unavailable", err)
	}
	repository, err := registry.NewRepository(pool)
	if err != nil {
		pool.Close()
		return nil, nil, nil, nil, newStartupFailure(startupRepositoryUnavailable, "development registry is unavailable", err)
	}
	rulesRepository, err := rules.NewRepository(pool)
	if err != nil {
		pool.Close()
		return nil, nil, nil, nil, newStartupFailure(startupRepositoryUnavailable, "development threshold rules are unavailable", err)
	}
	telemetryRepository, err := projection.NewRepositoryWithEvaluator(pool, rulesRepository)
	if err != nil {
		pool.Close()
		return nil, nil, nil, nil, newStartupFailure(startupRepositoryUnavailable, "development telemetry projection is unavailable", err)
	}
	if err := telemetryRepository.ValidateSchema(ctx); err != nil {
		pool.Close()
		return nil, nil, nil, nil, classifyTelemetrySchemaFailure(err)
	}
	if err := rulesRepository.ValidateSchema(ctx); err != nil {
		pool.Close()
		return nil, nil, nil, nil, classifyRulesSchemaFailure(err)
	}
	return pool, repository, telemetryRepository, rulesRepository, nil
}

func classifyRulesSchemaFailure(err error) error {
	if errors.Is(err, rules.ErrSchemaUnavailable) {
		return newStartupFailure(startupDatabaseSchemaUnavailable, "development database schema is unavailable; run migrations", err)
	}
	if pgError, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgError.Code {
		case "3F000", "42P01", "42703":
			return newStartupFailure(startupDatabaseSchemaUnavailable, "development database schema is unavailable; run migrations", err)
		}
	}
	return newStartupFailure(startupDatabaseUnavailable, "development database is unavailable", err)
}

func classifyTelemetrySchemaFailure(err error) error {
	if errors.Is(err, projection.ErrSchemaUnavailable) {
		return newStartupFailure(startupDatabaseSchemaUnavailable, "development database schema is unavailable; run migrations", err)
	}
	if pgError, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgError.Code {
		case "3F000", "42P01", "42703":
			return newStartupFailure(startupDatabaseSchemaUnavailable, "development database schema is unavailable; run migrations", err)
		}
	}
	return newStartupFailure(startupDatabaseUnavailable, "development database is unavailable", err)
}

func resolveDevelopmentOrganization(ctx context.Context, repository *registry.Repository) (uuid.UUID, error) {
	organizationID, err := repository.FindOrganizationBySlug(ctx, developmentOrganizationSlug)
	if err != nil {
		return uuid.Nil, classifyOrganizationLookupFailure(err)
	}
	return organizationID, nil
}

type repositoryResolver struct {
	repository *registry.Repository
}

func (r repositoryResolver) ResolveDevice(ctx context.Context, tenantSlug string, deviceID uuid.UUID) (uuid.UUID, error) {
	device, err := r.repository.ResolveDeviceByTenantSlug(ctx, tenantSlug, deviceID)
	if errors.Is(err, registry.ErrNotFound) {
		return uuid.Nil, ingestion.ErrDeviceNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	return device.OrganizationID, nil
}

func telemetryProcessor(handler *ingestion.Handler, logger *slog.Logger) mqtttransport.Processor {
	return func(ctx context.Context, delivery ingestion.Delivery) {
		accepted, err := handler.Handle(ctx, delivery)
		if err != nil {
			fields := []any{
				"reason_code", telemetryFailureReason(err),
				"ingestion_id", delivery.IngestionID,
				"payload_bytes", len(delivery.Payload),
				"qos", delivery.QoS,
				"retained", delivery.Retained,
				"duplicate", delivery.Duplicate,
			}
			if ingestion.IsRejected(err) {
				logger.Warn("mqtt telemetry rejected", fields...)
			} else {
				logger.Error("mqtt telemetry processing failed", fields...)
			}
			return
		}
		logger.Info("mqtt telemetry accepted",
			"reason_code", "telemetry_accepted",
			"ingestion_id", accepted.IngestionID,
			"message_id", accepted.MessageID,
			"organization_id", accepted.OrganizationID,
			"device_id", accepted.DeviceID,
			"observed_at", accepted.ObservedAt,
			"received_at", accepted.ReceivedAt,
			"duplicate", accepted.MQTTDuplicate,
		)
	}
}

func telemetryFailureReason(err error) string {
	switch {
	case errors.Is(err, projection.ErrMessageConflict):
		return "telemetry_message_id_conflict"
	case errors.Is(err, projection.ErrStorageConflict):
		return "telemetry_storage_conflict"
	case errors.Is(err, rules.ErrEvaluationFailed):
		return "telemetry_rule_evaluation_failed"
	case errors.Is(err, rules.ErrAlertPersistence):
		return "telemetry_alert_persistence_failed"
	default:
		return ingestion.ReasonOf(err)
	}
}

func readinessCheck(pool *pgxpool.Pool, mqttRuntime *mqtttransport.Transport) func(context.Context) error {
	if pool == nil && mqttRuntime == nil {
		return nil
	}
	return func(ctx context.Context) error {
		if pool != nil {
			if err := pool.Ping(ctx); err != nil {
				return err
			}
		}
		if mqttRuntime != nil && !mqttRuntime.Ready() {
			return errors.New("mqtt ingestion is unavailable")
		}
		return nil
	}
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
