package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/rules"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/telemetry/projection"
)

type fakeThresholdRuleRepository struct {
	mu             sync.Mutex
	organizationID uuid.UUID
	deviceID       uuid.UUID
	rule           rules.Rule
	alert          rules.Alert
	page           rules.AlertPage
	createErr      error
	updateErr      error
	listErr        error
	getErr         error
	lastOrg        uuid.UUID
	lastCreate     rules.CreateRuleInput
	lastUpdate     rules.UpdateRuleInput
	lastPageSize   int
	lastDevice     *uuid.UUID
	lastCursor     *rules.AlertCursor
}

func (f *fakeThresholdRuleRepository) CreateRule(_ context.Context, organizationID uuid.UUID, input rules.CreateRuleInput) (rules.Rule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOrg = organizationID
	f.lastCreate = input
	if f.createErr != nil {
		return rules.Rule{}, f.createErr
	}
	if organizationID != f.organizationID || input.DeviceID != f.deviceID {
		return rules.Rule{}, rules.ErrNotFound
	}
	if _, err := rules.Compare(input.Comparator, input.ThresholdCelsius, input.ThresholdCelsius); err != nil {
		return rules.Rule{}, err
	}
	f.rule = rules.Rule{
		ID: uuid.New(), DeviceID: input.DeviceID, Metric: rules.MetricTemperatureCelsius,
		Comparator: input.Comparator, ThresholdCelsius: input.ThresholdCelsius,
		Enabled: input.Enabled, Revision: 1,
		CreatedAt: time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC),
	}
	return f.rule, nil
}

func (f *fakeThresholdRuleRepository) UpdateRule(_ context.Context, organizationID uuid.UUID, input rules.UpdateRuleInput) (rules.Rule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOrg = organizationID
	f.lastUpdate = input
	if f.updateErr != nil {
		return rules.Rule{}, f.updateErr
	}
	if organizationID != f.organizationID || input.ID != f.rule.ID {
		return rules.Rule{}, rules.ErrNotFound
	}
	if _, err := rules.Compare(input.Comparator, input.ThresholdCelsius, input.ThresholdCelsius); err != nil {
		return rules.Rule{}, err
	}
	if input.ExpectedRevision != f.rule.Revision {
		return rules.Rule{}, rules.ErrConflict
	}
	f.rule.Comparator = input.Comparator
	f.rule.ThresholdCelsius = input.ThresholdCelsius
	f.rule.Enabled = input.Enabled
	f.rule.Revision++
	return f.rule, nil
}

func (f *fakeThresholdRuleRepository) ListRules(_ context.Context, organizationID, deviceID uuid.UUID) ([]rules.Rule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOrg = organizationID
	if f.listErr != nil {
		return nil, f.listErr
	}
	if organizationID != f.organizationID || deviceID != f.deviceID || f.rule.ID == uuid.Nil {
		return []rules.Rule{}, nil
	}
	return []rules.Rule{f.rule}, nil
}

func (f *fakeThresholdRuleRepository) GetAlert(_ context.Context, organizationID, alertID uuid.UUID) (rules.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOrg = organizationID
	if f.getErr != nil {
		return rules.Alert{}, f.getErr
	}
	if organizationID != f.organizationID || alertID != f.alert.ID {
		return rules.Alert{}, rules.ErrNotFound
	}
	return f.alert, nil
}

func (f *fakeThresholdRuleRepository) ListAlerts(_ context.Context, organizationID uuid.UUID, pageSize int, deviceFilter *uuid.UUID, cursor *rules.AlertCursor) (rules.AlertPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOrg = organizationID
	f.lastPageSize = pageSize
	f.lastDevice = deviceFilter
	f.lastCursor = cursor
	if f.listErr != nil {
		return rules.AlertPage{}, f.listErr
	}
	if cursor != nil && (cursor.OrganizationID != organizationID || !sameAlertDevice(cursor.DeviceFilter, deviceFilter)) {
		return rules.AlertPage{}, rules.ErrInvalidInput
	}
	if organizationID != f.organizationID || (deviceFilter != nil && *deviceFilter != f.deviceID) {
		return rules.AlertPage{Alerts: []rules.Alert{}}, nil
	}
	return f.page, nil
}

func TestGraphQLThresholdRuleMutationsAndTenantSafeReads(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	foreignDeviceID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	repository := &fakeThresholdRuleRepository{organizationID: organizationID, deviceID: deviceID}
	handler, err := NewHandlerWithRules(&fakeRepository{organizationID: organizationID}, nil, repository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandlerWithRules returned error: %v", err)
	}
	created := doGraphQL(t, handler, `{ "query": "mutation { createThresholdRule(input: { deviceId: \"22222222-2222-4222-8222-222222222222\", comparator: GT, thresholdCelsius: 25 }) { id deviceId metric comparator thresholdCelsius enabled revision } }" }`, "")
	if len(created.Errors) != 0 {
		t.Fatalf("create rule errors = %+v", created.Errors)
	}
	var createdData struct {
		CreateThresholdRule struct {
			ID               string  `json:"id"`
			DeviceID         string  `json:"deviceId"`
			Metric           string  `json:"metric"`
			Comparator       string  `json:"comparator"`
			ThresholdCelsius float64 `json:"thresholdCelsius"`
			Enabled          bool    `json:"enabled"`
			Revision         int     `json:"revision"`
		} `json:"createThresholdRule"`
	}
	decodeData(t, created, &createdData)
	if !createdData.CreateThresholdRule.Enabled || createdData.CreateThresholdRule.Metric != rules.MetricTemperatureCelsius || createdData.CreateThresholdRule.Revision != 1 {
		t.Fatalf("created rule data = %+v", createdData.CreateThresholdRule)
	}
	if repository.lastOrg != organizationID || repository.lastCreate.DeviceID != deviceID || !repository.lastCreate.Enabled {
		t.Fatalf("create authority/input = %s %+v", repository.lastOrg, repository.lastCreate)
	}

	updated := doGraphQL(t, handler, `{ "query": "mutation { updateThresholdRule(input: { id: \"`+createdData.CreateThresholdRule.ID+`\", expectedRevision: 1, comparator: GTE, thresholdCelsius: 30, enabled: false }) { revision comparator enabled } }" }`, "")
	if len(updated.Errors) != 0 || repository.lastUpdate.ExpectedRevision != 1 || repository.lastOrg != organizationID {
		t.Fatalf("update rule response = %+v authority=%s input=%+v", updated, repository.lastOrg, repository.lastUpdate)
	}
	repository.updateErr = rules.ErrConflict
	stale := doGraphQL(t, handler, `{ "query": "mutation { updateThresholdRule(input: { id: \"`+createdData.CreateThresholdRule.ID+`\", expectedRevision: 1, comparator: GTE, thresholdCelsius: 30, enabled: false }) { id } }" }`, "")
	assertErrorCode(t, stale, errorCodeConflict)

	foreign := doGraphQL(t, handler, `{ "query": "query { thresholdRules(deviceId: \"`+foreignDeviceID.String()+`\") { id } alert(id: \"`+uuid.NewString()+`\") { id } }" }`, "")
	if len(foreign.Errors) != 0 || strings.Contains(string(foreign.Data), `"id"`) {
		t.Fatalf("foreign read response = %+v", foreign)
	}
	if repository.lastOrg != organizationID {
		t.Fatalf("resolver used organization %s, want fixed authority %s", repository.lastOrg, organizationID)
	}

	status, invalid := doGraphQLWithStatus(t, handler, `{ "query": "mutation { createThresholdRule(input: { deviceId: \"`+deviceID.String()+`\", comparator: GT, thresholdCelsius: 1e309 }) { id } }" }`)
	if status != 400 || len(invalid.Errors) == 0 {
		t.Fatalf("non-finite threshold response = status %d, %+v", status, invalid)
	}
}

func TestGraphQLAlertConnectionBindsCursorToTenantAndFilter(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	alert := rules.Alert{
		ID:                 uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		RuleID:             uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		DeviceID:           deviceID,
		MessageID:          uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		ObservedAt:         time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC),
		ReceivedAt:         time.Date(2026, 9, 24, 9, 0, 1, 0, time.UTC),
		TemperatureCelsius: 31,
		Metric:             rules.MetricTemperatureCelsius,
		Comparator:         rules.GreaterThan,
		ThresholdCelsius:   30,
		CreatedAt:          time.Date(2026, 9, 24, 9, 0, 2, 123456000, time.UTC),
	}
	filter := deviceID
	cursor := &rules.AlertCursor{CreatedAt: alert.CreatedAt, ID: alert.ID, OrganizationID: organizationID, DeviceFilter: &filter}
	repository := &fakeThresholdRuleRepository{
		organizationID: organizationID,
		deviceID:       deviceID,
		alert:          alert,
		page:           rules.AlertPage{Alerts: []rules.Alert{alert}, NextCursor: cursor},
	}
	handler, err := NewHandlerWithRules(&fakeRepository{organizationID: organizationID}, nil, repository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandlerWithRules returned error: %v", err)
	}
	first := doGraphQL(t, handler, `{ "query": "query { alerts(first: 1, deviceId: \"22222222-2222-4222-8222-222222222222\") { edges { cursor node { id deviceId ruleId messageId observedAt receivedAt temperatureCelsius comparator thresholdCelsius } } pageInfo { endCursor hasNextPage } } }" }`, "")
	if len(first.Errors) != 0 {
		t.Fatalf("alert list errors = %+v", first.Errors)
	}
	var firstData struct {
		Alerts struct {
			Edges []struct {
				Cursor string `json:"cursor"`
			} `json:"edges"`
		} `json:"alerts"`
	}
	decodeData(t, first, &firstData)
	if len(firstData.Alerts.Edges) != 1 || firstData.Alerts.Edges[0].Cursor == "" || repository.lastPageSize != 1 || repository.lastOrg != organizationID {
		t.Fatalf("alert list data/authority = %+v org=%s size=%d", firstData, repository.lastOrg, repository.lastPageSize)
	}
	cursorString := firstData.Alerts.Edges[0].Cursor
	continued := doGraphQL(t, handler, `{ "query": "query { alerts(first: 1, after: \"`+cursorString+`\", deviceId: \"22222222-2222-4222-8222-222222222222\") { edges { cursor } pageInfo { hasNextPage } } }" }`, "")
	if len(continued.Errors) != 0 || repository.lastCursor == nil {
		t.Fatalf("alert cursor continuation = %+v cursor=%+v", continued, repository.lastCursor)
	}

	wrongFilter := doGraphQL(t, handler, `{ "query": "query { alerts(first: 1, after: \"`+cursorString+`\", deviceId: \"44444444-4444-4444-8444-444444444444\") { edges { cursor } } }" }`, "")
	assertErrorCode(t, wrongFilter, errorCodeBadUserInput)

	malformedCursor := doGraphQL(t, handler, `{ "query": "query { alerts(first: 1, after: \"not-a-cursor\") { edges { cursor } } }" }`, "")
	assertErrorCode(t, malformedCursor, errorCodeBadUserInput)

	telemetryCursor, err := encodeTelemetryCursor(projection.Cursor{
		ObservedAt: alert.ObservedAt,
		MessageID:  alert.MessageID,
	})
	if err != nil {
		t.Fatalf("encode telemetry cursor: %v", err)
	}
	wrongTypeCursor := doGraphQL(t, handler, `{ "query": "query { alerts(first: 1, after: \"`+telemetryCursor+`\") { edges { cursor } } }" }`, "")
	assertErrorCode(t, wrongTypeCursor, errorCodeBadUserInput)

	foreignOrganizationCursor, err := encodeAlertCursor(rules.AlertCursor{
		CreatedAt: alert.CreatedAt, ID: alert.ID,
		OrganizationID: uuid.MustParse("77777777-7777-4777-8777-777777777777"),
		DeviceFilter:   &filter,
	})
	if err != nil {
		t.Fatalf("encode foreign organization cursor: %v", err)
	}
	foreignOrganization := doGraphQL(t, handler, `{ "query": "query { alerts(first: 1, after: \"`+foreignOrganizationCursor+`\", deviceId: \"22222222-2222-4222-8222-222222222222\") { edges { cursor } } }" }`, "")
	assertErrorCode(t, foreignOrganization, errorCodeBadUserInput)

	oversized := doGraphQL(t, handler, `{ "query": "query { alerts(first: 101) { edges { cursor } } }" }`, "")
	assertErrorCode(t, oversized, errorCodeBadUserInput)
}

func TestGraphQLAlertsApplyDefaultMaximumAndComplexityBounds(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	repository := &fakeThresholdRuleRepository{organizationID: organizationID, deviceID: deviceID}
	handler, err := NewHandlerWithRules(&fakeRepository{organizationID: organizationID}, nil, repository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandlerWithRules returned error: %v", err)
	}
	defaultPage := doGraphQL(t, handler, `{ "query": "query { alerts { edges { cursor } pageInfo { hasNextPage } } }" }`, "")
	if len(defaultPage.Errors) != 0 || repository.lastPageSize != rules.DefaultAlertPageSize {
		t.Fatalf("default alert page errors=%+v size=%d, want %d", defaultPage.Errors, repository.lastPageSize, rules.DefaultAlertPageSize)
	}
	maximumPage := doGraphQL(t, handler, `{ "query": "query { alerts(first: 100) { edges { cursor } pageInfo { hasNextPage } } }" }`, "")
	if len(maximumPage.Errors) != 0 || repository.lastPageSize != rules.MaxAlertPageSize {
		t.Fatalf("maximum alert page errors=%+v size=%d, want %d", maximumPage.Errors, repository.lastPageSize, rules.MaxAlertPageSize)
	}
	oversized := doGraphQL(t, handler, `{ "query": "query { alerts(first: 101) { edges { cursor } } }" }`, "")
	assertErrorCode(t, oversized, errorCodeBadUserInput)

	fullFields := `edges { node { id deviceId ruleId messageId observedAt receivedAt temperatureCelsius metric comparator thresholdCelsius createdAt } } pageInfo { endCursor hasNextPage }`
	status, complex := doGraphQLWithStatus(t, handler, `{ "query": "query { first: alerts(first: 100) { `+fullFields+` } second: alerts(first: 100) { `+fullFields+` } }" }`)
	if status != http.StatusBadRequest {
		t.Fatalf("repeated maximum alert query status=%d, want %d; response=%+v", status, http.StatusBadRequest, complex)
	}
	assertErrorCode(t, complex, errorCodeBadUserInput)
}

func TestGraphQLThresholdRulesComplexityAccountsForRuleBound(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	repository := &fakeThresholdRuleRepository{organizationID: organizationID, deviceID: deviceID}
	handler, err := NewHandlerWithRules(&fakeRepository{organizationID: organizationID}, nil, repository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandlerWithRules returned error: %v", err)
	}

	query := "query {"
	for alias := range 9 {
		query += fmt.Sprintf(" rule%d: thresholdRules(deviceId: %q) { id deviceId metric comparator thresholdCelsius enabled revision createdAt updatedAt }", alias, deviceID.String())
	}
	query += " }"
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		t.Fatalf("encode complexity query: %v", err)
	}
	status, response := doGraphQLWithStatus(t, handler, string(body))
	if status != http.StatusBadRequest {
		t.Fatalf("repeated threshold-rule query status=%d, want %d; response=%+v", status, http.StatusBadRequest, response)
	}
	assertErrorCode(t, response, errorCodeBadUserInput)
}

func sameAlertDevice(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

var _ ThresholdRuleRepository = (*fakeThresholdRuleRepository)(nil)
