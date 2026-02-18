package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// requestIDHeader is the HTTP header name for request ID propagation.
// Uses the canonical X-Request-ID header recognised by proxies, load balancers,
// and observability tools (Envoy, nginx, Datadog, etc.).
const requestIDHeader = "X-Request-ID"

// requestIDKey is the context key for storing the request ID.
// Unexported to prevent external packages from constructing it directly.
type requestIDKey struct{}

// RequestIDFromContext extracts the request ID from the context.
// Returns an empty string if no request ID is present.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// ContextWithRequestID returns a new context with the given request ID stored.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID is middleware that propagates or generates a request ID for every request.
//
// Behavior:
//  1. If the incoming request has an X-Request-ID header, that value is used.
//  2. Otherwise, a new UUID v4 is generated.
//  3. The request ID is stored in the request context (retrieve via RequestIDFromContext).
//  4. The request ID is set on the response as the X-Request-ID header.
//  5. A request-scoped slog logger with the "request_id" attribute is injected into the context.
//
// This middleware should be placed early in the chain — after CORS (which must
// handle preflight before anything else) and security headers, but before auth
// and application-level middleware.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = uuid.New().String()
		}

		// Store in context
		ctx := ContextWithRequestID(r.Context(), id)

		// Add request-scoped slog logger with request_id attribute
		logger := slog.Default().With("request_id", id)
		ctx = contextWithLogger(ctx, logger)

		// Set response header so clients can correlate
		w.Header().Set(requestIDHeader, id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loggerKey is the context key for storing the request-scoped slog logger.
type loggerKey struct{}

// contextWithLogger stores a slog.Logger in the context.
func contextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFromContext retrieves the request-scoped slog.Logger from the context.
// Falls back to slog.Default() if no logger is present.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
