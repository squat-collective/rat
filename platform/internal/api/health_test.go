package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

	// Plugins map is empty in community edition (runner plugins exposed via /runner/plugins)
	plugins, ok := body["plugins"].(map[string]interface{})
	require.True(t, ok, "plugins should be a map")
	assert.Empty(t, plugins)
}

func TestHandleFeatures_WithPluginRegistry_ReturnsDynamic(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		Plugins: &mockPluginRegistry{
			features: domain.Features{
				Edition:    "pro",
				Namespaces: true,
				MultiUser:  true,
				Plugins:    map[string]domain.PluginFeature{},
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

	// Pro edition with multi-user enabled
	assert.Equal(t, "pro", body["edition"])
	assert.Equal(t, true, body["namespaces"])
	assert.Equal(t, true, body["multi_user"])

	// Plugins map is empty — runner plugins exposed via dedicated endpoint
	plugins, ok := body["plugins"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, plugins)
}

// --- /metrics ---

// parsePromMetrics returns a map of metric-name → numeric value parsed from
// a Prometheus text-format response body. Lines starting with "#" are
// metadata (HELP/TYPE) and skipped. Each value must parse as float64 — any
// non-numeric line fails the test so we catch malformed output early.
func parsePromMetrics(t *testing.T, body io.Reader) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	sc := bufio.NewScanner(body)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// "<name>[{labels}] <value>" — split on the LAST space to
		// preserve any label set in the name token.
		idx := strings.LastIndex(line, " ")
		require.Greater(t, idx, 0, "malformed metric line: %q", line)
		name := line[:idx]
		valStr := line[idx+1:]
		v, err := strconv.ParseFloat(valStr, 64)
		require.NoError(t, err, "non-numeric metric value: %q", line)
		out[name] = v
	}
	require.NoError(t, sc.Err())
	return out
}

func TestHandleMetrics_BareServer_EmitsRuntimeMetrics(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")

	metrics := parsePromMetrics(t, rec.Body)

	// Runtime gauges are always present, regardless of which dependencies
	// are wired. Goroutine count must be > 0 (the test itself is one).
	require.Contains(t, metrics, "ratd_goroutines")
	assert.Greater(t, metrics["ratd_goroutines"], 0.0)

	require.Contains(t, metrics, "ratd_memory_alloc_bytes")
	assert.GreaterOrEqual(t, metrics["ratd_memory_alloc_bytes"], 0.0)

	// The pool/plugin/scheduler metrics are *omitted* (not zeroed) when the
	// closures are nil — that's deliberate so a dev server with no DB
	// doesn't emit fake-saturated pool numbers.
	assert.NotContains(t, metrics, "ratd_postgres_pool_total")
	assert.NotContains(t, metrics, "ratd_plugins_total")
	assert.NotContains(t, metrics, "ratd_scheduler_last_tick_duration_seconds")
}

func TestHandleMetrics_AllClosuresWired_EmitsEveryMetric(t *testing.T) {
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		// Realistic-looking values so each gauge is observable.
		DBPoolStats:        func() (int32, int32) { return 10, 3 },
		HeartbeatPoolStats: func() (int32, int32) { return 1, 0 },
		PluginHealthStats:  func() (int, int) { return 5, 4 },
		SchedulerMetrics:   func() (float64, int) { return 0.042, 7 },
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	metrics := parsePromMetrics(t, rec.Body)

	// Pool — main.
	assert.InDelta(t, 10.0, metrics["ratd_postgres_pool_total"], 0.0001)
	assert.InDelta(t, 3.0, metrics["ratd_postgres_pool_acquired"], 0.0001)
	// Pool — heartbeat.
	assert.InDelta(t, 1.0, metrics["ratd_postgres_heartbeat_pool_total"], 0.0001)
	assert.InDelta(t, 0.0, metrics["ratd_postgres_heartbeat_pool_acquired"], 0.0001)
	// Plugin fleet.
	assert.InDelta(t, 5.0, metrics["ratd_plugins_total"], 0.0001)
	assert.InDelta(t, 4.0, metrics["ratd_plugins_healthy"], 0.0001)
	// Scheduler last-tick.
	assert.InDelta(t, 0.042, metrics["ratd_scheduler_last_tick_duration_seconds"], 0.0001)
	assert.InDelta(t, 7.0, metrics["ratd_scheduler_last_tick_dispatched_total"], 0.0001)
}

func TestHandleMetrics_OmitsHeartbeatPoolWhenNotWired(t *testing.T) {
	// Mirrors production behaviour when RAT_HEARTBEAT_POOL_ENABLED=false:
	// main pool is exposed, dedicated heartbeat pool is not.
	srv := &api.Server{
		LandingZones: newMemoryLandingZoneStore(),
		DBPoolStats:  func() (int32, int32) { return 8, 1 },
		// HeartbeatPoolStats intentionally nil.
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	metrics := parsePromMetrics(t, rec.Body)

	assert.Contains(t, metrics, "ratd_postgres_pool_total")
	assert.NotContains(t, metrics, "ratd_postgres_heartbeat_pool_total")
	assert.NotContains(t, metrics, "ratd_postgres_heartbeat_pool_acquired")
}

func TestHandleMetrics_ZeroValuesAreEmittedNotSuppressed(t *testing.T) {
	// A freshly-booted server has zero dispatches and a tiny tick duration;
	// the metric line must still appear so Prometheus has a baseline series.
	srv := &api.Server{
		LandingZones:      newMemoryLandingZoneStore(),
		PluginHealthStats: func() (int, int) { return 0, 0 },
		SchedulerMetrics:  func() (float64, int) { return 0, 0 },
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	metrics := parsePromMetrics(t, rec.Body)

	require.Contains(t, metrics, "ratd_plugins_total")
	assert.Equal(t, 0.0, metrics["ratd_plugins_total"])
	require.Contains(t, metrics, "ratd_plugins_healthy")
	assert.Equal(t, 0.0, metrics["ratd_plugins_healthy"])
	require.Contains(t, metrics, "ratd_scheduler_last_tick_dispatched_total")
	assert.Equal(t, 0.0, metrics["ratd_scheduler_last_tick_dispatched_total"])
}
