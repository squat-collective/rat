package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

// stubPluginHandler is a minimal PluginServiceHandler so we can verify
// MountStandardPluginRoutes wires ConnectRPC paths correctly.
type stubPluginHandler struct {
	pluginv1connect.UnimplementedPluginServiceHandler
}

func (s *stubPluginHandler) HealthCheck(
	_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest],
) (*connect.Response[pluginv1.HealthCheckResponse], error) {
	return connect.NewResponse(&pluginv1.HealthCheckResponse{
		Status: pluginv1.Status_STATUS_SERVING, Message: "ok",
	}), nil
}

func TestMountStandardPluginRoutes_BundleEndpointIsUnauthenticated(t *testing.T) {
	mux := http.NewServeMux()
	rest := http.NewServeMux()
	rest.HandleFunc("/api/data", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("data")) })

	handler := MountStandardPluginRoutes(mux, &stubPluginHandler{}, []byte("console.log('hi');"), "secret-token", rest)

	req := httptest.NewRequest(http.MethodGet, "/bundle.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/bundle.js should be 200 without token, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/javascript" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if got := rec.Body.String(); got != "console.log('hi');" {
		t.Errorf("body = %q", got)
	}
}

func TestMountStandardPluginRoutes_RESTPathsAreProtected(t *testing.T) {
	mux := http.NewServeMux()
	rest := http.NewServeMux()
	rest.HandleFunc("/api/data", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("data")) })

	handler := MountStandardPluginRoutes(mux, &stubPluginHandler{}, []byte("x"), "secret-token", rest)

	// No token → 401.
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("REST path without token should be 401, got %d", rec.Code)
	}

	// With token → 200.
	req = httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-RAT-Plugin-Token", "secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("REST path with token should be 200, got %d", rec.Code)
	}
}

func TestMountStandardPluginRoutes_EmptyTokenLeavesRESTOpen(t *testing.T) {
	mux := http.NewServeMux()
	rest := http.NewServeMux()
	rest.HandleFunc("/api/data", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("data")) })

	handler := MountStandardPluginRoutes(mux, &stubPluginHandler{}, []byte("x"), "", rest)

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty token should leave REST open, got %d", rec.Code)
	}
}

func TestMountStandardPluginRoutes_NilRestMux_NoPanic(t *testing.T) {
	mux := http.NewServeMux()

	// Should not panic when restMux is nil — defensive normalization
	// replaces it with http.NotFoundHandler() before the TokenAuth wrap.
	var handler http.Handler
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MountStandardPluginRoutes panicked with nil restMux: %v", r)
			}
		}()
		handler = MountStandardPluginRoutes(mux, &stubPluginHandler{}, []byte("x"), "secret-token", nil)
	}()

	// The bundle endpoint should still work — that path doesn't touch restMux.
	req := httptest.NewRequest(http.MethodGet, "/bundle.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/bundle.js should remain 200 in no-REST mode, got %d", rec.Code)
	}

	// An authenticated REST call should hit the NotFoundHandler — 404, not panic.
	req = httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	req.Header.Set("X-RAT-Plugin-Token", "secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("authenticated REST call with nil restMux should be 404, got %d", rec.Code)
	}

	// An unauthenticated REST call should still be rejected at the TokenAuth layer (401).
	req = httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated REST call with nil restMux should be 401, got %d", rec.Code)
	}
}

func TestMountStandardPluginRoutes_EmptyBundleJS_NoBundleEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	rest := http.NewServeMux()
	rest.HandleFunc("/api/data", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("data")) })

	// Empty bundleJS — no UI; the /bundle.js endpoint should not be mounted.
	handler := MountStandardPluginRoutes(mux, &stubPluginHandler{}, nil, "secret-token", rest)

	// /bundle.js should NOT be served by the bundle handler. Since "/" is
	// mounted with TokenAuth(rest) and rest has no handler for /bundle.js,
	// an unauthenticated request is rejected at the TokenAuth allowlist
	// boundary (which still allowlists /bundle.js, sending it through
	// rest, which 404s).
	req := httptest.NewRequest(http.MethodGet, "/bundle.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK && rec.Header().Get("Content-Type") == "application/javascript" {
		t.Fatalf("/bundle.js should NOT be mounted when bundleJS is empty, got %d Content-Type=%q",
			rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/bundle.js without bundle should fall through to 404, got %d", rec.Code)
	}

	// Also verify the empty-slice form of "no bundle" is handled identically.
	mux2 := http.NewServeMux()
	rest2 := http.NewServeMux()
	handler2 := MountStandardPluginRoutes(mux2, &stubPluginHandler{}, []byte{}, "secret-token", rest2)
	req = httptest.NewRequest(http.MethodGet, "/bundle.js", nil)
	rec = httptest.NewRecorder()
	handler2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/bundle.js with []byte{} bundle should be 404, got %d", rec.Code)
	}

	// REST surface still works with a token.
	req = httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("X-RAT-Plugin-Token", "secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("REST path should still be 200 with token, got %d", rec.Code)
	}
}

func TestH2CHandler_WrapsHandler(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("served")) })
	wrapped := H2CHandler(inner)
	if wrapped == nil {
		t.Fatal("H2CHandler returned nil")
	}
	// Smoke test: a plain HTTP/1.1 request should still flow through.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Body.String() != "served" {
		t.Errorf("body = %q", rec.Body.String())
	}
}
