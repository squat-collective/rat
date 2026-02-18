package api

import (
	"log/slog"
	"net/http"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture the status code and
// number of bytes written. This is needed because the standard ResponseWriter
// does not expose these values after the handler returns.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	bytesWritten int
}

// WriteHeader captures the status code before delegating to the underlying writer.
func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the number of bytes written before delegating to the underlying writer.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// Unwrap returns the underlying ResponseWriter, allowing middleware further
// down the chain (e.g. http.Flusher checks) to access the original writer.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// healthPaths contains the paths to skip logging for — they are called
// frequently by orchestrators and produce excessive noise.
var healthPaths = map[string]bool{
	"/health":      true,
	"/health/live": true,
}

// RequestLogger is middleware that logs every HTTP request with structured slog output.
//
// For each request it logs: method, path, status code, duration, request size (Content-Length),
// and response size. The log level depends on the response status code:
//   - 2xx/3xx: slog.Info
//   - 4xx:     slog.Warn
//   - 5xx:     slog.Error
//
// Health check endpoints (/health, /health/live) are skipped to reduce noise.
// If a request ID is present in the context (from the RequestID middleware), it is included.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip noisy health check endpoints.
		if healthPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		// Wrap the response writer to capture status code and response size.
		wrapped := &responseWriter{
			ResponseWriter: w,
			status:         http.StatusOK, // default if handler never calls WriteHeader
		}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		// Build structured log attributes.
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", wrapped.status),
			slog.String("duration", duration.String()),
			slog.Int64("request_size", r.ContentLength),
			slog.Int("response_size", wrapped.bytesWritten),
		}

		// Include request ID if available (set by the RequestID middleware).
		if reqID := RequestIDFromContext(r.Context()); reqID != "" {
			attrs = append(attrs, slog.String("request_id", reqID))
		}

		// Log at appropriate level based on status code.
		msg := "request completed"
		switch {
		case wrapped.status >= 500:
			slog.LogAttrs(r.Context(), slog.LevelError, msg, attrs...)
		case wrapped.status >= 400:
			slog.LogAttrs(r.Context(), slog.LevelWarn, msg, attrs...)
		default:
			slog.LogAttrs(r.Context(), slog.LevelInfo, msg, attrs...)
		}
	})
}
