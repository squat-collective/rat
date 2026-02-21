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

// mockRunnerPluginLister implements api.RunnerPluginLister for testing.
type mockRunnerPluginLister struct {
	plugins []domain.RunnerPlugin
	err     error
}

func (m *mockRunnerPluginLister) ListRunnerPlugins(_ context.Context) ([]domain.RunnerPlugin, error) {
	return m.plugins, m.err
}

func TestHandleRunnerPlugins_ReturnsPlugins(t *testing.T) {
	lister := &mockRunnerPluginLister{
		plugins: []domain.RunnerPlugin{
			{Name: "soft_delete", Group: "rat.hooks", Version: "0.3.1", PackageName: "rat-plugin-soft-delete"},
			{Name: "full_refresh", Group: "rat.strategies", Version: "2.0.0", PackageName: "rat-runner"},
		},
	}
	srv := &api.Server{RunnerPlugins: lister}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runner/plugins", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var plugins []domain.RunnerPlugin
	err := json.NewDecoder(rec.Body).Decode(&plugins)
	require.NoError(t, err)
	assert.Len(t, plugins, 2)
	assert.Equal(t, "soft_delete", plugins[0].Name)
	assert.Equal(t, "rat.hooks", plugins[0].Group)
}

func TestHandleRunnerPlugins_EmptyWhenNoLister(t *testing.T) {
	srv := &api.Server{} // No RunnerPlugins
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runner/plugins", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var plugins []interface{}
	err := json.NewDecoder(rec.Body).Decode(&plugins)
	require.NoError(t, err)
	assert.Empty(t, plugins)
}

func TestHandleRunnerPlugins_InternalErrorOnFailure(t *testing.T) {
	lister := &mockRunnerPluginLister{
		err: errors.New("runner unreachable"),
	}
	srv := &api.Server{RunnerPlugins: lister}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runner/plugins", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
