package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
)

type fakeRepository struct {
	organizationID uuid.UUID
	devices        []registry.Device
	createErr      error
	getErr         error
	listErr        error
	panicOnCreate  bool
	lastOrg        uuid.UUID
}

func (f *fakeRepository) CreateDevice(_ context.Context, organizationID uuid.UUID, input registry.CreateDeviceInput) (registry.Device, error) {
	f.lastOrg = organizationID
	if f.panicOnCreate {
		panic("resolver panic: database password=super-secret")
	}
	if f.createErr != nil {
		return registry.Device{}, f.createErr
	}
	device := registry.Device{
		ID:             uuid.New(),
		OrganizationID: organizationID,
		DeviceKey:      input.DeviceKey,
		DisplayName:    input.DisplayName,
		CreatedAt:      time.Date(2026, 9, 16, 8, 0, 0, 123456789, time.UTC),
	}
	f.devices = append([]registry.Device{device}, f.devices...)
	return device, nil
}

func (f *fakeRepository) GetDevice(_ context.Context, organizationID uuid.UUID, deviceID uuid.UUID) (registry.Device, error) {
	f.lastOrg = organizationID
	if f.getErr != nil {
		return registry.Device{}, f.getErr
	}
	for _, device := range f.devices {
		if device.ID == deviceID && device.OrganizationID == organizationID {
			return device, nil
		}
	}
	return registry.Device{}, registry.ErrNotFound
}

func (f *fakeRepository) ListDevices(_ context.Context, organizationID uuid.UUID, pageSize int, cursor *registry.Cursor) (registry.DevicePage, error) {
	f.lastOrg = organizationID
	if f.listErr != nil {
		return registry.DevicePage{}, f.listErr
	}
	start := 0
	if cursor != nil {
		for index, device := range f.devices {
			if device.ID == cursor.ID && device.CreatedAt.Equal(cursor.CreatedAt) {
				start = index + 1
				break
			}
		}
	}
	if start > len(f.devices) {
		start = len(f.devices)
	}
	end := start + pageSize
	if end > len(f.devices) {
		end = len(f.devices)
	}
	page := registry.DevicePage{Devices: append([]registry.Device(nil), f.devices[start:end]...)}
	if end < len(f.devices) {
		next := f.devices[end-1]
		page.NextCursor = &registry.Cursor{CreatedAt: next.CreatedAt, ID: next.ID}
	}
	return page, nil
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

func TestGraphQLDeviceContractAndTenantAuthority(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	deviceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	repository := &fakeRepository{
		organizationID: organizationID,
		devices: []registry.Device{
			{
				ID:             deviceID,
				OrganizationID: organizationID,
				DeviceKey:      "sensor-1",
				DisplayName:    "Temperature",
				CreatedAt:      time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC),
			},
		},
	}
	handler, err := NewHandler(repository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}

	mutation := doGraphQL(t, handler, `{ "query": "mutation { createDevice(input: { deviceKey: \"sensor-2\", displayName: \"Humidity\" }) { id deviceKey displayName createdAt } }" }`, "")
	if len(mutation.Errors) != 0 {
		t.Fatalf("mutation errors = %+v", mutation.Errors)
	}
	if repository.lastOrg != organizationID {
		t.Fatalf("repository organization = %s, want fixed %s", repository.lastOrg, organizationID)
	}
	var mutationData struct {
		CreateDevice struct {
			ID          string    `json:"id"`
			DeviceKey   string    `json:"deviceKey"`
			DisplayName string    `json:"displayName"`
			CreatedAt   time.Time `json:"createdAt"`
		} `json:"createDevice"`
	}
	decodeData(t, mutation, &mutationData)
	if mutationData.CreateDevice.DeviceKey != "sensor-2" || mutationData.CreateDevice.DisplayName != "Humidity" {
		t.Fatalf("mutation data = %+v", mutationData)
	}
	if mutationData.CreateDevice.CreatedAt.Location() != time.UTC {
		t.Fatalf("createdAt location = %v, want UTC", mutationData.CreateDevice.CreatedAt.Location())
	}

	page := doGraphQL(t, handler, `{ "query": "query { devices(first: 1) { edges { cursor node { id deviceKey } } pageInfo { endCursor hasNextPage } } }" }`, "*/*")
	if len(page.Errors) != 0 {
		t.Fatalf("page errors = %+v", page.Errors)
	}
	var pageData struct {
		Devices struct {
			Edges []struct {
				Cursor string `json:"cursor"`
				Node   struct {
					ID string `json:"id"`
				} `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				EndCursor   *string `json:"endCursor"`
				HasNextPage bool    `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"devices"`
	}
	decodeData(t, page, &pageData)
	if len(pageData.Devices.Edges) != 1 || pageData.Devices.PageInfo.EndCursor == nil || !pageData.Devices.PageInfo.HasNextPage {
		t.Fatalf("page data = %+v", pageData)
	}
	if *pageData.Devices.PageInfo.EndCursor != pageData.Devices.Edges[0].Cursor {
		t.Fatalf("end cursor = %q, edge cursor = %q", *pageData.Devices.PageInfo.EndCursor, pageData.Devices.Edges[0].Cursor)
	}

	missing := doGraphQL(t, handler, `{ "query": "query { device(id: \"33333333-3333-4333-8333-333333333333\") { id } }" }`, "")
	if len(missing.Errors) != 0 || string(missing.Data) == "null" {
		t.Fatalf("missing device response = %+v", missing)
	}

	spoofed := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"{ devices(first: 1) { edges { node { id } } } }"}`))
	spoofed.Header.Set("Content-Type", "application/json")
	spoofed.Header.Set("X-Organization-ID", "33333333-3333-4333-8333-333333333333")
	spoofed = spoofed.WithContext(WithRequestContext(spoofed.Context(), "spoofed-tenant-request", Principal{OrganizationID: organizationID}))
	spoofedResponse := httptest.NewRecorder()
	handler.ServeHTTP(spoofedResponse, spoofed)
	if spoofedResponse.Code != http.StatusOK {
		t.Fatalf("spoofed tenant status = %d, body = %s", spoofedResponse.Code, spoofedResponse.Body.String())
	}
	if repository.lastOrg != organizationID {
		t.Fatalf("spoofed tenant repository organization = %s, want fixed %s", repository.lastOrg, organizationID)
	}
}

func TestGraphQLRejectsInvalidInputAndMedia(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	repository := &fakeRepository{organizationID: organizationID}
	handler, err := NewHandler(repository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}

	invalidFirst := doGraphQL(t, handler, `{ "query": "query { devices(first: 0) { edges { cursor } } }" }`, "")
	assertErrorCode(t, invalidFirst, errorCodeBadUserInput)
	if string(invalidFirst.Data) != "null" {
		t.Fatalf("invalid first data = %s, want null", invalidFirst.Data)
	}

	invalidID := doGraphQL(t, handler, `{ "query": "query { device(id: \"not-a-uuid\") { id } }" }`, "")
	assertErrorCode(t, invalidID, errorCodeBadUserInput)

	request := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"{ devices { edges { cursor } } }"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request = request.WithContext(WithRequestContext(request.Context(), "media-request", Principal{OrganizationID: organizationID}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotAcceptable {
		t.Fatalf("unsupported accept status = %d, want %d", response.Code, http.StatusNotAcceptable)
	}
	if !strings.Contains(response.Body.String(), errorCodeBadUserInput) {
		t.Fatalf("unsupported accept body = %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"requestId":"media-request"`) {
		t.Fatalf("unsupported accept request id missing = %s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"{ devices { edges { cursor } } }"}`))
	request.Header.Set("Content-Type", "text/plain")
	request = request.WithContext(WithRequestContext(request.Context(), "content-type-request", Principal{OrganizationID: organizationID}))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType || !strings.Contains(response.Body.String(), `"requestId":"content-type-request"`) {
		t.Fatalf("unsupported content type response = %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"{ devices { edges { cursor } } }"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Accept", "application/graphql-response+json")
	request = request.WithContext(WithRequestContext(request.Context(), "multi-accept-request", Principal{OrganizationID: organizationID}))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("multiple accept response = %d, body = %s", response.Code, response.Body.String())
	}

	unknownTenantRequest := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"mutation { createDevice(input: { deviceKey: \"sensor-3\", displayName: \"Pressure\", organizationId: \"33333333-3333-4333-8333-333333333333\" }) { id } }"}`))
	unknownTenantRequest.Header.Set("Content-Type", "application/json")
	unknownTenantRequest = unknownTenantRequest.WithContext(WithRequestContext(unknownTenantRequest.Context(), "unknown-tenant-field-request", Principal{OrganizationID: organizationID}))
	unknownTenantResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownTenantResponse, unknownTenantRequest)
	if unknownTenantResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown tenant field status = %d, body = %s", unknownTenantResponse.Code, unknownTenantResponse.Body.String())
	}
	var unknownTenantBody graphqlResponse
	if err := json.NewDecoder(unknownTenantResponse.Body).Decode(&unknownTenantBody); err != nil {
		t.Fatalf("decode unknown tenant field response: %v", err)
	}
	assertErrorCode(t, unknownTenantBody, "GRAPHQL_VALIDATION_FAILED")

	request = httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if strings.Contains(response.Body.String(), `{"query":`) {
		t.Fatalf("malformed JSON echoed request body: %s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`[{"query":"{ __typename }"}]`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), errorCodeBadUserInput) {
		t.Fatalf("batched request response = %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"subscription { devices { edges { cursor } } }"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "GRAPHQL_VALIDATION_FAILED") {
		t.Fatalf("subscription response = %d %s", response.Code, response.Body.String())
	}
}

func TestAcceptsGraphQLResponseNegotiatesSupportedRangesAndQuality(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{name: "empty", header: "", want: true},
		{name: "wildcard", header: "*/*", want: true},
		{name: "supported with fallback", header: "application/graphql-response+json, application/json;q=0.9", want: true},
		{name: "supported after unsupported", header: "application/json, application/graphql-response+json", want: true},
		{name: "zero quality", header: "application/graphql-response+json;q=0", want: false},
		{name: "exact zero overrides wildcard", header: "*/*;q=1, application/graphql-response+json;q=0", want: false},
		{name: "exact quality overrides wildcard", header: "*/*;q=0, application/graphql-response+json;q=1", want: true},
		{name: "application wildcard", header: "application/*", want: true},
		{name: "application wildcard zero overrides global", header: "*/*;q=1, application/*;q=0", want: false},
		{name: "wildcard zero quality", header: "*/*;q=0", want: false},
		{name: "unsupported only", header: "application/json", want: false},
		{name: "invalid quality", header: "application/graphql-response+json;q=invalid", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := acceptsGraphQLResponse(test.header); got != test.want {
				t.Fatalf("acceptsGraphQLResponse(%q) = %t, want %t", test.header, got, test.want)
			}
		})
	}
}

func TestGraphQLRequiresTrustedPrincipal(t *testing.T) {
	organizationID := uuid.New()
	handler, err := NewHandler(&fakeRepository{organizationID: organizationID}, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"{ devices { edges { cursor } } }"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body graphqlResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertErrorCode(t, body, errorCodeInternal)
}

func TestGraphQLProtocolAndComplexityErrorsUseStableCodes(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	handler, err := NewHandler(&fakeRepository{organizationID: organizationID}, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"query {"}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(WithRequestContext(request.Context(), "protocol-request", Principal{OrganizationID: organizationID}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("parse status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var parseResponse graphqlResponse
	if err := json.NewDecoder(response.Body).Decode(&parseResponse); err != nil {
		t.Fatalf("decode parse response: %v", err)
	}
	assertErrorCode(t, parseResponse, "GRAPHQL_PARSE_FAILED")
	if got, _ := parseResponse.Errors[0].Extensions["requestId"].(string); got != "protocol-request" {
		t.Fatalf("parse request id = %q", got)
	}

	request = httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"query { unknownField }"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("validation status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var validationResponse graphqlResponse
	if err := json.NewDecoder(response.Body).Decode(&validationResponse); err != nil {
		t.Fatalf("decode validation response: %v", err)
	}
	assertErrorCode(t, validationResponse, "GRAPHQL_VALIDATION_FAILED")

	canonical := doGraphQL(t, handler, `{ "query": "query { devices(first: 100) { edges { cursor node { id deviceKey displayName createdAt } } pageInfo { endCursor hasNextPage } } }" }`, "")
	if len(canonical.Errors) != 0 {
		t.Fatalf("canonical maximum-page query errors = %+v", canonical.Errors)
	}

	request = httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"query { first: devices(first: 100) { edges { cursor node { id deviceKey displayName createdAt } } pageInfo { endCursor hasNextPage } } second: devices(first: 100) { edges { cursor node { id deviceKey displayName createdAt } } pageInfo { endCursor hasNextPage } } }"}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(WithRequestContext(request.Context(), "complexity-request", Principal{OrganizationID: organizationID}))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("complexity status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var complexityResponse graphqlResponse
	if err := json.NewDecoder(response.Body).Decode(&complexityResponse); err != nil {
		t.Fatalf("decode complexity response: %v", err)
	}
	assertErrorCode(t, complexityResponse, errorCodeBadUserInput)

	fragmentRequest := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", strings.NewReader(`{"query":"query { first: devices(first: 100) { edges { node { ...DeviceFields } } pageInfo { endCursor hasNextPage } } second: devices(first: 100) { edges { node { ...DeviceFields } } pageInfo { endCursor hasNextPage } } } fragment DeviceFields on Device { id deviceKey displayName createdAt }"}`))
	fragmentRequest.Header.Set("Content-Type", "application/json")
	fragmentRequest = fragmentRequest.WithContext(WithRequestContext(fragmentRequest.Context(), "fragment-complexity-request", Principal{OrganizationID: organizationID}))
	fragmentResponse := httptest.NewRecorder()
	handler.ServeHTTP(fragmentResponse, fragmentRequest)
	if fragmentResponse.Code != http.StatusBadRequest {
		t.Fatalf("fragment complexity status = %d, want %d; body = %s", fragmentResponse.Code, http.StatusBadRequest, fragmentResponse.Body.String())
	}
	var fragmentComplexityResponse graphqlResponse
	if err := json.NewDecoder(fragmentResponse.Body).Decode(&fragmentComplexityResponse); err != nil {
		t.Fatalf("decode fragment complexity response: %v", err)
	}
	assertErrorCode(t, fragmentComplexityResponse, errorCodeBadUserInput)
}

func TestGraphQLMapsRepositoryFailuresWithoutLeakingDetails(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	repository := &fakeRepository{organizationID: organizationID, createErr: registry.ErrConflict}
	handler, err := NewHandler(repository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	response := doGraphQL(t, handler, `{ "query": "mutation { createDevice(input: { deviceKey: \"duplicate\", displayName: \"Duplicate\" }) { id } }" }`, "")
	assertErrorCode(t, response, errorCodeConflict)
	if strings.Contains(response.Errors[0].Message, "registry") {
		t.Fatalf("repository error leaked: %+v", response.Errors[0])
	}
}

func TestGraphQLRecoversResolverPanicWithoutLeakingDetails(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	repository := &fakeRepository{organizationID: organizationID, panicOnCreate: true}
	handler, err := NewHandler(repository, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	response := doGraphQL(t, handler, `{ "query": "mutation { createDevice(input: { deviceKey: \"panic\", displayName: \"Panic\" }) { id } }" }`, "")
	assertErrorCode(t, response, errorCodeInternal)
	if strings.Contains(response.Errors[0].Message, "super-secret") || strings.Contains(strings.ToLower(response.Errors[0].Message), "panic") {
		t.Fatalf("panic details leaked: %+v", response.Errors[0])
	}
}

func TestGraphQLParserTokenLimitBoundsLargeDocuments(t *testing.T) {
	organizationID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	handler, err := NewHandler(&fakeRepository{organizationID: organizationID}, organizationID, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}
	var query strings.Builder
	query.WriteString("query { ")
	for index := range 400 {
		fmt.Fprintf(&query, "field%d: __typename ", index)
	}
	query.WriteString("}")
	body, err := json.Marshal(map[string]string{"query": query.String()})
	if err != nil {
		t.Fatalf("encode large query: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("large query status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var parsed graphqlResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode large query response: %v", err)
	}
	assertErrorCode(t, parsed, "GRAPHQL_PARSE_FAILED")
}

func doGraphQL(t *testing.T, handler http.Handler, body string, accept string) graphqlResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://example.test/graphql", bytes.NewBufferString(body))
	request = request.WithContext(WithRequestContext(request.Context(), "test-request-001", Principal{OrganizationID: uuid.MustParse("11111111-1111-4111-8111-111111111111")}))
	request.Header.Set("Content-Type", "application/json")
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GraphQL status = %d, body = %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/graphql-response+json") {
		t.Fatalf("GraphQL content type = %q, want application/graphql-response+json", contentType)
	}
	var bodyResponse graphqlResponse
	if err := json.NewDecoder(response.Body).Decode(&bodyResponse); err != nil {
		t.Fatalf("decode GraphQL response: %v", err)
	}
	return bodyResponse
}

func decodeData(t *testing.T, response graphqlResponse, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Data, target); err != nil {
		t.Fatalf("decode data: %v; raw=%s", err, response.Data)
	}
}

func assertErrorCode(t *testing.T, response graphqlResponse, want string) {
	t.Helper()
	if len(response.Errors) == 0 {
		t.Fatalf("response has no errors: %+v", response)
	}
	if got, _ := response.Errors[0].Extensions["code"].(string); got != want {
		t.Fatalf("error code = %q, want %q; errors=%+v", got, want, response.Errors)
	}
}

var _ DeviceRepository = (*fakeRepository)(nil)
