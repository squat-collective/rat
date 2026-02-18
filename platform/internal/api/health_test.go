package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPluginRegistry implements api.PluginRegistry for testing.
type mockPluginRegistry struct {
	features domain.Features
}

func (m *mockPluginRegistry) Features() domain.Features {
	return m.features
}

// mockHealthChecker implements api.HealthChecker for testing.
type mockHealthChecker struct {
	err error
}

func (m *mockHealthChecker) HealthCheck(_ context.Context) error {
	return m.err
}

// --- /health (backward compat) ---

func TestHandleHealth_ReturnsOK(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ok", body["status"])
}

func TestHandleHealth_ReturnsJSON(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

// --- /health/live ---

func TestHandleHealthLive_AlwaysReturns200(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		// Even with unhealthy dependencies, liveness always returns 200.
		DBHealth: &mockHealthChecker{err: errors.New("connection refused")},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health/live", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ok", body["status"])
}

// --- /health/ready ---

func TestHandleHealthReady_AllHealthy_Returns200(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		DBHealth:     &mockHealthChecker{err: nil},
		S3Health:     &mockHealthChecker{err: nil},
		RunnerHealth: &mockHealthChecker{err: nil},
		QueryHealth:  &mockHealthChecker{err: nil},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body api.ReadinessResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ready", body.Status)
	assert.Equal(t, "ok", body.Checks["postgres"].Status)
	assert.Equal(t, "ok", body.Checks["s3"].Status)
	assert.Equal(t, "ok", body.Checks["runner"].Status)
	assert.Equal(t, "ok", body.Checks["query"].Status)
	assert.Len(t, body.Checks, 4)
}

func TestHandleHealthReady_PostgresDown_Returns503(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		DBHealth:     &mockHealthChecker{err: errors.New("connection refused")},
		S3Health:     &mockHealthChecker{err: nil},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body api.ReadinessResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "not_ready", body.Status)
	assert.Equal(t, "error", body.Checks["postgres"].Status)
	assert.Equal(t, "connection refused", body.Checks["postgres"].Error)
	assert.Equal(t, "ok", body.Checks["s3"].Status)
}

func TestHandleHealthReady_S3Down_Returns503(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		DBHealth:     &mockHealthChecker{err: nil},
		S3Health:     &mockHealthChecker{err: errors.New("bucket not found")},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body api.ReadinessResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "not_ready", body.Status)
	assert.Equal(t, "error", body.Checks["s3"].Status)
	assert.Equal(t, "bucket not found", body.Checks["s3"].Error)
}

func TestHandleHealthReady_MultipleDepsDown_Returns503WithAllErrors(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		DBHealth:     &mockHealthChecker{err: errors.New("pg: connection refused")},
		S3Health:     &mockHealthChecker{err: errors.New("s3: timeout")},
		RunnerHealth: &mockHealthChecker{err: nil},
		QueryHealth:  &mockHealthChecker{err: errors.New("ratq: unavailable")},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body api.ReadinessResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "not_ready", body.Status)
	assert.Equal(t, "error", body.Checks["postgres"].Status)
	assert.Equal(t, "error", body.Checks["s3"].Status)
	assert.Equal(t, "ok", body.Checks["runner"].Status)
	assert.Equal(t, "error", body.Checks["query"].Status)
}

func TestHandleHealthReady_NoDepsConfigured_ReturnsReady(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body api.ReadinessResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ready", body.Status)
	assert.Empty(t, body.Checks)
}

func TestHandleHealthReady_OnlyPostgres_ReturnsReady(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		DBHealth:     &mockHealthChecker{err: nil},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body api.ReadinessResponse
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ready", body.Status)
	assert.Len(t, body.Checks, 1)
	assert.Equal(t, "ok", body.Checks["postgres"].Status)
}

func TestHandleHealthReady_ReturnsJSON(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		DBHealth:     &mockHealthChecker{err: nil},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

// --- /features ---

func TestHandleFeatures_ReturnsCommunityDefaults(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/features", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)

	// Community edition defaults
	assert.Equal(t, "community", body["edition"])
	assert.Equal(t, false, body["namespaces"])
	assert.Equal(t, false, body["multi_user"])

	// Plugins map exists
	plugins, ok := body["plugins"].(map[string]interface{})
	require.True(t, ok, "plugins should be a map")

	// Auth plugin disabled in community
	auth, ok := plugins["auth"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, auth["enabled"])

	// Executor plugin enabled (warmpool)
	executor, ok := plugins["executor"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, executor["enabled"])
	assert.Equal(t, "warmpool", executor["type"])
}

func TestHandleFeatures_WithPluginRegistry_ReturnsDynamic(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		Plugins: &mockPluginRegistry{
			features: domain.Features{
				Edition:    "pro",
				Namespaces: true,
				MultiUser:  true,
				Plugins: map[string]domain.PluginFeature{
					"auth":        {Enabled: true},
					"sharing":     {Enabled: false},
					"executor":    {Enabled: true, Type: "warmpool"},
					"audit":       {Enabled: false},
					"enforcement": {Enabled: false},
				},
			},
		},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/features", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)

	// Pro edition with auth enabled
	assert.Equal(t, "pro", body["edition"])
	assert.Equal(t, true, body["namespaces"])
	assert.Equal(t, true, body["multi_user"])

	plugins, ok := body["plugins"].(map[string]interface{})
	require.True(t, ok)

	auth, ok := plugins["auth"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, auth["enabled"])
}
