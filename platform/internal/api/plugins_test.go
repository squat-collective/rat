package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPluginManager implements api.PluginManager for tests.
type mockPluginManager struct {
	registerFunc    func(ctx context.Context, name, addr string) error
	enableFunc      func(ctx context.Context, name string) error
	disableFunc     func(ctx context.Context, name string) error
	removeFunc      func(ctx context.Context, name string) error
	updateConfigErr error
}

func (m *mockPluginManager) Register(ctx context.Context, name, addr string) error {
	if m.registerFunc != nil {
		return m.registerFunc(ctx, name, addr)
	}
	return nil
}

func (m *mockPluginManager) Enable(ctx context.Context, name string) error {
	if m.enableFunc != nil {
		return m.enableFunc(ctx, name)
	}
	return nil
}

func (m *mockPluginManager) Disable(ctx context.Context, name string) error {
	if m.disableFunc != nil {
		return m.disableFunc(ctx, name)
	}
	return nil
}

func (m *mockPluginManager) Remove(ctx context.Context, name string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, name)
	}
	return nil
}

func (m *mockPluginManager) UpdateConfig(_ context.Context, name string, config json.RawMessage) (*domain.PluginEntry, error) {
	if m.updateConfigErr != nil {
		return nil, m.updateConfigErr
	}
	return &domain.PluginEntry{Name: name, Config: config}, nil
}

// mockPluginLister implements api.PluginLister for tests.
type mockPluginLister struct {
	plugins []domain.PluginEntry
}

func (m *mockPluginLister) ListPlugins(_ context.Context, _ domain.PluginFilter) ([]domain.PluginEntry, error) {
	return m.plugins, nil
}

func (m *mockPluginLister) GetPlugin(_ context.Context, name string) (*domain.PluginEntry, error) {
	for _, p := range m.plugins {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, nil
}

// ── Phone-home (POST /internal/plugins/register) ──────────────────────────

func TestHandlePluginRegister_Success(t *testing.T) {
	var registeredName, registeredAddr string
	mgr := &mockPluginManager{
		registerFunc: func(_ context.Context, name, addr string) error {
			registeredName = name
			registeredAddr = addr
			return nil
		},
	}
	srv := &api.Server{PluginManager: mgr}

	body := `{"name":"auth","addr":"auth:50060"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandlePluginRegister(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "auth", registeredName)
	assert.Equal(t, "auth:50060", registeredAddr)
}

func TestHandlePluginRegister_MissingName(t *testing.T) {
	srv := &api.Server{PluginManager: &mockPluginManager{}}

	body := `{"addr":"auth:50060"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandlePluginRegister(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePluginRegister_InvalidName(t *testing.T) {
	srv := &api.Server{PluginManager: &mockPluginManager{}}

	body := `{"name":"Auth-Plugin!","addr":"auth:50060"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandlePluginRegister(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePluginRegister_NoManager(t *testing.T) {
	srv := &api.Server{} // No PluginManager set.

	body := `{"name":"auth","addr":"auth:50060"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandlePluginRegister(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandlePluginRegister_ManagerError(t *testing.T) {
	mgr := &mockPluginManager{
		registerFunc: func(_ context.Context, _, _ string) error {
			return fmt.Errorf("health check failed")
		},
	}
	srv := &api.Server{PluginManager: mgr}

	body := `{"name":"auth","addr":"auth:50060"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.HandlePluginRegister(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ── List (GET /api/v1/plugins) ────────────────────────────────────────────

func TestHandleListPlugins_Empty(t *testing.T) {
	srv := &api.Server{PluginCatalog: &mockPluginLister{}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", http.NoBody)
	rec := httptest.NewRecorder()

	srv.HandleListPlugins(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var plugins []domain.PluginEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&plugins))
	assert.Empty(t, plugins)
}

func TestHandleListPlugins_WithPlugins(t *testing.T) {
	lister := &mockPluginLister{
		plugins: []domain.PluginEntry{
			{Name: "auth", Status: domain.PluginStatusEnabled},
			{Name: "executor", Status: domain.PluginStatusEnabled},
		},
	}
	srv := &api.Server{PluginCatalog: lister}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", http.NoBody)
	rec := httptest.NewRecorder()

	srv.HandleListPlugins(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var plugins []domain.PluginEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&plugins))
	assert.Len(t, plugins, 2)
}

func TestHandleListPlugins_NoCatalog_ReturnsEmpty(t *testing.T) {
	srv := &api.Server{} // No PluginCatalog set.

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", http.NoBody)
	rec := httptest.NewRecorder()

	srv.HandleListPlugins(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ── Get (GET /api/v1/plugins/{name}) ──────────────────────────────────────

func TestHandleGetPlugin_Found(t *testing.T) {
	lister := &mockPluginLister{
		plugins: []domain.PluginEntry{
			{Name: "auth", Version: "1.0.0", Status: domain.PluginStatusEnabled},
		},
	}
	srv := &api.Server{PluginCatalog: lister, PluginManager: &mockPluginManager{}}

	// Use the full router to get chi URL params.
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/auth", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var plugin domain.PluginEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&plugin))
	assert.Equal(t, "auth", plugin.Name)
}

func TestHandleGetPlugin_NotFound(t *testing.T) {
	srv := &api.Server{PluginCatalog: &mockPluginLister{}, PluginManager: &mockPluginManager{}}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/nonexistent", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ── Enable (PUT /api/v1/plugins/{name}/enable) ───────────────────────────

func TestHandleEnablePlugin_Success(t *testing.T) {
	mgr := &mockPluginManager{}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/plugins/auth/enable", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ── Disable (PUT /api/v1/plugins/{name}/disable) ─────────────────────────

func TestHandleDisablePlugin_Success(t *testing.T) {
	mgr := &mockPluginManager{}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/plugins/auth/disable", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ── Delete (DELETE /api/v1/plugins/{name}) ────────────────────────────────

func TestHandleDeletePlugin_Success(t *testing.T) {
	mgr := &mockPluginManager{}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/auth", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// ── Plugin Source mocks & tests ───────────────────────────────────────────

type mockPluginSourceStore struct {
	sources []domain.PluginSource
}

func (m *mockPluginSourceStore) ListPluginSources(_ context.Context) ([]domain.PluginSource, error) {
	return m.sources, nil
}

func (m *mockPluginSourceStore) CreatePluginSource(_ context.Context, src domain.PluginSource) (*domain.PluginSource, error) {
	src.CreatedAt = time.Now()
	m.sources = append(m.sources, src)
	return &src, nil
}

func (m *mockPluginSourceStore) DeletePluginSource(_ context.Context, id uuid.UUID) error {
	for i, s := range m.sources {
		if s.ID == id {
			m.sources = append(m.sources[:i], m.sources[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("source not found")
}

func TestHandleListPluginSources_Empty(t *testing.T) {
	store := &mockPluginSourceStore{}
	srv := &api.Server{PluginSources: store}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugin-sources", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var sources []domain.PluginSource
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&sources))
	assert.Empty(t, sources)
}

func TestHandleListPluginSources_NilStore_Returns404(t *testing.T) {
	srv := &api.Server{} // No PluginSources — routes not mounted.
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugin-sources", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleCreatePluginSource_Success(t *testing.T) {
	store := &mockPluginSourceStore{}
	srv := &api.Server{PluginSources: store}
	router := api.NewRouter(srv)

	body := `{"type":"oci","url":"registry.example.com/plugins"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin-sources", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var src domain.PluginSource
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&src))
	assert.Equal(t, "oci", src.Type)
	assert.Equal(t, "registry.example.com/plugins", src.URL)
	assert.True(t, src.Enabled, "enabled should default to true")
	assert.False(t, src.Trusted)
}

func TestHandleCreatePluginSource_MissingFields(t *testing.T) {
	store := &mockPluginSourceStore{}
	srv := &api.Server{PluginSources: store}
	router := api.NewRouter(srv)

	body := `{"type":"oci"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin-sources", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleDeletePluginSource_Success(t *testing.T) {
	id := uuid.New()
	store := &mockPluginSourceStore{
		sources: []domain.PluginSource{{ID: id, Type: "oci", URL: "example.com"}},
	}
	srv := &api.Server{PluginSources: store}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugin-sources/"+id.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, store.sources)
}

func TestHandleDeletePluginSource_InvalidID(t *testing.T) {
	store := &mockPluginSourceStore{}
	srv := &api.Server{PluginSources: store}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugin-sources/not-a-uuid", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── Plugin Policy mocks & tests ───────────────────────────────────────────

type mockPluginPolicyStore struct {
	policies []domain.PluginPolicy
}

func (m *mockPluginPolicyStore) ListPluginPolicies(_ context.Context) ([]domain.PluginPolicy, error) {
	return m.policies, nil
}

func (m *mockPluginPolicyStore) CreatePluginPolicy(_ context.Context, policy domain.PluginPolicy) (*domain.PluginPolicy, error) {
	policy.CreatedAt = time.Now()
	m.policies = append(m.policies, policy)
	return &policy, nil
}

func (m *mockPluginPolicyStore) DeletePluginPolicy(_ context.Context, id uuid.UUID) error {
	for i, p := range m.policies {
		if p.ID == id {
			m.policies = append(m.policies[:i], m.policies[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("policy not found")
}

func TestHandleListPluginPolicies_Empty(t *testing.T) {
	store := &mockPluginPolicyStore{}
	srv := &api.Server{PluginPolicies: store}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugin-policies", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var policies []domain.PluginPolicy
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&policies))
	assert.Empty(t, policies)
}

func TestHandleListPluginPolicies_NilStore_Returns404(t *testing.T) {
	srv := &api.Server{} // No PluginPolicies — routes not mounted.
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugin-policies", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleCreatePluginPolicy_Success(t *testing.T) {
	store := &mockPluginPolicyStore{}
	srv := &api.Server{PluginPolicies: store}
	router := api.NewRouter(srv)

	body := `{"rule":"allow","pattern":"auth-*"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var policy domain.PluginPolicy
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&policy))
	assert.Equal(t, "allow", policy.Rule)
	assert.Equal(t, "auth-*", policy.Pattern)
}

func TestHandleCreatePluginPolicy_InvalidRule(t *testing.T) {
	store := &mockPluginPolicyStore{}
	srv := &api.Server{PluginPolicies: store}
	router := api.NewRouter(srv)

	body := `{"rule":"maybe","pattern":"auth-*"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCreatePluginPolicy_MissingFields(t *testing.T) {
	store := &mockPluginPolicyStore{}
	srv := &api.Server{PluginPolicies: store}
	router := api.NewRouter(srv)

	body := `{"rule":"allow"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugin-policies", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleDeletePluginPolicy_Success(t *testing.T) {
	id := uuid.New()
	store := &mockPluginPolicyStore{
		policies: []domain.PluginPolicy{{ID: id, Rule: "allow", Pattern: "auth-*"}},
	}
	srv := &api.Server{PluginPolicies: store}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugin-policies/"+id.String(), http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, store.policies)
}

func TestHandleDeletePluginPolicy_InvalidID(t *testing.T) {
	store := &mockPluginPolicyStore{}
	srv := &api.Server{PluginPolicies: store}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugin-policies/not-a-uuid", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
