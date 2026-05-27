package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Regression tests for the centralised internal-route builder. ─────────
//
// These tests pin three things that the refactor MUST preserve:
//
//   1. Every endpoint that used to live on the internal listener is still
//      reachable when MountAllInternalRoutes is called with all flags true.
//   2. InternalRouterConfig{} (the zero value) enables EVERY group — the
//      safe production default. A missing flag must never silently mute
//      an endpoint.
//   3. The public listener still 404s for every internal path, including
//      the new /api/v1/internal/plugins/register alias.
//
// All assertions are run against the shared *Server / NewRouter / a fresh
// chi.Router so the test reflects the production wiring as closely as
// possible without booting a full ratd process.

// fullInternalTestServer builds a Server with the minimum stores that the
// internal endpoints touch (Runs for the run-status callback, FailedMerges
// for the audit callback, PluginManager for phone-home). Each store is the
// existing per-feature mock so tests stay independent of Postgres.
func fullInternalTestServer() *api.Server {
	store := newMemoryRunStore()
	run := &domain.Run{Status: domain.RunStatusRunning}
	_ = store.CreateRun(nil, run)
	return &api.Server{
		Runs: store,
		Executor: &mockCallbackExecutor{
			handleFunc: func(_ api.RunStatusUpdate) error { return nil },
		},
		FailedMerges:  &mockFailedMergesStore{},
		PluginManager: &mockPluginManager{},
	}
}

// fullInternalTestServerWithRun returns (srv, runID) so callers that need
// the runID for the URL can avoid round-tripping through the store.
func fullInternalTestServerWithRun() (*api.Server, string) {
	store := newMemoryRunStore()
	run := &domain.Run{Status: domain.RunStatusRunning}
	_ = store.CreateRun(nil, run)
	srv := &api.Server{
		Runs: store,
		Executor: &mockCallbackExecutor{
			handleFunc: func(_ api.RunStatusUpdate) error { return nil },
		},
		FailedMerges:  &mockFailedMergesStore{},
		PluginManager: &mockPluginManager{},
	}
	return srv, run.ID.String()
}

// internalRoute describes one expected entry on the internal listener:
// the chi pattern, a representative HTTP method, and a body builder so we
// can hit POST routes with a minimally-valid payload.
type internalRoute struct {
	name      string
	method    string
	pathFor   func(runID string) string
	body      func() []byte
	expectMin int // accepted minimum status (some endpoints return 200, some 204)
}

func internalRoutesTable() []internalRoute {
	return []internalRoute{
		{
			name:      "health",
			method:    http.MethodGet,
			pathFor:   func(_ string) string { return "/health" },
			body:      func() []byte { return nil },
			expectMin: http.StatusOK,
		},
		{
			name:      "health-live",
			method:    http.MethodGet,
			pathFor:   func(_ string) string { return "/health/live" },
			body:      func() []byte { return nil },
			expectMin: http.StatusOK,
		},
		{
			name:      "health-ready",
			method:    http.MethodGet,
			pathFor:   func(_ string) string { return "/health/ready" },
			body:      func() []byte { return nil },
			expectMin: http.StatusOK,
		},
		{
			name:    "run-status-callback",
			method:  http.MethodPost,
			pathFor: func(runID string) string { return "/api/v1/internal/runs/" + runID + "/status" },
			body: func() []byte {
				b, _ := json.Marshal(api.RunStatusUpdate{
					Status:     "success",
					DurationMs: 100,
				})
				return b
			},
			expectMin: http.StatusOK,
		},
		{
			name:    "failed-merges-audit",
			method:  http.MethodPost,
			pathFor: func(_ string) string { return "/api/v1/internal/failed-merges" },
			body: func() []byte {
				b, _ := json.Marshal(domain.FailedMerge{
					RunID:        "00000000-0000-0000-0000-000000000001",
					BranchName:   "run-1",
					ErrorKind:    "conflict_exhausted",
					ErrorMessage: "test",
				})
				return b
			},
			expectMin: http.StatusOK,
		},
		{
			name:    "plugin-register-canonical",
			method:  http.MethodPost,
			pathFor: func(_ string) string { return "/api/v1/internal/plugins/register" },
			body: func() []byte {
				return []byte(`{"name":"new-path-plugin","addr":"new-path-plugin:50099"}`)
			},
			expectMin: http.StatusOK,
		},
		{
			name:    "plugin-register-legacy-alias",
			method:  http.MethodPost,
			pathFor: func(_ string) string { return "/internal/plugins/register" },
			body: func() []byte {
				return []byte(`{"name":"old-path-plugin","addr":"old-path-plugin:50099"}`)
			},
			expectMin: http.StatusOK,
		},
	}
}

func TestMountAllInternalRoutes_AllFlagsTrue_AllRoutesReachable(t *testing.T) {
	srv, runID := fullInternalTestServerWithRun()
	r := chi.NewRouter()
	api.MountAllInternalRoutes(r, api.DefaultInternalRouterConfig(), srv)

	for _, route := range internalRoutesTable() {
		t.Run(route.name, func(t *testing.T) {
			var body *bytes.Reader
			if b := route.body(); b != nil {
				body = bytes.NewReader(b)
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(route.method, route.pathFor(runID), body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			// 404 means chi has no matching route — that is the failure
			// mode this test exists to catch. Anything else (200, 500,
			// 503 from a missing dep) means the route is wired.
			require.NotEqual(t, http.StatusNotFound, rec.Code,
				"route %s %s was not reachable — chi returned 404; expected the handler to run",
				route.method, route.pathFor(runID))
			require.GreaterOrEqual(t, rec.Code, route.expectMin,
				"route %s %s returned %d, expected at least %d",
				route.method, route.pathFor(runID), rec.Code, route.expectMin)
		})
	}
}

func TestMountAllInternalRoutes_DefaultConfig_AllRoutesEnabled(t *testing.T) {
	// The zero value of InternalRouterConfig MUST mean "everything
	// enabled" — operators (and our own NewInternalRouter) rely on the
	// safe-default semantics. A future field added without "true by
	// default" handling would break this test.
	srv, runID := fullInternalTestServerWithRun()
	r := chi.NewRouter()
	api.MountAllInternalRoutes(r, api.InternalRouterConfig{}, srv)

	for _, route := range internalRoutesTable() {
		t.Run(route.name, func(t *testing.T) {
			var body *bytes.Reader
			if b := route.body(); b != nil {
				body = bytes.NewReader(b)
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(route.method, route.pathFor(runID), body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.NotEqual(t, http.StatusNotFound, rec.Code,
				"zero-value config must enable %s %s — but chi returned 404",
				route.method, route.pathFor(runID))
		})
	}
}

func TestPublicRouter_StillDoesNotExposeAnyInternalRoute(t *testing.T) {
	// The whole point of the internal listener split (ADR-019) is that
	// the public router must NOT serve any internal path — including
	// the new /api/v1/internal/plugins/register alias. This test loops
	// the route table and asserts 404 from NewRouter for each one.
	srv := fullInternalTestServer()
	srv.Pipelines = newMemoryPipelineStore()
	srv.Versions = newMemoryVersionStore()
	srv.Namespaces = newMemoryNamespaceStore()
	srv.Schedules = newMemoryScheduleStore()
	srv.Storage = newMemoryStorageStore()
	srv.Quality = newMemoryQualityStore()
	srv.Query = newMemoryQueryStore()
	srv.LandingZones = newMemoryLandingZoneStore()
	srv.Triggers = newMemoryTriggerStore()
	router := api.NewRouter(srv)

	const runID = "00000000-0000-0000-0000-000000000001"

	for _, route := range internalRoutesTable() {
		t.Run(route.name, func(t *testing.T) {
			// /health is INTENTIONALLY served on both listeners. We
			// skip the health rows here because they aren't internal
			// in any meaningful sense — they're mirrored, not leaked.
			if strings.HasPrefix(route.pathFor(runID), "/health") {
				t.Skip("health endpoints are mirrored on both routers by design")
			}
			var body *bytes.Reader
			if b := route.body(); b != nil {
				body = bytes.NewReader(b)
			} else {
				body = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(route.method, route.pathFor(runID), body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusNotFound, rec.Code,
				"public router MUST 404 internal path %s %s — got %d",
				route.method, route.pathFor(runID), rec.Code)
		})
	}
}

func TestInternalRouter_PluginRegister_BothPathsWork(t *testing.T) {
	// The URL-shape harmonisation keeps the legacy /internal/plugins/register
	// working as a deprecated alias while the canonical
	// /api/v1/internal/plugins/register is the new shape. Both must
	// succeed; only the legacy path emits a deprecation WARN.

	var mu sync.Mutex
	var legacyRegistered, canonicalRegistered string
	mgr := &mockPluginManager{
		registerFunc: func(_ context.Context, name, _ string) error {
			mu.Lock()
			defer mu.Unlock()
			if strings.HasPrefix(name, "legacy") {
				legacyRegistered = name
			} else {
				canonicalRegistered = name
			}
			return nil
		},
	}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewInternalRouter(srv)

	// Capture slog output so we can assert a deprecation WARN fires for
	// the legacy path and DOES NOT fire for the canonical one. A buffer
	// + JSON handler lets us inspect each line without coupling to the
	// global default logger.
	var logs syncBuf
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// ── Canonical (new) path ──
	canonical := `{"name":"canonical-test-plugin","addr":"canonical-test-plugin:50099"}`
	reqNew := httptest.NewRequest(http.MethodPost, "/api/v1/internal/plugins/register", strings.NewReader(canonical))
	reqNew.Header.Set("Content-Type", "application/json")
	recNew := httptest.NewRecorder()
	router.ServeHTTP(recNew, reqNew)
	require.Equal(t, http.StatusOK, recNew.Code,
		"canonical /api/v1/internal/plugins/register must succeed: %s", recNew.Body.String())

	// Drain the buffer so we can inspect ONLY the lines from the legacy
	// request that follows.
	logsBeforeLegacy := logs.String()
	logs.Reset()

	// ── Legacy alias path ──
	legacy := `{"name":"legacy-test-plugin","addr":"legacy-test-plugin:50099"}`
	reqOld := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", strings.NewReader(legacy))
	reqOld.Header.Set("Content-Type", "application/json")
	reqOld.RemoteAddr = "10.0.0.42:55555" // unique addr so the dedup limiter doesn't suppress us
	recOld := httptest.NewRecorder()
	router.ServeHTTP(recOld, reqOld)
	require.Equal(t, http.StatusOK, recOld.Code,
		"legacy /internal/plugins/register must STILL succeed (alias for one release cycle): %s", recOld.Body.String())

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "canonical-test-plugin", canonicalRegistered,
		"canonical path should have triggered Register()")
	assert.Equal(t, "legacy-test-plugin", legacyRegistered,
		"legacy path should have triggered Register() too")

	// The canonical path must NOT have emitted a deprecation WARN.
	assert.NotContains(t, logsBeforeLegacy, "deprecated:",
		"canonical /api/v1/internal/plugins/register must NOT log a deprecation WARN")

	// The legacy path MUST emit a deprecation WARN.
	legacyLogs := logs.String()
	assert.Contains(t, legacyLogs, "deprecated:",
		"legacy /internal/plugins/register must emit a deprecation WARN")
	assert.Contains(t, legacyLogs, "/api/v1/internal/plugins/register",
		"deprecation WARN must point operators at the new path")
}

// syncBuf is a goroutine-safe bytes.Buffer for capturing slog output.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuf) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}
