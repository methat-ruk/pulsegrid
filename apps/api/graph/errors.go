package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/errcode"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/device/registry"
	"github.com/methat-ruk/pulsegrid/apps/api/internal/platform/requestcontext"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

const (
	errorCodeBadUserInput = "BAD_USER_INPUT"
	errorCodeConflict     = "CONFLICT"
	errorCodeInternal     = "INTERNAL_SERVER_ERROR"
	complexityLimitCode   = "COMPLEXITY_LIMIT_EXCEEDED"

	maxCursorSize   = 256
	maxParserTokens = 1000
	// One canonical maximum-page device query is allowed (about 1,001 cost
	// units); repeated maximum-page selections exceed this bounded budget.
	maxComplexity = 1500
)

var (
	errMissingPrincipal = errors.New("graphql principal is unavailable")
)

type publicError struct {
	code    string
	message string
	cause   error
}

func (e *publicError) Error() string {
	if e.cause == nil {
		return e.message
	}
	return fmt.Sprintf("%s: %v", e.message, e.cause)
}

func (e *publicError) Unwrap() error { return e.cause }

func newPublicError(code string, message string, cause error) error {
	return &publicError{code: code, message: message, cause: cause}
}

func presentGraphQLError(logger *slog.Logger) graphql.ErrorPresenterFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, err error) *gqlerror.Error {
		base := graphql.DefaultErrorPresenter(ctx, err)
		code, message := classifyError(err, base)
		presented := &gqlerror.Error{
			Message: message,
			Path:    base.Path,
			Extensions: map[string]any{
				"code": code,
			},
		}
		if requestID := requestcontext.RequestID(ctx); requestID != "" {
			presented.Extensions["requestId"] = requestID
		}
		if shouldLogGraphQLError(code, err) {
			logger.Error("graphql request failed", "reason_code", code, "request_id", requestcontext.RequestID(ctx))
		}
		return presented
	}
}

func recoverGraphQLError(logger *slog.Logger) graphql.RecoverFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, recovered any) error {
		logger.Error("graphql resolver panic recovered", "reason_code", "panic_recovered", "request_id", requestcontext.RequestID(ctx))
		return newPublicError(errorCodeInternal, "internal server error", fmt.Errorf("resolver panic: %v", recovered))
	}
}

func classifyError(err error, base *gqlerror.Error) (string, string) {
	if explicit, ok := errors.AsType[*publicError](err); ok {
		return explicit.code, explicit.message
	}

	baseCode := ""
	if base != nil && base.Extensions != nil {
		baseCode, _ = base.Extensions["code"].(string)
	}
	switch baseCode {
	case errcode.ParseFailed:
		return baseCode, "query could not be parsed"
	case errcode.ValidationFailed:
		return baseCode, "query failed validation"
	case complexityLimitCode:
		return errorCodeBadUserInput, "operation exceeds the complexity limit"
	}

	if err != nil && strings.Contains(err.Error(), "json request body could not be decoded") {
		return errorCodeBadUserInput, "request body must be valid JSON"
	}
	if errors.Is(err, registry.ErrInvalidInput) {
		return errorCodeBadUserInput, "request input is invalid"
	}
	if errors.Is(err, registry.ErrConflict) {
		return errorCodeConflict, "device already exists"
	}
	return errorCodeInternal, "internal server error"
}

func shouldLogGraphQLError(code string, err error) bool {
	if code == errcode.ParseFailed || code == errcode.ValidationFailed {
		return false
	}
	return err != nil
}

func protocolError(message string) error {
	return newPublicError(errorCodeBadUserInput, message, registry.ErrInvalidInput)
}
