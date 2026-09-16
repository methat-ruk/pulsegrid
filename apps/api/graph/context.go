package graph

import (
	"context"

	"github.com/google/uuid"
)

type principalContextKey struct{}

// WithPrincipal attaches a server-selected principal to the request context.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext returns the trusted principal, if one was installed by
// the HTTP composition layer.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func principalFromContext(ctx context.Context) (Principal, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok || principal.OrganizationID == uuid.Nil {
		return Principal{}, newPublicError(errorCodeInternal, "request authority is unavailable", errMissingPrincipal)
	}
	return principal, nil
}
