package graph

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/99designs/gqlgen/graphql/errcode"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/google/uuid"
	"github.com/methat-ruk/pulsegrid/apps/api/graph/model"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/requestcontext"
)

const GraphQLPath = "/graphql"

// NewHandler constructs the constrained development-only GraphQL transport.
// It deliberately registers only POST JSON and does not enable subscriptions,
// GET, multipart uploads, APQ, or playground endpoints.
func NewHandler(repository DeviceRepository, organizationID uuid.UUID, logger *slog.Logger) (http.Handler, error) {
	return NewHandlerWithTelemetry(repository, nil, organizationID, logger)
}

// NewHandlerWithTelemetry constructs the development GraphQL transport with
// the additive telemetry read boundary enabled.
func NewHandlerWithTelemetry(repository DeviceRepository, telemetryRepository TelemetryRepository, organizationID uuid.UUID, logger *slog.Logger) (http.Handler, error) {
	if repository == nil {
		return nil, errors.New("graphql handler requires a device repository")
	}
	if organizationID == uuid.Nil {
		return nil, errors.New("graphql handler requires an organization")
	}
	if logger == nil {
		logger = slog.Default()
	}

	config := Config{Resolvers: NewResolverWithTelemetry(repository, telemetryRepository, organizationID)}
	config.Complexity.Query.Device = func(childComplexity int, id string) int {
		return 1 + childComplexity
	}
	config.Complexity.Query.Devices = func(childComplexity int, first int, after *string) int {
		if first < 1 {
			return childComplexity + 1
		}
		return 1 + (first * childComplexity)
	}
	config.Complexity.Query.DeviceCurrentState = func(childComplexity int, deviceID string) int {
		return 1 + childComplexity
	}
	config.Complexity.Query.DeviceTelemetry = func(childComplexity int, deviceID string, first int, after *string) int {
		if first < 1 {
			return childComplexity + 1
		}
		return 1 + (first * childComplexity)
	}
	config.Complexity.Mutation.CreateDevice = func(childComplexity int, input model.CreateDeviceInput) int {
		return 1 + childComplexity
	}

	srv := handler.New(NewExecutableSchema(config))
	srv.AddTransport(transport.POST{UseGrapQLResponseJsonByDefault: true})
	srv.SetParserTokenLimit(maxParserTokens)
	srv.SetDisableSuggestion(true)
	srv.SetErrorPresenter(presentGraphQLError(logger))
	srv.SetRecoverFunc(recoverGraphQLError(logger))
	srv.Use(extension.Introspection{})
	srv.Use(extension.FixedComplexityLimit(maxComplexity))
	errcode.RegisterErrorType(complexityLimitCode, errcode.KindProtocol)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if localContext, ok := adaptor.LocalContextFromHTTPRequest(request); ok {
			request = request.WithContext(localContext)
		}
		if !acceptsGraphQLResponse(strings.Join(request.Header.Values("Accept"), ",")) {
			writeGraphQLError(response, http.StatusNotAcceptable, "response media type is not supported", requestcontext.RequestID(request.Context()))
			return
		}
		if !supportsJSONRequest(request) {
			writeGraphQLError(response, http.StatusUnsupportedMediaType, "request content type must be application/json", requestcontext.RequestID(request.Context()))
			return
		}
		srv.ServeHTTP(response, request)
	}), nil
}

func acceptsGraphQLResponse(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	var exactQuality float64
	var applicationWildcardQuality float64
	var wildcardQuality float64
	var exactFound bool
	var applicationWildcardFound bool
	var wildcardFound bool
	for item := range strings.SplitSeq(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return false
		}
		mediaType, parameters, err := mime.ParseMediaType(item)
		if err != nil {
			return false
		}
		quality := 1.0
		if rawQuality, ok := parameters["q"]; ok {
			quality, err = strconv.ParseFloat(strings.TrimSpace(rawQuality), 64)
			if err != nil || math.IsNaN(quality) || math.IsInf(quality, 0) || quality < 0 || quality > 1 {
				return false
			}
		}
		switch mediaType {
		case "application/graphql-response+json":
			if !exactFound {
				exactQuality = quality
				exactFound = true
			}
		case "application/*":
			if !applicationWildcardFound {
				applicationWildcardQuality = quality
				applicationWildcardFound = true
			}
		case "*/*":
			if !wildcardFound {
				wildcardQuality = quality
				wildcardFound = true
			}
		}
	}
	if exactFound {
		return exactQuality > 0
	}
	if applicationWildcardFound {
		return applicationWildcardQuality > 0
	}
	return wildcardFound && wildcardQuality > 0
}

func supportsJSONRequest(request *http.Request) bool {
	if request == nil || request.Method != http.MethodPost {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

func writeGraphQLError(response http.ResponseWriter, status int, message string, requestID string) {
	response.Header().Set("Content-Type", "application/graphql-response+json")
	response.WriteHeader(status)
	extensions := map[string]any{"code": errorCodeBadUserInput}
	if requestID != "" {
		extensions["requestId"] = requestID
	}
	_ = json.NewEncoder(response).Encode(map[string]any{
		"errors": []map[string]any{{
			"message":    message,
			"extensions": extensions,
		}},
	})
}

// WithRequestContext is used by tests and non-Fiber adapters that need to
// provide the same trusted correlation context as the production bridge.
func WithRequestContext(ctx context.Context, requestID string, principal Principal) context.Context {
	return WithPrincipal(requestcontext.WithRequestID(ctx, requestID), principal)
}
