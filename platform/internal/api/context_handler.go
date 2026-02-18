package api

import (
	"context"
	"log/slog"
)

// ContextHandler is an slog.Handler that automatically enriches log records
// with values from the context. When a request_id is present in the context
// (set by the RequestID middleware), it is automatically added to every log
// record without the caller needing to pass it explicitly.
//
// P10-40: This handler wraps any slog.Handler and extracts context values
// that were set by the RequestID middleware. It enables handlers and services
// to use slog.InfoContext/ErrorContext and automatically get request_id in
// every log record.
//
// Usage in main.go:
//
//	base := slog.NewJSONHandler(os.Stdout, nil)
//	handler := api.NewContextHandler(base)
//	slog.SetDefault(slog.New(handler))
type ContextHandler struct {
	inner slog.Handler
}

// NewContextHandler creates a new ContextHandler wrapping the given handler.
func NewContextHandler(inner slog.Handler) *ContextHandler {
	return &ContextHandler{inner: inner}
}

// Enabled delegates to the inner handler.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle enriches the record with context values before delegating.
func (h *ContextHandler) Handle(ctx context.Context, record slog.Record) error {
	// Extract request_id from context if available.
	if reqID := RequestIDFromContext(ctx); reqID != "" {
		record.AddAttrs(slog.String("request_id", reqID))
	}
	return h.inner.Handle(ctx, record)
}

// WithAttrs returns a new ContextHandler wrapping the inner handler with additional attributes.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new ContextHandler wrapping the inner handler with a group prefix.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{inner: h.inner.WithGroup(name)}
}
