package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/stretchr/testify/assert"
)

// --- validName tests via middleware ---
// validName is unexported, so we test it indirectly through the ValidatePathParams middleware
// by making requests with path parameters.

func TestValidatePathParams_ValidLowercaseSlug_Passes(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	// A valid lowercase namespace in a path
	req := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestValidatePathParams_UppercaseNamespace_Returns400(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/Default/bronze/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestValidatePathParams_UppercaseName_Returns400(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/MyPipeline", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestValidatePathParams_NameWithSpaces_Returns400(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/my%20pipeline", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestValidatePathParams_NameStartsWithDigit_Returns400(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/bronze/1orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestValidatePathParams_InvalidLayer_Returns400(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/default/platinum/orders", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "layer must be bronze, silver, or gold")
}

func TestValidatePathParams_ValidSlugWithHyphensUnderscores_Passes(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	// Pipeline with hyphens and underscores — valid slug, should pass middleware
	// (will get 404 since pipeline doesn't exist, but NOT 400)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines/my-ns/bronze/my_pipeline-v2", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code) // passes middleware, 404 from handler
}

func TestValidatePathParams_UUIDParam_Skipped(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	// runID is a UUID param — should be skipped by middleware (not validated as a name)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/550e8400-e29b-41d4-a716-446655440000", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Should not be 400 — either 200 (found) or 404 (not found), but not a validation error
	assert.NotEqual(t, http.StatusBadRequest, rec.Code)
}

func TestValidatePathParams_QualityTestName_Validated(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	// testName with uppercase should fail
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pipelines/default/silver/orders/tests/BadName", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "lowercase slug")
}

func TestValidatePathParams_LandingZoneNsParam_Validated(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	// "namespace" param with uppercase should fail
	req := httptest.NewRequest(http.MethodGet, "/api/v1/landing-zones/BadNs/uploads", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- CORS P10-20 ---

func TestCORS_WildcardOrigin_ReflectsRequestOrigin(t *testing.T) {
	srv := fullTestServer()
	srv.CORSOrigins = []string{"*"}
	router := api.NewRouter(srv)

	// Send preflight request with a specific Origin.
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/features", http.NoBody)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The response should reflect the request origin, NOT "*".
	origin := rec.Header().Get("Access-Control-Allow-Origin")
	assert.Equal(t, "https://app.example.com", origin, "should reflect request origin, not wildcard")
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_ExplicitOrigins_DoesNotReflectUnknown(t *testing.T) {
	srv := fullTestServer()
	srv.CORSOrigins = []string{"https://allowed.example.com"}
	router := api.NewRouter(srv)

	// Send preflight from a disallowed origin.
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/features", http.NoBody)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Should NOT echo the disallowed origin.
	origin := rec.Header().Get("Access-Control-Allow-Origin")
	assert.NotEqual(t, "https://evil.example.com", origin)
}

func TestCORS_ExplicitOrigins_AllowsConfiguredOrigin(t *testing.T) {
	srv := fullTestServer()
	srv.CORSOrigins = []string{"https://allowed.example.com"}
	router := api.NewRouter(srv)

	// Send preflight from the configured origin.
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/features", http.NoBody)
	req.Header.Set("Origin", "https://allowed.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, "https://allowed.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

// --- Webhook Rate Limiting P10-24 ---

func TestWebhookRateLimit_ExceedsBurst_Returns429(t *testing.T) {
	srv := fullTestServer()
	// Configure a very tight webhook rate limit for testing.
	srv.WebhookRateLimit = &api.WebhookRateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             2,
		CleanupInterval:   60_000_000_000,
	}
	router := api.NewRouter(srv)
	defer func() {
		if srv.WebhookRateLimiterStop != nil {
			srv.WebhookRateLimiterStop()
		}
	}()

	// First 2 requests should succeed (burst) — they'll get 400 (missing token) but not 429.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", http.NoBody)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.NotEqual(t, http.StatusTooManyRequests, rec.Code, "request %d should not be rate limited", i+1)
	}

	// 3rd request should be rate limited.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", http.NoBody)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestWebhookRateLimit_DifferentIPs_Independent(t *testing.T) {
	srv := fullTestServer()
	srv.WebhookRateLimit = &api.WebhookRateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
		CleanupInterval:   60_000_000_000,
	}
	router := api.NewRouter(srv)
	defer func() {
		if srv.WebhookRateLimiterStop != nil {
			srv.WebhookRateLimiterStop()
		}
	}()

	// Exhaust IP A's burst.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", http.NoBody)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.NotEqual(t, http.StatusTooManyRequests, rec.Code)

	// IP A should now be limited.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", http.NoBody)
	req.RemoteAddr = "10.0.0.1:1234"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// IP B should still be allowed.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", http.NoBody)
	req.RemoteAddr = "10.0.0.2:1234"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.NotEqual(t, http.StatusTooManyRequests, rec.Code)
}

func TestWebhookRateLimit_DoesNotAffectAPIRoutes(t *testing.T) {
	srv := fullTestServer()
	srv.WebhookRateLimit = &api.WebhookRateLimitConfig{
		RequestsPerSecond: 1,
		Burst:             1,
		CleanupInterval:   60_000_000_000,
	}
	router := api.NewRouter(srv)
	defer func() {
		if srv.WebhookRateLimiterStop != nil {
			srv.WebhookRateLimiterStop()
		}
	}()

	// Exhaust webhook rate limit.
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks", http.NoBody)
		req.RemoteAddr = "10.0.0.1:1234"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}

	// API routes should NOT be affected by the webhook rate limiter.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", http.NoBody)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.NotEqual(t, http.StatusTooManyRequests, rec.Code)
}
