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
