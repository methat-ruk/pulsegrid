package graph

// This file is regenerated when the SDL changes. The resolver implementations
// are preserved by gqlgen's preserve_resolver setting.

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/graph/model"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
)

// CreateDevice is the resolver for the createDevice field.
func (r *mutationResolver) CreateDevice(ctx context.Context, input model.CreateDeviceInput) (*model.Device, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	device, err := r.repository.CreateDevice(ctx, organizationID, registry.CreateDeviceInput{
		DeviceKey:   input.DeviceKey,
		DisplayName: input.DisplayName,
	})
	if err != nil {
		return nil, err
	}
	return presentDevice(device), nil
}

// Device is the resolver for the device field.
func (r *queryResolver) Device(ctx context.Context, id string) (*model.Device, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	deviceID, err := parseCanonicalUUID(id)
	if err != nil {
		return nil, err
	}
	device, err := r.repository.GetDevice(ctx, organizationID, deviceID)
	if errors.Is(err, registry.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return presentDevice(device), nil
}

// Devices is the resolver for the devices field.
func (r *queryResolver) Devices(ctx context.Context, first int, after *string) (*model.DeviceConnection, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	if first < 1 || first > registry.MaxPageSize {
		return nil, newPublicError(errorCodeBadUserInput, "first must be between 1 and 100", registry.ErrInvalidInput)
	}

	var cursor *registry.Cursor
	if after != nil {
		cursor, err = decodeCursor(*after)
		if err != nil {
			return nil, err
		}
	}
	page, err := r.repository.ListDevices(ctx, organizationID, first, cursor)
	if err != nil {
		return nil, err
	}

	edges := make([]*model.DeviceEdge, 0, len(page.Devices))
	for _, device := range page.Devices {
		deviceCursor, cursorErr := encodeCursor(registry.Cursor{CreatedAt: device.CreatedAt, ID: device.ID})
		if cursorErr != nil {
			return nil, cursorErr
		}
		edges = append(edges, &model.DeviceEdge{Cursor: deviceCursor, Node: presentDevice(device)})
	}

	pageInfo := &model.PageInfo{HasNextPage: page.NextCursor != nil}
	if len(edges) > 0 {
		endCursor := edges[len(edges)-1].Cursor
		pageInfo.EndCursor = &endCursor
	}
	return &model.DeviceConnection{Edges: edges, PageInfo: pageInfo}, nil
}

func presentDevice(device registry.Device) *model.Device {
	return &model.Device{
		ID:          device.ID.String(),
		DeviceKey:   device.DeviceKey,
		DisplayName: device.DisplayName,
		CreatedAt:   device.CreatedAt.UTC(),
	}
}

func parseCanonicalUUID(raw string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil || parsed.String() != raw {
		return uuid.Nil, newPublicError(errorCodeBadUserInput, "id must be a canonical UUID", registry.ErrInvalidInput)
	}
	return parsed, nil
}

func (r *Resolver) authority(ctx context.Context) (uuid.UUID, error) {
	principal, err := principalFromContext(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if r.repository == nil || r.organizationID == uuid.Nil || principal.OrganizationID != r.organizationID {
		return uuid.Nil, newPublicError(errorCodeInternal, "request authority is unavailable", errMissingPrincipal)
	}
	return principal.OrganizationID, nil
}

// Mutation returns MutationResolver implementation.
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

// Query returns QueryResolver implementation.
func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }

type (
	mutationResolver struct{ *Resolver }
	queryResolver    struct{ *Resolver }
)
