package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	connect "connectrpc.com/connect"
	"github.com/google/uuid"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Memory Catalog (test double) ──────────────────────────────────────────

type memoryCatalog struct {
	mu      sync.Mutex
	plugins map[string]domain.PluginEntry
}

func newMemoryCatalog() *memoryCatalog {
	return &memoryCatalog{plugins: make(map[string]domain.PluginEntry)}
}

func (c *memoryCatalog) UpsertPlugin(_ context.Context, entry domain.PluginEntry) (*domain.PluginEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.plugins[entry.Name] = entry
	return &entry, nil
}

func (c *memoryCatalog) ListPlugins(_ context.Context, filter domain.PluginFilter) ([]domain.PluginEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result []domain.PluginEntry
	for _, p := range c.plugins {
		if filter.Status != "" && string(p.Status) != filter.Status {
			continue
		}
		if filter.Kind != "" && string(p.Kind) != filter.Kind {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

func (c *memoryCatalog) GetPlugin(_ context.Context, name string) (*domain.PluginEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.plugins[name]; ok {
		return &p, nil
	}
	return nil, nil
}

func (c *memoryCatalog) DeletePlugin(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.plugins, name)
	return nil
}

func (c *memoryCatalog) UpdatePluginStatus(_ context.Context, name string, status domain.PluginStatus, errMsg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.plugins[name]; ok {
		p.Status = status
		p.Error = errMsg
		c.plugins[name] = p
	}
	return nil
}

func (c *memoryCatalog) UpdatePluginHealth(_ context.Context, name string, healthy bool, errMsg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.plugins[name]; ok {
		p.Healthy = healthy
		p.Error = errMsg
		c.plugins[name] = p
	}
	return nil
}

func (c *memoryCatalog) UpdatePluginConfig(_ context.Context, name string, config json.RawMessage) (*domain.PluginEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.plugins[name]; ok {
		p.Config = config
		c.plugins[name] = p
		return &p, nil
	}
	return nil, fmt.Errorf("plugin %s not found", name)
}

// ── Test helper: mock ConnectRPC plugin server ────────────────────────────

// mockPluginService is a minimal PluginService for Register-flow tests. It
// embeds Unimplemented so RPCs it does not provide return CodeUnimplemented.
type mockPluginService struct {
	pluginv1connect.UnimplementedPluginServiceHandler
	caps []string
}

func (m *mockPluginService) HealthCheck(
	_ context.Context, _ *connect.Request[pluginv1.HealthCheckRequest],
) (*connect.Response[pluginv1.HealthCheckResponse], error) {
	return connect.NewResponse(&pluginv1.HealthCheckResponse{
		Status: pluginv1.Status_STATUS_SERVING,
	}), nil
}

func (m *mockPluginService) Describe(
	_ context.Context, _ *connect.Request[pluginv1.DescribeRequest],
) (*connect.Response[pluginv1.DescribeResponse], error) {
	return connect.NewResponse(&pluginv1.DescribeResponse{
		Name:         "test-plugin",
		Version:      "1.0.0",
		Capabilities: m.caps,
	}), nil
}

// startMockPluginServer creates an HTTP test server running a real ConnectRPC
// PluginService handler, so the Manager's Connect client negotiates content
// types correctly. Used to test the Manager's Register flow.
func startMockPluginServer(t *testing.T, caps []string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	path, handler := pluginv1connect.NewPluginServiceHandler(&mockPluginService{caps: caps})
	mux.Handle(path, handler)

	return httptest.NewServer(mux)
}

// ── Manager Tests ─────────────────────────────────────────────────────────

func TestNewManager_NilCatalog(t *testing.T) {
	mgr := NewManager(nil, "community", nil)
	assert.NotNil(t, mgr)
	assert.NotNil(t, mgr.Registry())
	assert.Nil(t, mgr.Catalog())
}

func TestManager_SetCatalog(t *testing.T) {
	mgr := NewManager(nil, "community", nil)
	catalog := newMemoryCatalog()

	mgr.SetCatalog(catalog)

	assert.NotNil(t, mgr.Catalog())
}

func TestManager_LoadFromCatalog_NilCatalog(t *testing.T) {
	mgr := NewManager(nil, "community", nil)
	err := mgr.LoadFromCatalog(context.Background())
	assert.NoError(t, err, "nil catalog should return no error")
}

func TestManager_Disable(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)

	// Pre-populate a plugin in the registry.
	require.NoError(t, mgr.Registry().Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}))

	err := mgr.Disable(context.Background(), "auth")
	require.NoError(t, err)

	// Plugin should be removed from in-memory registry.
	assert.Nil(t, mgr.Registry().Get("auth"))
	assert.False(t, mgr.Registry().AuthEnabled())

	// Catalog should be updated.
	entry, _ := catalog.GetPlugin(context.Background(), "auth")
	if entry != nil {
		assert.Equal(t, domain.PluginStatusDisabled, entry.Status)
	}
}

func TestManager_Remove(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)

	// Pre-populate.
	require.NoError(t, mgr.Registry().Register(&Plugin{
		Name:         "auth",
		Addr:         "http://auth:50060",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapAuth},
	}))
	catalog.UpsertPlugin(context.Background(), domain.PluginEntry{
		Name:   "auth",
		Status: domain.PluginStatusEnabled,
	})

	// Track callbacks.
	authCallbackFired := false
	mgr.OnAuthChanged = func(_ *Registry) { authCallbackFired = true }

	err := mgr.Remove(context.Background(), "auth")
	require.NoError(t, err)

	assert.Nil(t, mgr.Registry().Get("auth"))

	// Catalog should be deleted.
	entry, _ := catalog.GetPlugin(context.Background(), "auth")
	assert.Nil(t, entry)

	// Callback should have fired.
	assert.True(t, authCallbackFired)
}

func TestManager_Callbacks_FireForCapabilities(t *testing.T) {
	mgr := NewManager(nil, "pro", nil)

	var authFired, execFired, enforceFired bool
	mgr.OnAuthChanged = func(_ *Registry) { authFired = true }
	mgr.OnExecutorChanged = func(_ *Registry) { execFired = true }
	mgr.OnEnforcementChanged = func(_ *Registry) { enforceFired = true }

	// Manually fire callbacks.
	mgr.fireCallbacks([]string{CapAuth, CapExecutor, CapEnforcement})

	assert.True(t, authFired)
	assert.True(t, execFired)
	assert.True(t, enforceFired)
}

func TestManager_Callbacks_NilCallbacks_NoPanic(t *testing.T) {
	mgr := NewManager(nil, "pro", nil)
	// All callbacks are nil — should not panic.
	mgr.fireCallbacks([]string{CapAuth, CapExecutor, CapEnforcement, CapCloud, CapSharing})
}

func TestManager_UpdateConfig(t *testing.T) {
	catalog := newMemoryCatalog()
	catalog.UpsertPlugin(context.Background(), domain.PluginEntry{
		Name:   "auth",
		Status: domain.PluginStatusEnabled,
	})

	mgr := NewManager(catalog, "pro", nil)
	config := json.RawMessage(`{"key": "value"}`)

	entry, err := mgr.UpdateConfig(context.Background(), "auth", config)
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"value"}`, string(entry.Config))
}

func TestManager_UpdateConfig_NoCatalog(t *testing.T) {
	mgr := NewManager(nil, "pro", nil)
	_, err := mgr.UpdateConfig(context.Background(), "auth", json.RawMessage(`{}`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no catalog")
}

func TestManager_Enable_NoCatalog(t *testing.T) {
	mgr := NewManager(nil, "pro", nil)
	err := mgr.Enable(context.Background(), "auth")
	assert.Error(t, err)
}

func TestDescribePlugin_Unimplemented_FallsBackToName(t *testing.T) {
	// Create a mock that returns Unimplemented for Describe.
	mock := &mockPluginServiceClient{
		describeFunc: func(_ context.Context, _ *connect.Request[pluginv1.DescribeRequest]) (*connect.Response[pluginv1.DescribeResponse], error) {
			return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
		},
	}

	mgr := NewManager(nil, "pro", nil)
	caps, _, version, descriptor := mgr.describePlugin(context.Background(), "auth", mock)

	assert.Equal(t, []string{CapAuth}, caps)
	assert.Equal(t, "", version)
	assert.Nil(t, descriptor)
}

func TestDescribePlugin_Success(t *testing.T) {
	mock := &mockPluginServiceClient{
		describeFunc: func(_ context.Context, _ *connect.Request[pluginv1.DescribeRequest]) (*connect.Response[pluginv1.DescribeResponse], error) {
			return connect.NewResponse(&pluginv1.DescribeResponse{
				Name:               "auth-keycloak",
				Version:            "2.1.0",
				Capabilities:       []string{CapAuth},
				EventSubscriptions: []string{"run_completed"},
			}), nil
		},
	}

	mgr := NewManager(nil, "pro", nil)
	caps, events, version, descriptor := mgr.describePlugin(context.Background(), "auth-keycloak", mock)

	assert.Equal(t, []string{CapAuth}, caps)
	assert.Equal(t, []string{"run_completed"}, events)
	assert.Equal(t, "2.1.0", version)
	assert.NotNil(t, descriptor)
}

func TestInferKind(t *testing.T) {
	assert.Equal(t, domain.PluginKindPlatform, inferKind([]string{CapAuth}))
	assert.Equal(t, domain.PluginKindPlatform, inferKind([]string{CapExecutor}))
	assert.Equal(t, domain.PluginKindPlatform, inferKind([]string{"custom-cap"}))
}

func TestInferCapabilitiesFromEntry_WithDescriptor(t *testing.T) {
	descriptor := `{"capabilities": ["auth", "enforcement"]}`
	entry := domain.PluginEntry{
		Name:       "auth",
		Descriptor: json.RawMessage(descriptor),
	}

	caps := inferCapabilitiesFromEntry(entry)
	assert.Equal(t, []string{"auth", "enforcement"}, caps)
}

func TestInferCapabilitiesFromEntry_NoDescriptor_FallsBackToName(t *testing.T) {
	entry := domain.PluginEntry{Name: "auth"}
	caps := inferCapabilitiesFromEntry(entry)
	assert.Equal(t, []string{CapAuth}, caps)
}

func TestManager_NotifyHealthTransition_FiresOnExecutorChanged(t *testing.T) {
	mgr := NewManager(nil, "pro", nil)

	// Register an executor plugin in the registry.
	require.NoError(t, mgr.Registry().Register(&Plugin{
		Name:         "executor",
		Addr:         "http://executor:50070",
		Status:       domain.PluginStatusEnabled,
		Capabilities: []string{CapExecutor},
	}))

	var execCallbackFired bool
	mgr.OnExecutorChanged = func(_ *Registry) { execCallbackFired = true }

	mgr.NotifyHealthTransition("executor")

	assert.True(t, execCallbackFired, "OnExecutorChanged should fire for executor capability")
}

func TestManager_NotifyHealthTransition_UnknownPlugin_NoPanic(t *testing.T) {
	mgr := NewManager(nil, "pro", nil)

	// Should not panic when plugin doesn't exist.
	mgr.NotifyHealthTransition("nonexistent")
}

// ── Policy Enforcement Tests ──────────────────────────────────────────────

// memoryPolicyStore is a test double for PluginPolicyLister.
type memoryPolicyStore struct {
	policies []domain.PluginPolicy
}

func (m *memoryPolicyStore) ListPluginPolicies(_ context.Context) ([]domain.PluginPolicy, error) {
	return m.policies, nil
}

func TestEvaluatePolicies_DenyBlocksRegistration(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)
	mgr.SetPolicies(&memoryPolicyStore{
		policies: []domain.PluginPolicy{
			{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Rule: "deny", Pattern: "evil-*"},
		},
	})

	// Start a mock plugin server.
	ts := startMockPluginServer(t, []string{"custom"})
	defer ts.Close()

	err := mgr.Register(context.Background(), "evil-plugin", ts.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied by policy")
	assert.Contains(t, err.Error(), "evil-plugin")

	// Plugin should NOT be in the registry.
	assert.Nil(t, mgr.Registry().Get("evil-plugin"))
}

func TestEvaluatePolicies_AllowPermitsRegistration(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)
	mgr.SetPolicies(&memoryPolicyStore{
		policies: []domain.PluginPolicy{
			{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Rule: "allow", Pattern: "good-*"},
			{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Rule: "deny", Pattern: "*"},
		},
	})

	ts := startMockPluginServer(t, []string{"custom"})
	defer ts.Close()

	err := mgr.Register(context.Background(), "good-plugin", ts.URL)
	require.NoError(t, err)

	// Plugin should be in the registry.
	assert.NotNil(t, mgr.Registry().Get("good-plugin"))
}

func TestEvaluatePolicies_NoPoliciesDefaultAllow(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)
	mgr.SetPolicies(&memoryPolicyStore{policies: nil})

	ts := startMockPluginServer(t, []string{"custom"})
	defer ts.Close()

	err := mgr.Register(context.Background(), "any-plugin", ts.URL)
	require.NoError(t, err)
	assert.NotNil(t, mgr.Registry().Get("any-plugin"))
}

func TestEvaluatePolicies_FirstMatchWins(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)
	mgr.SetPolicies(&memoryPolicyStore{
		policies: []domain.PluginPolicy{
			// First: allow auth-* specifically
			{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Rule: "allow", Pattern: "auth-*"},
			// Second: deny everything
			{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Rule: "deny", Pattern: "*"},
		},
	})

	ts := startMockPluginServer(t, []string{CapAuth})
	defer ts.Close()

	// auth-keycloak matches first rule (allow) — should succeed.
	err := mgr.Register(context.Background(), "auth-keycloak", ts.URL)
	require.NoError(t, err)
	assert.NotNil(t, mgr.Registry().Get("auth-keycloak"))
}

func TestEvaluatePolicies_KindScopedPolicy(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)
	mgr.SetPolicies(&memoryPolicyStore{
		policies: []domain.PluginPolicy{
			// Deny platform plugins matching "custom-*"
			{
				ID:      uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				Rule:    "deny",
				Pattern: "custom-*",
				Kind:    string(domain.PluginKindRunner),
			},
		},
	})

	ts := startMockPluginServer(t, []string{CapAuth})
	defer ts.Close()

	// custom-executor has platform kind (from CapAuth), policy targets runner kind — no match.
	// Should be allowed (no matching policy = default allow).
	err := mgr.Register(context.Background(), "custom-executor", ts.URL)
	require.NoError(t, err)
	assert.NotNil(t, mgr.Registry().Get("custom-executor"))
}

func TestEvaluatePolicies_NilPoliciesStore_NoEnforcement(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)
	// No SetPolicies call — policies is nil.

	ts := startMockPluginServer(t, []string{"custom"})
	defer ts.Close()

	err := mgr.Register(context.Background(), "any-plugin", ts.URL)
	require.NoError(t, err)
	assert.NotNil(t, mgr.Registry().Get("any-plugin"))
}
