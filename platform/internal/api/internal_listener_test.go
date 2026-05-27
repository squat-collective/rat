package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the trust boundary between the public listener and the
// private (internal) listener. The two routers share the same *Server but
// must serve different surfaces:
//
//   - Public listener (NewRouter): end-user APIs only. /api/v1/internal/* and
//     /internal/plugins/register MUST 404 — leaking their existence would
//     give an attacker a roadmap.
//   - Internal listener (NewInternalRouter): service-to-service callbacks
//     only. End-user APIs MUST NOT be reachable here (they would bypass auth,
//     rate limiting, CORS, audit, and authorization).

// ── Public listener must NOT expose internal routes ──────────────────────

func TestPublicRouter_DoesNotExposeRunStatusCallback(t *testing.T) {
	store := newMemoryRunStore()
	run := &domain.Run{Status: domain.RunStatusRunning}
	_ = store.CreateRun(nil, run)

	srv := &api.Server{
		Runs: store,
		Executor: &mockCallbackExecutor{
			handleFunc: func(_ api.RunStatusUpdate) error { return nil },
		},
	}
	router := api.NewRouter(srv)

	body := api.RunStatusUpdate{
		RunID:  run.ID.String(),
		Status: "success",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/"+run.ID.String()+"/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"public router must return 404 for the internal run-status callback")
}

func TestPublicRouter_DoesNotExposePluginRegister(t *testing.T) {
	srv := &api.Server{PluginManager: &mockPluginManager{}}
	router := api.NewRouter(srv)

	body := `{"name":"auth","addr":"auth:50060"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"public router must return 404 for the internal plugin register phone-home")
}

// ── Internal listener must accept internal routes ────────────────────────

func TestInternalRouter_AcceptsRunStatusCallback(t *testing.T) {
	store := newMemoryRunStore()
	run := &domain.Run{Status: domain.RunStatusRunning}
	_ = store.CreateRun(nil, run)

	mock := &mockCallbackExecutor{
		handleFunc: func(_ api.RunStatusUpdate) error { return nil },
	}
	srv := &api.Server{
		Runs:     store,
		Executor: mock,
	}
	router := api.NewInternalRouter(srv)

	body := api.RunStatusUpdate{
		RunID:       run.ID.String(),
		Status:      "success",
		DurationMs:  100,
		RowsWritten: 1,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/runs/"+run.ID.String()+"/status", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"internal router must accept the run-status callback")
	assert.True(t, mock.called, "executor callback should have been invoked")
}

func TestInternalRouter_AcceptsPluginRegister(t *testing.T) {
	var got struct{ name, addr string }
	mgr := &mockPluginManager{
		registerFunc: func(_ context.Context, name, addr string) error {
			got.name, got.addr = name, addr
			return nil
		},
	}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewInternalRouter(srv)

	body := `{"name":"auth","addr":"auth:50060"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code,
		"internal router must accept the plugin register phone-home")
	assert.Equal(t, "auth", got.name)
	assert.Equal(t, "auth:50060", got.addr)
}

// ── Internal listener must NOT expose end-user APIs ──────────────────────

// The internal router is for trusted service-to-service traffic. End-user
// APIs are NOT mounted on it so a misconfigured internal port (e.g.
// accidentally bound to a public interface) doesn't grant unauthenticated
// access to the data plane.

func TestInternalRouter_DoesNotExposePipelinesAPI(t *testing.T) {
	srv := fullTestServer()
	router := api.NewInternalRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"internal router must NOT expose /api/v1/pipelines")
}

func TestInternalRouter_DoesNotExposeNamespacesAPI(t *testing.T) {
	srv := fullTestServer()
	router := api.NewInternalRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"internal router must NOT expose /api/v1/namespaces")
}

// ── Public listener still serves end-user APIs ───────────────────────────

func TestPublicRouter_StillServesNamespaces(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"public router must still serve /api/v1/namespaces")
}

func TestPublicRouter_StillServesPipelines(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipelines", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"public router must still serve /api/v1/pipelines")
}

// ── Health is mirrored on both listeners ────────────────────────────────

// /health is needed on the internal listener so a container probe pointed at
// the internal port doesn't have to talk back to the public port to check
// liveness.

func TestInternalRouter_ServesHealth(t *testing.T) {
	srv := &api.Server{}
	router := api.NewInternalRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"internal router must serve /health for container probes")
}

// ── Identity routes are user-facing, mounted on public listener only ─────

// Wave 5 (commit 0d7529f) wired MountIdentityRoutes into router.go's public
// listener. These tests pin that wiring so a future refactor that drops the
// MountIdentityRoutes(vr, srv) call would fail loudly instead of silently
// regressing Pro identity plugins.
//
// The distinction we assert is 401 vs 404:
//   - 401 (Unauthorized): chi has the route mounted, the handler ran, and
//     requireIdentity self-gated on missing auth context.
//   - 404 (Not Found): chi has no route — the mount call is missing.

func TestPublicRouter_ServesIdentityRoutes(t *testing.T) {
	srv := fullTestServer()
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/identity/users", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"public router must serve /api/v1/identity/users — handler self-gates on auth (401), not chi 404")
}

func TestInternalRouter_DoesNotExposeIdentity(t *testing.T) {
	srv := fullTestServer()
	router := api.NewInternalRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/identity/users", http.NoBody)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"internal router must NOT expose /api/v1/identity/users — identity is user-facing, not service-to-service")
}
