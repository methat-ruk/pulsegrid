package graph

import (
	"context"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
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

// Principal is the trusted server-selected GraphQL authority. No GraphQL
// argument, header, cookie, or client state can replace it.
type Principal struct {
	OrganizationID uuid.UUID
}

// Resolver owns the request-scoped device use cases and fixed tenant scope.
type Resolver struct {
	repository     DeviceRepository
	organizationID uuid.UUID
}

// NewResolver constructs the GraphQL resolver root for one fixed organization.
func NewResolver(repository DeviceRepository, organizationID uuid.UUID) *Resolver {
	return &Resolver{
		repository:     repository,
		organizationID: organizationID,
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
