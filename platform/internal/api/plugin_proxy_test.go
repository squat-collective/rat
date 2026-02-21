package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPluginRegistryLive implements api.PluginRegistryLive for proxy tests.
type mockPluginRegistryLive struct {
	registry *plugins.Registry
}

func (m *mockPluginRegistryLive) Get(name string) *plugins.Plugin {
	return m.registry.Get(name)
}

func TestHandlePluginProxy_ForwardsToPlugin(t *testing.T) {
	// Start a mock upstream plugin server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","path":"` + r.URL.Path + `"}`))
	}))
	defer upstream.Close()

	// Register a plugin pointing to the upstream.
	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         upstream.URL,
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin/test/path", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	body, _ := io.ReadAll(rec.Body)
	assert.Contains(t, string(body), `"/test/path"`)
}

func TestHandlePluginProxy_UnknownPlugin_Returns404(t *testing.T) {
	reg := plugins.NewRegistry("pro")
	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/nonexistent/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlePluginProxy_DisabledPlugin_Returns503(t *testing.T) {
	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         "http://localhost:9999",
		Status:       domain.PluginStatusDisabled,
		Capabilities: []string{},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandlePluginProxy_NoRegistry_Returns503(t *testing.T) {
	srv := &api.Server{} // No PluginRegistry.
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin/test", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandlePluginProxy_ForwardsQueryParams(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.RawQuery))
	}))
	defer upstream.Close()

	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         upstream.URL,
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin/search?q=hello&limit=10", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body, _ := io.ReadAll(rec.Body)
	assert.Contains(t, string(body), "q=hello")
	assert.Contains(t, string(body), "limit=10")
}

func TestHandlePluginProxy_PluginRootPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`path:` + r.URL.Path))
	}))
	defer upstream.Close()

	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         upstream.URL,
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	// Request to plugin root (no trailing path).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x/my-plugin", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body, _ := io.ReadAll(rec.Body)
	assert.Equal(t, "path:/", string(body))
}
