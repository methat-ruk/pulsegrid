// Package requestcontext contains the small, trusted context bridge shared by
// the HTTP transport and application handlers.
package requestcontext

import "context"

type requestIDKey struct{}

// WithRequestID attaches the server-generated correlation identifier to a
// request context. Callers should only pass values produced by the HTTP
// request-ID middleware.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestID returns the trusted request correlation identifier, if present.
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}
