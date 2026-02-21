package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePluginBundle_ProxiesBundleContent(t *testing.T) {
	// Upstream serves the JS bundle.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`(function(){window.__RAT_REGISTER_PLUGIN("test",{})})();`))
	}))
	defer upstream.Close()

	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         "http://localhost:9999",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
		Descriptor: &pluginv1.DescribeResponse{
			Ui: &pluginv1.PluginUIDescriptor{
				BundleUrl: upstream.URL + "/bundle.js",
			},
		},
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/my-plugin/ui/bundle.js", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/javascript", rec.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=300", rec.Header().Get("Cache-Control"))

	body, _ := io.ReadAll(rec.Body)
	assert.Contains(t, string(body), "__RAT_REGISTER_PLUGIN")
}

func TestHandlePluginBundle_PluginNotFound_Returns404(t *testing.T) {
	reg := plugins.NewRegistry("pro")
	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/nonexistent/ui/bundle.js", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlePluginBundle_NoUIDescriptor_Returns404(t *testing.T) {
	reg := plugins.NewRegistry("pro")
	require.NoError(t, reg.Register(&plugins.Plugin{
		Name:         "my-plugin",
		Addr:         "http://localhost:9999",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{},
		Descriptor:   &pluginv1.DescribeResponse{}, // No UI descriptor.
	}))

	srv := &api.Server{
		PluginRegistry: &mockPluginRegistryLive{registry: reg},
	}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/my-plugin/ui/bundle.js", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlePluginBundle_NoRegistry_Returns503(t *testing.T) {
	srv := &api.Server{} // No PluginRegistry.
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/my-plugin/ui/bundle.js", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
