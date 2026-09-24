package graph

import (
	"context"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/rules"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

// DeviceRepository is the narrow persistence boundary consumed by GraphQL.
// Keeping the interface here makes resolver and contract tests independent of
// PostgreSQL while preserving the registry's tenant-scoped implementation.
type DeviceRepository interface {
	CreateDevice(context.Context, uuid.UUID, registry.CreateDeviceInput) (registry.Device, error)
	GetDevice(context.Context, uuid.UUID, uuid.UUID) (registry.Device, error)
	ListDevices(context.Context, uuid.UUID, int, *registry.Cursor) (registry.DevicePage, error)
}

// TelemetryRepository is the tenant-scoped read boundary used by GraphQL.
// Storage records remain behind the telemetry projection package.
type TelemetryRepository interface {
	GetCurrentState(context.Context, uuid.UUID, uuid.UUID) (projection.CurrentState, error)
	ListTelemetry(context.Context, uuid.UUID, uuid.UUID, int, *projection.Cursor) (projection.TelemetryPage, error)
}

// ThresholdRuleRepository is the tenant-scoped application boundary for
// rule configuration and immutable alert history.
type ThresholdRuleRepository interface {
	CreateRule(context.Context, uuid.UUID, rules.CreateRuleInput) (rules.Rule, error)
	UpdateRule(context.Context, uuid.UUID, rules.UpdateRuleInput) (rules.Rule, error)
	ListRules(context.Context, uuid.UUID, uuid.UUID) ([]rules.Rule, error)
	GetAlert(context.Context, uuid.UUID, uuid.UUID) (rules.Alert, error)
	ListAlerts(context.Context, uuid.UUID, int, *uuid.UUID, *rules.AlertCursor) (rules.AlertPage, error)
}

// Principal is the trusted server-selected GraphQL authority. No GraphQL
// argument, header, cookie, or client state can replace it.
type Principal struct {
	OrganizationID uuid.UUID
}

// Resolver owns the request-scoped device use cases and fixed tenant scope.
type Resolver struct {
	repository          DeviceRepository
	telemetryRepository TelemetryRepository
	rulesRepository     ThresholdRuleRepository
	organizationID      uuid.UUID
}

// NewResolver constructs the GraphQL resolver root for one fixed organization.
func NewResolver(repository DeviceRepository, organizationID uuid.UUID) *Resolver {
	return NewResolverWithTelemetry(repository, nil, organizationID)
}

// NewResolverWithTelemetry constructs the GraphQL resolver root with the
// additive telemetry read boundary enabled.
func NewResolverWithTelemetry(repository DeviceRepository, telemetryRepository TelemetryRepository, organizationID uuid.UUID) *Resolver {
	return NewResolverWithRules(repository, telemetryRepository, nil, organizationID)
}

// NewResolverWithRules constructs the GraphQL resolver root with telemetry
// and threshold-rule boundaries enabled.
func NewResolverWithRules(repository DeviceRepository, telemetryRepository TelemetryRepository, rulesRepository ThresholdRuleRepository, organizationID uuid.UUID) *Resolver {
	return &Resolver{
		repository:          repository,
		telemetryRepository: telemetryRepository,
		rulesRepository:     rulesRepository,
		organizationID:      organizationID,
	}
}

// NewDevelopmentContextEnricher returns the HTTP composition hook that places
// the fixed development principal into the request context.
func NewDevelopmentContextEnricher(organizationID uuid.UUID) func(context.Context) context.Context {
	principal := Principal{OrganizationID: organizationID}
	return func(ctx context.Context) context.Context {
		return WithPrincipal(ctx, principal)
	}
}
