package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/auth"
	"github.com/stretchr/testify/assert"
)

func TestNoop_PassesRequestThrough(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mw := auth.Noop()
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestNoop_PreservesHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify incoming headers are preserved
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	})

	mw := auth.Noop()
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNoop_PreservesContext(t *testing.T) {
	type ctxKey string
	key := ctxKey("test-key")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := r.Context().Value(key)
		assert.Equal(t, "test-value", val)
		w.WriteHeader(http.StatusOK)
	})

	mw := auth.Noop()
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	ctx := req.Context()
	req = req.WithContext(context.WithValue(ctx, key, "test-value"))
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- APIKey middleware tests ---

func TestAPIKey_BlocksRequestWithoutAuthHeader(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	mw := auth.APIKey("my-secret-key")
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing or invalid Authorization header")
}

func TestAPIKey_AllowsRequestWithCorrectKey(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mw := auth.APIKey("my-secret-key")
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Bearer my-secret-key")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestAPIKey_RejectsWrongKey(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	mw := auth.APIKey("my-secret-key")
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid API key")
}

func TestAPIKey_EmptyKeyActsAsNoop(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mw := auth.APIKey("")
	wrapped := mw(handler)

	// No auth header — should still pass through.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestAPIKey_HealthEndpointExemptFromAuth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("healthy"))
	})

	mw := auth.APIKey("my-secret-key")
	wrapped := mw(handler)

	// GET /health without any auth header should pass through.
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "healthy", rec.Body.String())
}

func TestAPIKey_RejectsNonBearerAuthScheme(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	mw := auth.APIKey("my-secret-key")
	wrapped := mw(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing or invalid Authorization header")
}

func TestAPIKey_PostToHealthRequiresAuth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for POST /health without auth")
	})

	mw := auth.APIKey("my-secret-key")
	wrapped := mw(handler)

	// POST /health should NOT be exempt — only GET /health is.
	req := httptest.NewRequest(http.MethodPost, "/health", http.NoBody)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
