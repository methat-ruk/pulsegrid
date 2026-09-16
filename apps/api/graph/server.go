package graph

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
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
	if repository == nil {
		return nil, errors.New("graphql handler requires a device repository")
	}
	if organizationID == uuid.Nil {
		return nil, errors.New("graphql handler requires an organization")
	}
	if logger == nil {
		logger = slog.Default()
	}

	config := Config{Resolvers: NewResolver(repository, organizationID)}
	config.Complexity.Query.Device = func(childComplexity int, id string) int {
		return 1 + childComplexity
	}
	config.Complexity.Query.Devices = func(childComplexity int, first int, after *string) int {
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
		if !acceptsGraphQLResponse(request.Header.Get("Accept")) {
			writeGraphQLError(response, http.StatusNotAcceptable, "response media type is not supported")
			return
		}
		if !supportsJSONRequest(request) {
			writeGraphQLError(response, http.StatusUnsupportedMediaType, "request content type must be application/json")
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
	for _, item := range strings.Split(raw, ",") {
		mediaType := strings.TrimSpace(strings.SplitN(item, ";", 2)[0])
		if mediaType != "*/*" && mediaType != "application/graphql-response+json" {
			return false
		}
	}
	return true
}

func supportsJSONRequest(request *http.Request) bool {
	if request == nil || request.Method != http.MethodPost {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

func writeGraphQLError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "application/graphql-response+json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]any{
		"errors": []map[string]any{{
			"message":    message,
			"extensions": map[string]any{"code": errorCodeBadUserInput},
		}},
	})
}

// WithRequestContext is used by tests and non-Fiber adapters that need to
// provide the same trusted correlation context as the production bridge.
func WithRequestContext(ctx context.Context, requestID string, principal Principal) context.Context {
	return WithPrincipal(requestcontext.WithRequestID(ctx, requestID), principal)
}
