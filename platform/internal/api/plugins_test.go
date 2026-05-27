package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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
	registerFunc     func(ctx context.Context, name, addr string) error
	enableFunc       func(ctx context.Context, name string) error
	disableFunc      func(ctx context.Context, name string) error
	removeFunc       func(ctx context.Context, name string) error
	updateConfigErr  error
	updateConfigFunc func(ctx context.Context, name string, config json.RawMessage, expectedVersion *int64) (*domain.PluginEntry, error)
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

func (m *mockPluginManager) UpdateConfig(ctx context.Context, name string, config json.RawMessage, expectedVersion *int64) (*domain.PluginEntry, error) {
	if m.updateConfigFunc != nil {
		return m.updateConfigFunc(ctx, name, config, expectedVersion)
	}
	if m.updateConfigErr != nil {
		return nil, m.updateConfigErr
	}
	return &domain.PluginEntry{Name: name, Config: config, ConfigVersion: 1}, nil
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

// ── Phone-home rate limiting ──────────────────────────────────────────────
//
// The /internal/plugins/register endpoint is unauthenticated (it's the
// phone-home address), so a runaway or compromised plugin could hammer it
// at full HTTP throughput. The per-name token bucket caps that to a burst
// of 10 followed by 10/min steady-state; the 11th rapid attempt returns 429
// with Retry-After so the client backs off.

func TestHandlePluginRegister_RateLimited(t *testing.T) {
	mgr := &mockPluginManager{}
	srv := &api.Server{PluginManager: mgr}

	// Unique name so other tests in the same run can't consume our budget.
	name := "rl-" + uuid.NewString()[:8]
	body := fmt.Sprintf(`{"name":%q,"addr":"%s:50099"}`, name, name)

	// 10 rapid register attempts should all succeed (burst capacity).
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.HandlePluginRegister(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "attempt %d should succeed within burst", i+1)
	}

	// The 11th attempt within the same window must be throttled with 429.
	req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.HandlePluginRegister(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "11th attempt should be rate-limited")
	assert.NotEmpty(t, rec.Header().Get("Retry-After"),
		"429 response must carry Retry-After so the client can back off")
	assert.Contains(t, rec.Body.String(), "RESOURCE_EXHAUSTED",
		"error body should signal RESOURCE_EXHAUSTED for parity with the per-IP limiter")
}

func TestHandlePluginRegister_RateLimit_PerName(t *testing.T) {
	mgr := &mockPluginManager{}
	srv := &api.Server{PluginManager: mgr}

	// Two distinct plugin names: blowing one's budget must NOT affect the other.
	nameA := "rla-" + uuid.NewString()[:8]
	nameB := "rlb-" + uuid.NewString()[:8]
	bodyA := fmt.Sprintf(`{"name":%q,"addr":"%s:50099"}`, nameA, nameA)
	bodyB := fmt.Sprintf(`{"name":%q,"addr":"%s:50099"}`, nameB, nameB)

	// Exhaust nameA's burst.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(bodyA))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.HandlePluginRegister(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	// Confirm nameA is now throttled.
	reqA := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(bodyA))
	reqA.Header.Set("Content-Type", "application/json")
	recA := httptest.NewRecorder()
	srv.HandlePluginRegister(recA, reqA)
	require.Equal(t, http.StatusTooManyRequests, recA.Code, "nameA should be throttled at attempt 11")

	// nameB has its own bucket and should still be wide open.
	reqB := httptest.NewRequest(http.MethodPost, "/internal/plugins/register", bytes.NewBufferString(bodyB))
	reqB.Header.Set("Content-Type", "application/json")
	recB := httptest.NewRecorder()
	srv.HandlePluginRegister(recB, reqB)
	assert.Equal(t, http.StatusOK, recB.Code,
		"throttling nameA must not bleed into nameB — buckets are per-name")
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

// ── Update Config (PUT /api/v1/plugins/{name}/config) ────────────────────
//
// These tests cover the optimistic-concurrency contract added in migration
// 017: If-Match parses to expectedVersion, ETag echoes the new
// config_version, 409 on mismatch, and the legacy no-header path stays
// working with a one-line WARN per plugin per second.

func TestHandleUpdatePluginConfig_NoIfMatch_LegacyPath_LogsWarn(t *testing.T) {
	buf, restore := captureSlog(t)
	defer restore()

	var observedExpected *int64
	mgr := &mockPluginManager{
		updateConfigFunc: func(_ context.Context, name string, config json.RawMessage, exp *int64) (*domain.PluginEntry, error) {
			observedExpected = exp
			return &domain.PluginEntry{Name: name, Config: config, ConfigVersion: 7}, nil
		},
	}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewRouter(srv)

	// Unique plugin name so the package-level rate limiter doesn't suppress
	// the WARN when other tests in this run hit /config first.
	name := "warn-no-ifmatch-" + uuid.NewString()[:8]
	req := httptest.NewRequest(http.MethodPut, "/api/v1/plugins/"+name+"/config",
		bytes.NewBufferString(`{"k":"v"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "legacy no-If-Match path still 200s")
	assert.Equal(t, `"7"`, rec.Header().Get("ETag"), "new config_version surfaces as ETag")
	assert.Nil(t, observedExpected, "no If-Match → nil expectedVersion passed through")
	assert.Contains(t, buf.String(), "without If-Match",
		"backward-compat path must log a WARN so operators see unsafe writers")
	assert.Contains(t, buf.String(), name)
}

func TestHandleUpdatePluginConfig_CorrectIfMatch_Bumps(t *testing.T) {
	var observedExpected *int64
	mgr := &mockPluginManager{
		updateConfigFunc: func(_ context.Context, name string, config json.RawMessage, exp *int64) (*domain.PluginEntry, error) {
			observedExpected = exp
			require.NotNil(t, exp)
			return &domain.PluginEntry{Name: name, Config: config, ConfigVersion: *exp + 1}, nil
		},
	}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/plugins/secrets/config",
		bytes.NewBufferString(`{"k":"v"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"3"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `"4"`, rec.Header().Get("ETag"), "ETag bumped by 1 on success")
	require.NotNil(t, observedExpected)
	assert.Equal(t, int64(3), *observedExpected)

	var entry domain.PluginEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entry))
	assert.Equal(t, int64(4), entry.ConfigVersion, "body carries new config_version too")
}

func TestHandleUpdatePluginConfig_StaleIfMatch_Returns409(t *testing.T) {
	mgr := &mockPluginManager{
		updateConfigFunc: func(_ context.Context, name string, _ json.RawMessage, _ *int64) (*domain.PluginEntry, error) {
			return &domain.PluginEntry{Name: name, ConfigVersion: 10}, domain.ErrConfigVersionMismatch
		},
	}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/plugins/secrets/config",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"3"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, `"10"`, rec.Header().Get("ETag"),
		"409 echoes the CURRENT version so the client can refetch & retry")
	assert.Contains(t, rec.Body.String(), "CONFIG_VERSION_MISMATCH")
	assert.Contains(t, rec.Body.String(), "expected 3")
	assert.Contains(t, rec.Body.String(), "current 10")
}

func TestHandleUpdatePluginConfig_MalformedIfMatch_Returns400(t *testing.T) {
	mgr := &mockPluginManager{
		updateConfigFunc: func(_ context.Context, _ string, _ json.RawMessage, _ *int64) (*domain.PluginEntry, error) {
			t.Fatal("manager should not be called when If-Match fails to parse")
			return nil, nil
		},
	}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/plugins/secrets/config",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"not-a-number"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleUpdatePluginConfig_ConcurrentSameIfMatch_OneWinsOneConflicts(t *testing.T) {
	// Emulate the real race: two writers GET version=5, both PUT with
	// If-Match: "5". The first to land bumps to 6; the second sees the
	// stale check fail and gets 409.
	var (
		mu       sync.Mutex
		current  = int64(5)
		winners  int
		conflict int
	)
	mgr := &mockPluginManager{
		updateConfigFunc: func(_ context.Context, name string, _ json.RawMessage, exp *int64) (*domain.PluginEntry, error) {
			require.NotNil(t, exp)
			mu.Lock()
			defer mu.Unlock()
			if *exp != current {
				return &domain.PluginEntry{Name: name, ConfigVersion: current}, domain.ErrConfigVersionMismatch
			}
			current++
			return &domain.PluginEntry{Name: name, ConfigVersion: current}, nil
		},
	}
	srv := &api.Server{PluginManager: mgr}
	router := api.NewRouter(srv)

	const writers = 2
	results := make(chan int, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/plugins/secrets/config",
				bytes.NewBufferString(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", `"5"`)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			results <- rec.Code
		}()
	}
	wg.Wait()
	close(results)
	for code := range results {
		switch code {
		case http.StatusOK:
			winners++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}

	assert.Equal(t, 1, winners, "exactly one writer commits")
	assert.Equal(t, 1, conflict, "the other writer sees 409")
}

func TestHandleGetPlugin_SetsETag(t *testing.T) {
	lister := &mockPluginLister{
		plugins: []domain.PluginEntry{
			{Name: "secrets", Status: domain.PluginStatusEnabled, ConfigVersion: 12},
		},
	}
	srv := &api.Server{PluginCatalog: lister, PluginManager: &mockPluginManager{}}
	router := api.NewRouter(srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/secrets", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, `"12"`, rec.Header().Get("ETag"),
		"GET surfaces ETag so clients can capture it once and reuse on PUT")
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
