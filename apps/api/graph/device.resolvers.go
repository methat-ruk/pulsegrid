package graph

// This file is regenerated when the SDL changes. The resolver implementations
// are preserved by gqlgen's preserve_resolver setting.

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/graph/model"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/rules"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
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

// DeviceCurrentState is the resolver for the deviceCurrentState field.
func (r *queryResolver) DeviceCurrentState(ctx context.Context, deviceID string) (*model.DeviceCurrentState, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	telemetryRepository, err := r.telemetryRepositoryForQuery()
	if err != nil {
		return nil, err
	}
	parsedDeviceID, err := parseCanonicalUUID(deviceID)
	if err != nil {
		return nil, err
	}
	state, err := telemetryRepository.GetCurrentState(ctx, organizationID, parsedDeviceID)
	if errors.Is(err, projection.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return presentCurrentState(state), nil
}

// DeviceTelemetry is the resolver for the deviceTelemetry field.
func (r *queryResolver) DeviceTelemetry(ctx context.Context, deviceID string, first int, after *string) (*model.TelemetryConnection, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	telemetryRepository, err := r.telemetryRepositoryForQuery()
	if err != nil {
		return nil, err
	}
	parsedDeviceID, err := parseCanonicalUUID(deviceID)
	if err != nil {
		return nil, err
	}
	if first < 1 || first > projection.MaxPageSize {
		return nil, newPublicError(errorCodeBadUserInput, "first must be between 1 and 100", projection.ErrInvalidInput)
	}

	var cursor *projection.Cursor
	if after != nil {
		cursor, err = decodeTelemetryCursor(*after)
		if err != nil {
			return nil, err
		}
	}
	page, err := telemetryRepository.ListTelemetry(ctx, organizationID, parsedDeviceID, first, cursor)
	if err != nil {
		if errors.Is(err, projection.ErrInvalidInput) {
			return nil, newPublicError(errorCodeBadUserInput, "telemetry query input is invalid", err)
		}
		return nil, err
	}

	edges := make([]*model.TelemetryEdge, 0, len(page.Points))
	for _, point := range page.Points {
		pointCursor, cursorErr := encodeTelemetryCursor(projection.Cursor{ObservedAt: point.ObservedAt, MessageID: point.MessageID})
		if cursorErr != nil {
			return nil, cursorErr
		}
		edges = append(edges, &model.TelemetryEdge{
			Cursor: pointCursor,
			Node:   presentTelemetryPoint(point),
		})
	}

	pageInfo := &model.PageInfo{HasNextPage: page.NextCursor != nil}
	if len(edges) > 0 {
		endCursor := edges[len(edges)-1].Cursor
		pageInfo.EndCursor = &endCursor
	}
	return &model.TelemetryConnection{Edges: edges, PageInfo: pageInfo}, nil
}

// CreateThresholdRule creates one tenant-scoped temperature rule.
func (r *mutationResolver) CreateThresholdRule(ctx context.Context, input model.CreateThresholdRuleInput) (*model.ThresholdRule, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	rulesRepository, err := r.rulesRepositoryForMutation()
	if err != nil {
		return nil, err
	}
	deviceID, err := parseCanonicalUUID(input.DeviceID)
	if err != nil {
		return nil, err
	}
	created, err := rulesRepository.CreateRule(ctx, organizationID, rules.CreateRuleInput{
		DeviceID:         deviceID,
		Comparator:       rules.Comparator(input.Comparator),
		ThresholdCelsius: input.ThresholdCelsius,
		Enabled:          input.Enabled,
	})
	if err != nil {
		return nil, presentRuleMutationError(err)
	}
	return presentThresholdRule(created), nil
}

// UpdateThresholdRule replaces mutable fields only when the caller's revision
// still matches the stored rule.
func (r *mutationResolver) UpdateThresholdRule(ctx context.Context, input model.UpdateThresholdRuleInput) (*model.ThresholdRule, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	rulesRepository, err := r.rulesRepositoryForMutation()
	if err != nil {
		return nil, err
	}
	ruleID, err := parseCanonicalUUID(input.ID)
	if err != nil {
		return nil, err
	}
	updated, err := rulesRepository.UpdateRule(ctx, organizationID, rules.UpdateRuleInput{
		ID:               ruleID,
		ExpectedRevision: input.ExpectedRevision,
		Comparator:       rules.Comparator(input.Comparator),
		ThresholdCelsius: input.ThresholdCelsius,
		Enabled:          input.Enabled,
	})
	if err != nil {
		return nil, presentRuleMutationError(err)
	}
	return presentThresholdRule(updated), nil
}

// ThresholdRules returns a bounded rule list only for a device in the fixed
// tenant scope.
func (r *queryResolver) ThresholdRules(ctx context.Context, deviceID string) ([]*model.ThresholdRule, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	rulesRepository, err := r.rulesRepositoryForQuery()
	if err != nil {
		return nil, err
	}
	parsedDeviceID, err := parseCanonicalUUID(deviceID)
	if err != nil {
		return nil, err
	}
	ruleList, err := rulesRepository.ListRules(ctx, organizationID, parsedDeviceID)
	if err != nil {
		if errors.Is(err, rules.ErrNotFound) {
			return []*model.ThresholdRule{}, nil
		}
		return nil, presentRuleQueryError(err)
	}
	result := make([]*model.ThresholdRule, 0, len(ruleList))
	for _, rule := range ruleList {
		result = append(result, presentThresholdRule(rule))
	}
	return result, nil
}

// Alert returns an occurrence only inside the fixed tenant scope.
func (r *queryResolver) Alert(ctx context.Context, id string) (*model.AlertOccurrence, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	rulesRepository, err := r.rulesRepositoryForQuery()
	if err != nil {
		return nil, err
	}
	alertID, err := parseCanonicalUUID(id)
	if err != nil {
		return nil, err
	}
	alert, err := rulesRepository.GetAlert(ctx, organizationID, alertID)
	if errors.Is(err, rules.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, presentRuleQueryError(err)
	}
	return presentAlert(alert), nil
}

// Alerts returns tenant-scoped, bounded occurrence history.
func (r *queryResolver) Alerts(ctx context.Context, first int, after *string, deviceID *string) (*model.AlertConnection, error) {
	organizationID, err := r.authority(ctx)
	if err != nil {
		return nil, err
	}
	rulesRepository, err := r.rulesRepositoryForQuery()
	if err != nil {
		return nil, err
	}
	if first < 1 || first > rules.MaxAlertPageSize {
		return nil, newPublicError(errorCodeBadUserInput, "first must be between 1 and 100", rules.ErrInvalidInput)
	}
	var filter *uuid.UUID
	if deviceID != nil {
		parsedDeviceID, parseErr := parseCanonicalUUID(*deviceID)
		if parseErr != nil {
			return nil, parseErr
		}
		filter = &parsedDeviceID
	}
	var cursor *rules.AlertCursor
	if after != nil {
		cursor, err = decodeAlertCursor(*after)
		if err != nil {
			return nil, err
		}
	}
	page, err := rulesRepository.ListAlerts(ctx, organizationID, first, filter, cursor)
	if err != nil {
		return nil, presentRuleQueryError(err)
	}
	edges := make([]*model.AlertEdge, 0, len(page.Alerts))
	for _, alert := range page.Alerts {
		alertCursor, cursorErr := encodeAlertCursor(rules.AlertCursor{
			CreatedAt:      alert.CreatedAt,
			ID:             alert.ID,
			OrganizationID: organizationID,
			DeviceFilter:   cloneUUID(filter),
		})
		if cursorErr != nil {
			return nil, cursorErr
		}
		edges = append(edges, &model.AlertEdge{Cursor: alertCursor, Node: presentAlert(alert)})
	}
	pageInfo := &model.PageInfo{HasNextPage: page.NextCursor != nil}
	if len(edges) > 0 {
		endCursor := edges[len(edges)-1].Cursor
		pageInfo.EndCursor = &endCursor
	}
	return &model.AlertConnection{Edges: edges, PageInfo: pageInfo}, nil
}

func (r *queryResolver) telemetryRepositoryForQuery() (TelemetryRepository, error) {
	if r.telemetryRepository == nil {
		return nil, newPublicError(errorCodeInternal, "telemetry repository is unavailable", errors.New("telemetry repository is not configured"))
	}
	return r.telemetryRepository, nil
}

func (r *Resolver) rulesRepositoryForQuery() (ThresholdRuleRepository, error) {
	if r.rulesRepository == nil {
		return nil, newPublicError(errorCodeInternal, "threshold rules repository is unavailable", errors.New("threshold rules repository is not configured"))
	}
	return r.rulesRepository, nil
}

func (r *Resolver) rulesRepositoryForMutation() (ThresholdRuleRepository, error) {
	return r.rulesRepositoryForQuery()
}

func presentDevice(device registry.Device) *model.Device {
	return &model.Device{
		ID:          device.ID.String(),
		DeviceKey:   device.DeviceKey,
		DisplayName: device.DisplayName,
		CreatedAt:   device.CreatedAt.UTC(),
	}
}

func presentCurrentState(state projection.CurrentState) *model.DeviceCurrentState {
	return &model.DeviceCurrentState{
		MessageID:          state.MessageID.String(),
		ObservedAt:         state.ObservedAt.UTC(),
		ReceivedAt:         state.ReceivedAt.UTC(),
		TemperatureCelsius: state.TemperatureCelsius,
		LastSeenAt:         state.LastSeenAt.UTC(),
	}
}

func presentTelemetryPoint(point projection.TelemetryPoint) *model.TelemetryPoint {
	return &model.TelemetryPoint{
		MessageID:          point.MessageID.String(),
		ObservedAt:         point.ObservedAt.UTC(),
		ReceivedAt:         point.ReceivedAt.UTC(),
		TemperatureCelsius: point.TemperatureCelsius,
	}
}

func presentThresholdRule(rule rules.Rule) *model.ThresholdRule {
	return &model.ThresholdRule{
		ID:               rule.ID.String(),
		DeviceID:         rule.DeviceID.String(),
		Metric:           model.ThresholdMetricTemperatureCelsius,
		Comparator:       model.ThresholdComparator(rule.Comparator),
		ThresholdCelsius: rule.ThresholdCelsius,
		Enabled:          rule.Enabled,
		Revision:         rule.Revision,
		CreatedAt:        rule.CreatedAt.UTC(),
		UpdatedAt:        rule.UpdatedAt.UTC(),
	}
}

func presentAlert(alert rules.Alert) *model.AlertOccurrence {
	return &model.AlertOccurrence{
		ID:                 alert.ID.String(),
		DeviceID:           alert.DeviceID.String(),
		RuleID:             alert.RuleID.String(),
		MessageID:          alert.MessageID.String(),
		ObservedAt:         alert.ObservedAt.UTC(),
		ReceivedAt:         alert.ReceivedAt.UTC(),
		TemperatureCelsius: alert.TemperatureCelsius,
		Metric:             model.ThresholdMetricTemperatureCelsius,
		Comparator:         model.ThresholdComparator(alert.Comparator),
		ThresholdCelsius:   alert.ThresholdCelsius,
		CreatedAt:          alert.CreatedAt.UTC(),
	}
}

func presentRuleMutationError(err error) error {
	switch {
	case errors.Is(err, rules.ErrInvalidInput), errors.Is(err, rules.ErrNotFound):
		return newPublicError(errorCodeBadUserInput, "threshold rule input is invalid", err)
	case errors.Is(err, rules.ErrConflict), errors.Is(err, rules.ErrRuleLimitReached):
		return newPublicError(errorCodeConflict, "threshold rule conflicts with current state", err)
	default:
		return err
	}
}

func presentRuleQueryError(err error) error {
	if errors.Is(err, rules.ErrInvalidInput) {
		return newPublicError(errorCodeBadUserInput, "threshold rule query input is invalid", err)
	}
	return err
}

func cloneUUID(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
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
