package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestID_GeneratesUUIDWhenNotPresent(t *testing.T) {
	var capturedID string
	handler := api.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = api.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Must be a valid UUID
	assert.NotEmpty(t, capturedID)
	_, err := uuid.Parse(capturedID)
	require.NoError(t, err, "generated request ID should be a valid UUID")

	// Response header should match
	assert.Equal(t, capturedID, rec.Header().Get("X-Request-ID"))
}

func TestRequestID_PreservesProvidedHeader(t *testing.T) {
	clientID := "my-custom-request-id-12345"
	var capturedID string

	handler := api.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = api.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("X-Request-ID", clientID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, clientID, capturedID)
	assert.Equal(t, clientID, rec.Header().Get("X-Request-ID"))
}

func TestRequestID_ResponseHeaderAlwaysSet(t *testing.T) {
	handler := api.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// X-Request-ID must always be in the response
	responseID := rec.Header().Get("X-Request-ID")
	assert.NotEmpty(t, responseID, "X-Request-ID response header must always be set")
}

func TestRequestID_EachRequestGetsUniqueID(t *testing.T) {
	var ids []string
	handler := api.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, api.RequestIDFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// All IDs should be unique
	seen := make(map[string]bool)
	for _, id := range ids {
		assert.False(t, seen[id], "request ID %s was duplicated", id)
		seen[id] = true
	}
}

func TestRequestIDFromContext_ReturnsEmptyForBareContext(t *testing.T) {
	id := api.RequestIDFromContext(context.Background())
	assert.Empty(t, id, "bare context should return empty request ID")
}

func TestContextWithRequestID_RoundTrips(t *testing.T) {
	ctx := api.ContextWithRequestID(context.Background(), "test-id-42")
	assert.Equal(t, "test-id-42", api.RequestIDFromContext(ctx))
}

func TestRequestID_LoggerInContext(t *testing.T) {
	handler := api.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := api.LoggerFromContext(r.Context())
		assert.NotNil(t, logger, "logger should be present in context")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLoggerFromContext_FallsBackToDefault(t *testing.T) {
	// Without middleware, should fall back to slog.Default()
	logger := api.LoggerFromContext(context.Background())
	assert.NotNil(t, logger, "should fall back to slog.Default()")
}
