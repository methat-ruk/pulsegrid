package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/config"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/requestcontext"
)

func TestHealthEndpointsExposeMinimalStates(t *testing.T) {
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	response := performRequest(t, server, http.MethodGet, ReadyPath)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("not-ready status = %d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}
	assertJSONContentType(t, response)
	if body := readBody(t, response); body != `{"status":"not_ready","reason":"starting"}` {
		t.Fatalf("not-ready body = %q", body)
	}

	response = performRequest(t, server, http.MethodGet, LivePath)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("live status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	assertJSONContentType(t, response)
	if body := readBody(t, response); body != `{"status":"ok"}` {
		t.Fatalf("live body = %q", body)
	}

	server.state.Store(lifecycleReady)
	response = performRequest(t, server, http.MethodGet, ReadyPath)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ready status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	assertJSONContentType(t, response)
	if body := readBody(t, response); body != `{"status":"ready"}` {
		t.Fatalf("ready body = %q", body)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("health response unexpectedly enabled CORS")
	}

	server.state.Store(lifecycleDraining)
	response = performRequest(t, server, http.MethodGet, ReadyPath)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("draining status = %d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}
	assertJSONContentType(t, response)
	if body := readBody(t, response); body != `{"status":"not_ready","reason":"draining"}` {
		t.Fatalf("draining body = %q", body)
	}
}

func TestReadinessReportsDependencyUnavailableWithoutChangingLiveness(t *testing.T) {
	dependencyAvailable := false
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		ReadinessCheck: func(context.Context) error {
			if !dependencyAvailable {
				return errors.New("dependency is down")
			}
			return nil
		},
	})
	server.state.Store(lifecycleReady)

	response := performRequest(t, server, http.MethodGet, ReadyPath)
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("dependency-down readiness status = %d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}
	if body := readBody(t, response); body != `{"status":"not_ready","reason":"dependency_unavailable"}` {
		t.Fatalf("dependency-down readiness body = %q", body)
	}

	response = performRequest(t, server, http.MethodGet, LivePath)
	if response.StatusCode != http.StatusOK || readBody(t, response) != `{"status":"ok"}` {
		t.Fatalf("liveness changed while dependency was down: status=%d", response.StatusCode)
	}

	dependencyAvailable = true
	response = performRequest(t, server, http.MethodGet, ReadyPath)
	if response.StatusCode != http.StatusOK || readBody(t, response) != `{"status":"ready"}` {
		t.Fatalf("dependency-up readiness = %d", response.StatusCode)
	}
}

func TestGraphQLCompositionBridgesRequestContext(t *testing.T) {
	type contextProbeKey struct{}
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		GraphQLHandler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if localContext, ok := adaptor.LocalContextFromHTTPRequest(request); ok {
				request = request.WithContext(localContext)
			}
			tenant, _ := request.Context().Value(contextProbeKey{}).(string)
			_, hasDeadline := request.Context().Deadline()
			response.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(response).Encode(map[string]string{
				"requestId": requestcontext.RequestID(request.Context()),
				"tenant":    tenant,
				"deadline":  fmt.Sprint(hasDeadline),
			})
		}),
		ContextEnricher: func(ctx context.Context) context.Context {
			return context.WithValue(ctx, contextProbeKey{}, "fixed-tenant")
		},
	})
	request := httptest.NewRequest(http.MethodPost, "http://example.test"+GraphQLPath, strings.NewReader(`{}`))
	request.Header.Set(requestIDHeader, "safe-request-001")
	response, err := server.app.Test(request)
	if err != nil {
		t.Fatalf("app.Test returned error: %v", err)
	}
	defer response.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode context bridge body: %v", err)
	}
	if body["requestId"] != "safe-request-001" || body["tenant"] != "fixed-tenant" || body["deadline"] != "true" {
		t.Fatalf("context bridge body = %+v", body)
	}
}

func TestGraphQLRouteEnforcesBoundedRequestBody(t *testing.T) {
	handlerCalled := false
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		GraphQLHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			handlerCalled = true
		}),
	})
	request := httptest.NewRequest(http.MethodPost, "http://example.test"+GraphQLPath, strings.NewReader(strings.Repeat("x", maxBodySize+1)))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.app.Test(request)
	if err != nil {
		if !strings.Contains(err.Error(), "body size exceeds") {
			t.Fatalf("oversized request returned unexpected error: %v", err)
		}
		if handlerCalled {
			t.Fatal("oversized request reached GraphQL handler")
		}
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized GraphQL status = %d, want %d", response.StatusCode, http.StatusRequestEntityTooLarge)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read oversized response: %v", err)
	}
	if handlerCalled || strings.Contains(string(body), strings.Repeat("x", 32)) {
		t.Fatalf("oversized request reached handler or leaked body: %q", body)
	}
}

func TestUnsupportedMethodUsesStableErrorContract(t *testing.T) {
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	response := performRequest(t, server, http.MethodPost, LivePath)
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
	assertJSONContentType(t, response)
	if body := readBody(t, response); body != `{"error":{"code":"method_not_allowed","message":"method not allowed"}}` {
		t.Fatalf("method error body = %q", body)
	}
}

func TestDefaultCompositionDoesNotMountGraphQL(t *testing.T) {
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := performRequest(t, server, http.MethodPost, GraphQLPath)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled GraphQL status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
	if body := readBody(t, response); body != `{"error":{"code":"not_found","message":"route not found"}}` {
		t.Fatalf("disabled GraphQL body = %q", body)
	}
}

func TestEnabledGraphQLRejectsNonPostMethods(t *testing.T) {
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		GraphQLHandler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	response := performRequest(t, server, http.MethodGet, GraphQLPath)
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GraphQL GET status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
	if body := readBody(t, response); body != `{"error":{"code":"method_not_allowed","message":"method not allowed"}}` {
		t.Fatalf("GraphQL GET body = %q", body)
	}
}

func TestErrorResponsesAreStableAndDoNotLeakInternalErrors(t *testing.T) {
	var logs bytes.Buffer
	server := New(testConfig(), slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	server.app.Get("/test/error", func(fiber.Ctx) error {
		return fiber.NewError(http.StatusInternalServerError, "secret provider detail")
	})

	response := performRequest(t, server, http.MethodGet, "/missing")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("not-found status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
	if body := readBody(t, response); body != `{"error":{"code":"not_found","message":"route not found"}}` {
		t.Fatalf("not-found body = %q", body)
	}

	response = performRequest(t, server, http.MethodGet, "/test/error")
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("internal status = %d, want %d", response.StatusCode, http.StatusInternalServerError)
	}
	body := readBody(t, response)
	if strings.Contains(body, "secret provider detail") || strings.Contains(logs.String(), "secret provider detail") {
		t.Fatal("internal error detail leaked to response or log")
	}
	requestID := response.Header.Get(requestIDHeader)
	if requestID == "" {
		t.Fatal("response did not include a generated request ID")
	}
	if !strings.Contains(logs.String(), "request_id="+requestID) {
		t.Fatalf("error log did not include response request ID %q; logs: %s", requestID, logs.String())
	}
}

func TestRequestIDAcceptsBoundedSafeValueOnly(t *testing.T) {
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptestRequest(http.MethodGet, LivePath)
	request.Header.Set(requestIDHeader, strings.Repeat("x", 65))
	response, err := server.app.Test(request)
	if err != nil {
		t.Fatalf("app.Test returned error: %v", err)
	}
	defer response.Body.Close()

	requestID := response.Header.Get(requestIDHeader)
	if requestID == "" || len(requestID) != 32 {
		t.Fatalf("generated request ID = %q, want 32 hex characters", requestID)
	}
}

func TestListenBecomesReadyAndStopsOnContextCancellation(t *testing.T) {
	var logs bytes.Buffer
	server := New(testConfig(), slog.New(slog.NewTextHandler(&logs, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- server.Listen(ctx, 2*time.Second)
	}()

	select {
	case <-server.ReadySignal():
	case <-time.After(2 * time.Second):
		t.Fatal("server did not become ready")
	}
	if !server.Ready() {
		t.Fatal("server readiness state is false after listener started")
	}

	cancel()
	select {
	case err := <-listenErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Listen returned error after cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop within the shutdown bound")
	}
	select {
	case <-server.StoppedSignal():
	case <-time.After(1 * time.Second):
		t.Fatal("server did not complete post-shutdown lifecycle")
	}
	if server.Ready() {
		t.Fatal("server remained ready after shutdown")
	}
	if stoppedCount := strings.Count(logs.String(), "msg=\"http server stopped\""); stoppedCount != 1 {
		t.Fatalf("stopped log count = %d, want 1; logs: %s", stoppedCount, logs.String())
	}
}

func TestListenHandlesContextCancellationDuringStartup(t *testing.T) {
	server := New(testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- server.Listen(ctx, 2*time.Second)
	}()

	select {
	case err := <-listenErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Listen returned error after startup cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop after startup cancellation")
	}
	select {
	case <-server.StoppedSignal():
	case <-time.After(time.Second):
		t.Fatal("server did not complete post-shutdown lifecycle")
	}
	if server.Ready() {
		t.Fatal("server became ready after startup cancellation")
	}
}

func testConfig() config.Config {
	return config.Config{
		Environment:     config.Test,
		HTTPHost:        "127.0.0.1",
		HTTPPort:        0,
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 2 * time.Second,
	}
}

func performRequest(t *testing.T, server *Server, method string, path string) *http.Response {
	t.Helper()
	response, err := server.app.Test(httptestRequest(method, path))
	if err != nil {
		t.Fatalf("app.Test returned error: %v", err)
	}
	return response
}

func httptestRequest(method string, path string) *http.Request {
	return httptest.NewRequest(method, "http://example.test"+path, nil)
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(body)
}

func assertJSONContentType(t *testing.T, response *http.Response) {
	t.Helper()
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
}
