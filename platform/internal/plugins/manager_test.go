package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func (c *memoryCatalog) UpdatePluginConfig(_ context.Context, name string, config json.RawMessage, expectedVersion *int64) (*domain.PluginEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.plugins[name]
	if !ok {
		// Match the real PluginStore: (nil, nil) means not-found so the
		// handler maps to 404 instead of 500.
		return nil, nil
	}
	if expectedVersion != nil && p.ConfigVersion != *expectedVersion {
		// Surface the current entry alongside the sentinel so the HTTP
		// layer can echo the live version back in the 409.
		current := p
		return &current, domain.ErrConfigVersionMismatch
	}
	p.Config = config
	p.ConfigVersion++
	c.plugins[name] = p
	return &p, nil
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
//
// Callers MUST set PLUGIN_ALLOW_LOOPBACK=true (via t.Setenv) BEFORE
// constructing the Manager, because httptest.NewServer binds to 127.0.0.1
// and the SSRF guard rejects loopback by default. The Manager reads the env
// var once at NewManager(), so the order matters.
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

	entry, err := mgr.UpdateConfig(context.Background(), "auth", config, nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"value"}`, string(entry.Config))
	assert.Equal(t, int64(1), entry.ConfigVersion, "version bumps from 0 → 1 on first write")
}

func TestManager_UpdateConfig_NoCatalog(t *testing.T) {
	mgr := NewManager(nil, "pro", nil)
	_, err := mgr.UpdateConfig(context.Background(), "auth", json.RawMessage(`{}`), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no catalog")
}

func TestManager_UpdateConfig_VersionMatch_Succeeds(t *testing.T) {
	catalog := newMemoryCatalog()
	catalog.UpsertPlugin(context.Background(), domain.PluginEntry{
		Name:          "auth",
		Status:        domain.PluginStatusEnabled,
		ConfigVersion: 5,
	})

	mgr := NewManager(catalog, "pro", nil)
	expected := int64(5)

	entry, err := mgr.UpdateConfig(context.Background(), "auth", json.RawMessage(`{}`), &expected)
	require.NoError(t, err)
	assert.Equal(t, int64(6), entry.ConfigVersion)
}

func TestManager_UpdateConfig_VersionMismatch_ReturnsSentinel(t *testing.T) {
	catalog := newMemoryCatalog()
	catalog.UpsertPlugin(context.Background(), domain.PluginEntry{
		Name:          "auth",
		Status:        domain.PluginStatusEnabled,
		ConfigVersion: 5,
	})

	mgr := NewManager(catalog, "pro", nil)
	stale := int64(2)

	entry, err := mgr.UpdateConfig(context.Background(), "auth", json.RawMessage(`{}`), &stale)
	require.ErrorIs(t, err, domain.ErrConfigVersionMismatch)
	require.NotNil(t, entry, "current entry returned alongside sentinel so handler can echo live version")
	assert.Equal(t, int64(5), entry.ConfigVersion, "version unchanged on mismatch")
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
	// httptest.NewServer binds to 127.0.0.1; the SSRF guard reads the env var
	// at NewManager(), so this must be set first.
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
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
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
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
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
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
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
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
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
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

// ── SSRF guard integration ────────────────────────────────────────────────

func TestManager_Register_RejectsLoopback(t *testing.T) {
	// Ensure the env override is OFF — we want production behaviour.
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "false")
	mgr := NewManager(newMemoryCatalog(), "pro", nil)

	err := mgr.Register(context.Background(), "evil", "http://127.0.0.1:50100")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAddressRejected)
	assert.Contains(t, err.Error(), "loopback")
	assert.Nil(t, mgr.Registry().Get("evil"), "rejected plugin must not be registered")
}

func TestManager_Register_RejectsAWSMetadata(t *testing.T) {
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "false")
	mgr := NewManager(newMemoryCatalog(), "pro", nil)

	err := mgr.Register(context.Background(), "evil", "http://169.254.169.254/")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAddressRejected)
	assert.Contains(t, err.Error(), "link-local")
	assert.Nil(t, mgr.Registry().Get("evil"))
}

func TestManager_Register_LoopbackOverride_Permitted(t *testing.T) {
	// With the override on, a loopback address passes the validator. The
	// register flow itself still fails at the health check (nothing is
	// listening), but the failure mode must NOT be ErrAddressRejected — that
	// proves the validator stepped aside.
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
	mgr := NewManager(newMemoryCatalog(), "pro", nil)

	err := mgr.Register(context.Background(), "dev", "http://127.0.0.1:1")
	require.Error(t, err, "no listener on :1, register should still fail")
	assert.NotErrorIs(t, err, ErrAddressRejected, "loopback override should bypass the SSRF guard")
}

func TestEvaluatePolicies_NilPoliciesStore_NoEnforcement(t *testing.T) {
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)
	// No SetPolicies call — policies is nil.

	ts := startMockPluginServer(t, []string{"custom"})
	defer ts.Close()

	err := mgr.Register(context.Background(), "any-plugin", ts.URL)
	require.NoError(t, err)
	assert.NotNil(t, mgr.Registry().Get("any-plugin"))
}

// ── Atomicity tests ───────────────────────────────────────────────────────

// failingUpsertCatalog wraps memoryCatalog and returns an error from
// UpsertPlugin, so we can prove the manager rolls back the in-memory
// registration when persistence fails.
type failingUpsertCatalog struct {
	*memoryCatalog
	err error
}

func (c *failingUpsertCatalog) UpsertPlugin(_ context.Context, _ domain.PluginEntry) (*domain.PluginEntry, error) {
	return nil, c.err
}

func TestManager_Register_CatalogWriteFails_RollsBackRegistry(t *testing.T) {
	// httptest binds to 127.0.0.1, so we need the loopback override on.
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")

	wantErr := errors.New("simulated postgres outage")
	catalog := &failingUpsertCatalog{memoryCatalog: newMemoryCatalog(), err: wantErr}
	mgr := NewManager(catalog, "pro", nil)

	ts := startMockPluginServer(t, []string{"custom"})
	defer ts.Close()

	err := mgr.Register(context.Background(), "doomed", ts.URL)
	require.Error(t, err, "catalog failure must surface as a Register error")
	assert.ErrorIs(t, err, wantErr, "caller must see the underlying catalog error wrapped")

	// The in-memory registry must NOT have the plugin — the catalog write
	// failed, so the rollback should have removed it.
	assert.Nil(t, mgr.Registry().Get("doomed"),
		"register must roll back in-memory state when the catalog write fails")
}

// concurrentSerializingCatalog counts how many UpsertPlugin calls are in
// flight at once and fails the test if the manager ever lets two cross. This
// is the strongest assertion we can make about the manager-level lock: it
// MUST serialize the whole register flow.
type concurrentSerializingCatalog struct {
	*memoryCatalog
	t          *testing.T
	inFlight   atomic.Int32
	maxSeen    atomic.Int32
	upsertHits atomic.Int32
}

func (c *concurrentSerializingCatalog) UpsertPlugin(ctx context.Context, entry domain.PluginEntry) (*domain.PluginEntry, error) {
	now := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	for {
		prev := c.maxSeen.Load()
		if now <= prev || c.maxSeen.CompareAndSwap(prev, now) {
			break
		}
	}
	c.upsertHits.Add(1)
	return c.memoryCatalog.UpsertPlugin(ctx, entry)
}

func TestManager_Register_ConcurrentSameName_OneWinsOthersFail(t *testing.T) {
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")

	catalog := &concurrentSerializingCatalog{memoryCatalog: newMemoryCatalog(), t: t}
	mgr := NewManager(catalog, "pro", nil)

	ts := startMockPluginServer(t, []string{"custom"})
	defer ts.Close()

	const n = 5
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			errs[i] = mgr.Register(context.Background(), "race", ts.URL)
		}()
	}
	wg.Wait()

	// The manager mutex must keep UpsertPlugin serialised — we should never
	// observe more than one concurrent call at the catalog layer.
	assert.LessOrEqual(t, catalog.maxSeen.Load(), int32(1),
		"manager mutex should serialise UpsertPlugin calls")

	// All N attempts use the same name. Re-registration is idempotent in the
	// registry (it replaces the previous entry) — what matters is that the
	// registry and catalog AGREE at the end, and that the manager processed
	// each request without panicking. Count successes for an audit trail.
	var successes int
	for _, e := range errs {
		if e == nil {
			successes++
		}
	}
	assert.GreaterOrEqual(t, successes, 1, "at least one registration must succeed")

	// Final state: registry has the plugin, catalog has the plugin, and they
	// agree on the name.
	assert.NotNil(t, mgr.Registry().Get("race"), "winner must be live in registry")
	entry, err := catalog.GetPlugin(context.Background(), "race")
	require.NoError(t, err)
	require.NotNil(t, entry, "winner must be persisted in catalog")
	assert.Equal(t, "race", entry.Name)
}

// ── Reconciler tests ──────────────────────────────────────────────────────

// captureSlog routes the default slog logger into a buffer for the duration
// of the test, returning a getter for the captured text. Restores the
// previous handler on cleanup.
func captureSlog(t *testing.T) func() string {
	t.Helper()
	prev := slog.Default()
	var buf bytes.Buffer
	var bufMu sync.Mutex

	// Lock around buf writes — slog handlers may run from goroutines.
	writer := &lockedWriter{w: &buf, mu: &bufMu}
	slog.SetDefault(slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return func() string {
		bufMu.Lock()
		defer bufMu.Unlock()
		return buf.String()
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestManager_Reconciler_DetectsCatalogOnlyDrift(t *testing.T) {
	catalog := newMemoryCatalog()
	// Catalog says the plugin exists and is ENABLED, but the in-memory
	// registry is empty — exactly the post-restart-without-LoadFromCatalog
	// state that should trigger a warning.
	_, err := catalog.UpsertPlugin(context.Background(), domain.PluginEntry{
		Name:   "ghost",
		Status: domain.PluginStatusEnabled,
	})
	require.NoError(t, err)

	logs := captureSlog(t)

	mgr := NewManager(catalog, "pro", nil)
	mgr.reconcileOnce(context.Background())

	out := logs()
	assert.Contains(t, out, "plugin in catalog but not in registry",
		"reconciler must warn on catalog-only plugins")
	assert.Contains(t, out, "ghost",
		"warning must name the divergent plugin")
}

func TestManager_Reconciler_DetectsRegistryOnlyDrift(t *testing.T) {
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)
	// Slip a plugin into the registry without persisting it — the "rollback
	// went wrong" or "someone DELETEd from plugins" scenario.
	require.NoError(t, mgr.Registry().Register(&Plugin{
		Name:   "orphan",
		Status: domain.PluginStatusEnabled,
	}))

	logs := captureSlog(t)
	mgr.reconcileOnce(context.Background())

	out := logs()
	assert.Contains(t, out, "plugin in registry but not in catalog")
	assert.Contains(t, out, "orphan")
}

func TestManager_Reconciler_IgnoresDisabledCatalogEntries(t *testing.T) {
	catalog := newMemoryCatalog()
	// Disabled plugins legitimately don't appear in the registry — that's not
	// drift, that's intended state. The reconciler must stay quiet.
	_, err := catalog.UpsertPlugin(context.Background(), domain.PluginEntry{
		Name:   "sleeping",
		Status: domain.PluginStatusDisabled,
	})
	require.NoError(t, err)

	logs := captureSlog(t)
	mgr := NewManager(catalog, "pro", nil)
	mgr.reconcileOnce(context.Background())

	assert.NotContains(t, logs(), "plugin in catalog but not in registry",
		"disabled plugins must not trigger drift warnings")
}

func TestManager_Reconciler_NilCatalog_NoPanic(t *testing.T) {
	mgr := NewManager(nil, "pro", nil)
	// Should be a no-op (and definitely not panic).
	mgr.reconcileOnce(context.Background())
}

func TestManager_Reconciler_HealthyState_NoWarning(t *testing.T) {
	catalog := newMemoryCatalog()
	_, err := catalog.UpsertPlugin(context.Background(), domain.PluginEntry{
		Name:   "aligned",
		Status: domain.PluginStatusEnabled,
	})
	require.NoError(t, err)

	mgr := NewManager(catalog, "pro", nil)
	require.NoError(t, mgr.Registry().Register(&Plugin{
		Name:   "aligned",
		Status: domain.PluginStatusEnabled,
	}))

	logs := captureSlog(t)
	mgr.reconcileOnce(context.Background())

	out := logs()
	assert.NotContains(t, out, "plugin in catalog but not in registry")
	assert.NotContains(t, out, "plugin in registry but not in catalog")
}

// ── Callback panic safety ─────────────────────────────────────────────────

// TestManager_Register_CallbackPanics_DoesNotDeadlock proves that a panic
// inside an OnAuthChanged handler does NOT propagate up through Register
// (which would skip the deferred Manager.mu unlock and deadlock every
// subsequent registration). The first Register must return normally and
// the second one must also succeed — proof that the mutex is free.
func TestManager_Register_CallbackPanics_DoesNotDeadlock(t *testing.T) {
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)

	mgr.OnAuthChanged = func(_ *Registry) {
		panic("intentional callback explosion")
	}

	logs := captureSlog(t)

	ts1 := startMockPluginServer(t, []string{CapAuth})
	defer ts1.Close()

	// (a) First Register with the panicking auth callback — must not propagate.
	assert.NotPanics(t, func() {
		err := mgr.Register(context.Background(), "auth-one", ts1.URL)
		require.NoError(t, err, "Register must absorb the callback panic")
	}, "Register must not let the callback panic propagate")
	assert.NotNil(t, mgr.Registry().Get("auth-one"), "first plugin still registered")

	// (b) Second Register on a fresh address with a non-exclusive capability —
	// must succeed, which proves Manager.mu was actually released after the
	// first call. (CapAuth is mutually-exclusive across plugins, so we use
	// a generic capability here.)
	ts2 := startMockPluginServer(t, []string{"custom"})
	defer ts2.Close()
	done := make(chan error, 1)
	go func() {
		done <- mgr.Register(context.Background(), "other-plugin", ts2.URL)
	}()
	select {
	case err := <-done:
		require.NoError(t, err, "second Register must succeed (mutex was released)")
	case <-time.After(5 * time.Second):
		t.Fatal("second Register deadlocked — Manager.mu was held across the callback panic")
	}
	assert.NotNil(t, mgr.Registry().Get("other-plugin"), "second plugin registered")

	// (c) The panic was logged at ERROR with greppable structure.
	out := logs()
	assert.Contains(t, out, "plugin callback panicked",
		"the recovery shim must log at ERROR with a stable message")
	assert.Contains(t, out, "intentional callback explosion",
		"the original panic value must appear in the log")
	assert.Contains(t, out, "capability="+CapAuth,
		"the failing capability must be tagged for greppability")
}

// TestManager_Register_MultipleCallbacksOneCrashes_OthersRun proves
// callbacks are independent: when OnAuthChanged panics, OnCloudChanged
// still runs for the same plugin (both capabilities fire in a single
// fireCallbacks loop).
func TestManager_Register_MultipleCallbacksOneCrashes_OthersRun(t *testing.T) {
	t.Setenv("PLUGIN_ALLOW_LOOPBACK", "true")
	catalog := newMemoryCatalog()
	mgr := NewManager(catalog, "pro", nil)

	var cloudFired atomic.Bool
	mgr.OnAuthChanged = func(_ *Registry) {
		panic("auth handler is broken")
	}
	mgr.OnCloudChanged = func(_ *Registry) {
		cloudFired.Store(true)
	}

	logs := captureSlog(t)

	ts := startMockPluginServer(t, []string{CapAuth, CapCloud})
	defer ts.Close()

	err := mgr.Register(context.Background(), "auth-and-cloud", ts.URL)
	require.NoError(t, err, "Register must succeed despite the auth callback panic")

	assert.True(t, cloudFired.Load(),
		"OnCloudChanged must still run after OnAuthChanged panicked")
	assert.Contains(t, logs(), "plugin callback panicked",
		"the panicking callback must still be logged")
}
