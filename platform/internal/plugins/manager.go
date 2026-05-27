package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	"github.com/rat-data/rat/platform/internal/domain"
)

const (
	healthCheckTimeout = 5 * time.Second
	describeTimeout    = 10 * time.Second

	// DefaultReconcilerInterval is how often the reconciler compares the
	// in-memory registry to the catalog. Picked to be short enough that drift
	// is visible within a couple of minutes but long enough that the constant
	// listing of the catalog doesn't dominate Postgres traffic.
	DefaultReconcilerInterval = 60 * time.Second
)

// PluginCatalog is the persistence interface the Manager uses for plugin state.
// Implemented by postgres.PluginStore.
//
// UpdatePluginConfig honours optimistic concurrency: pass a non-nil
// expectedVersion to require the on-disk config_version to match (returns
// domain.ErrConfigVersionMismatch with the current entry on conflict). Pass
// nil for legacy last-write-wins behaviour (still bumps config_version).
type PluginCatalog interface {
	UpsertPlugin(ctx context.Context, entry domain.PluginEntry) (*domain.PluginEntry, error)
	ListPlugins(ctx context.Context, filter domain.PluginFilter) ([]domain.PluginEntry, error)
	GetPlugin(ctx context.Context, name string) (*domain.PluginEntry, error)
	DeletePlugin(ctx context.Context, name string) error
	UpdatePluginStatus(ctx context.Context, name string, status domain.PluginStatus, errMsg string) error
	UpdatePluginHealth(ctx context.Context, name string, healthy bool, errMsg string) error
	UpdatePluginConfig(ctx context.Context, name string, config json.RawMessage, expectedVersion *int64) (*domain.PluginEntry, error)
}

// PluginPolicyLister loads plugin policies for evaluation during registration.
type PluginPolicyLister interface {
	ListPluginPolicies(ctx context.Context) ([]domain.PluginPolicy, error)
}

// PluginSourceLister loads plugin sources for validation during registration.
type PluginSourceLister interface {
	ListPluginSources(ctx context.Context) ([]domain.PluginSource, error)
}

// Manager orchestrates the plugin lifecycle: registration, describe,
// enable/disable, and removal. It owns both the in-memory Registry and the
// persistent catalog (Postgres).
type Manager struct {
	// mu serialises the ENTIRE flow of Register, Enable, Disable, and Remove
	// — every method that mutates the catalog/registry pair. The registry has
	// its own mutex (it's separately thread-safe), but that mutex only covers
	// the in-memory bookkeeping; without an outer lock here two concurrent
	// registrations could each pass the in-memory step and then race in the
	// catalog UpsertPlugin, leaving the registry and catalog disagreeing about
	// which plugin "won".
	//
	// Trade-off: this serialises plugin startup. Cold-start cost is bounded
	// (N plugins × ~1s health check), which is fine. If startup latency
	// becomes a problem, a per-name lock would let DIFFERENT plugins register
	// in parallel — but the threat model (two registrations of the SAME name)
	// makes the coarse lock the right tool for v1.
	mu sync.Mutex

	registry   *Registry
	catalog    PluginCatalog
	policies   PluginPolicyLister
	sources    PluginSourceLister
	httpClient *http.Client
	edition    string

	// allowLoopback gates the SSRF guard's loopback exception. Read once at
	// construction from PLUGIN_ALLOW_LOOPBACK so behaviour is stable across the
	// process lifetime. Default false — only flip on for local-dev plugin
	// development outside Docker.
	allowLoopback bool

	// Callbacks fired when well-known capability plugins change.
	// Set by main.go to re-wire auth middleware, executor, etc. at runtime.
	OnAuthChanged        func(*Registry)
	OnExecutorChanged    func(*Registry)
	OnEnforcementChanged func(*Registry)
	OnCloudChanged       func(*Registry)
}

// NewManager creates a plugin Manager. Pass nil catalog for no persistence (tests).
//
// Reads PLUGIN_ALLOW_LOOPBACK once at construction; when set to "true" (or
// "1"/"yes"), the SSRF guard permits loopback plugin addresses (127.0.0.1,
// ::1, localhost). Default is false so production deployments reject SSRF
// attempts against the host loopback.
func NewManager(catalog PluginCatalog, edition string, httpClient *http.Client) *Manager {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Manager{
		registry:      NewRegistry(edition),
		catalog:       catalog,
		httpClient:    httpClient,
		edition:       edition,
		allowLoopback: loopbackOverrideFromEnv(),
	}
}

// loopbackOverrideFromEnv parses PLUGIN_ALLOW_LOOPBACK once at boot.
// Truthy values: "true", "1", "yes" (case-insensitive). Everything else, and
// an unset variable, evaluates to false.
func loopbackOverrideFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLUGIN_ALLOW_LOOPBACK"))) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// SetCatalog sets the persistent catalog. Called after Postgres is initialized.
func (m *Manager) SetCatalog(catalog PluginCatalog) {
	m.catalog = catalog
}

// Catalog returns the persistent catalog, or nil if not set.
func (m *Manager) Catalog() PluginCatalog {
	return m.catalog
}

// SetPolicies sets the policy store for registration-time enforcement.
func (m *Manager) SetPolicies(policies PluginPolicyLister) {
	m.policies = policies
}

// SetSources sets the source store for registration-time validation.
func (m *Manager) SetSources(sources PluginSourceLister) {
	m.sources = sources
}

// Registry returns the live in-memory Registry for read access.
func (m *Manager) Registry() *Registry {
	return m.registry
}

// LoadFromCatalog reconnects all previously-registered plugins on startup.
// Plugins that fail health-check are marked as error but kept in the registry
// so they can recover via the health loop.
func (m *Manager) LoadFromCatalog(ctx context.Context) error {
	if m.catalog == nil {
		return nil
	}

	entries, err := m.catalog.ListPlugins(ctx, domain.PluginFilter{})
	if err != nil {
		return fmt.Errorf("load plugins from catalog: %w", err)
	}

	for _, entry := range entries {
		if entry.Status == domain.PluginStatusDisabled {
			slog.Info("skipping disabled plugin from catalog", "name", entry.Name)
			continue
		}

		if err := m.reconnectPlugin(ctx, entry); err != nil {
			slog.Warn("failed to reconnect plugin from catalog, marking as error",
				"name", entry.Name, "error", err)
			if m.catalog != nil {
				_ = m.catalog.UpdatePluginStatus(ctx, entry.Name, domain.PluginStatusError, err.Error())
			}
		}
	}

	return nil
}

// Register handles the phone-home flow: health-check → describe → persist → register in memory.
// This is called when a plugin POSTs to /internal/plugins/register.
//
// The whole flow runs under the manager mutex — see the comment on Manager.mu
// for the threat model. We hold the lock across the outbound HTTP calls
// (health-check, describe) too: that's slightly wasteful when two DIFFERENT
// plugins register at the same time, but it keeps the contract simple and
// the cold-start cost is bounded.
func (m *Manager) Register(ctx context.Context, name, addr string) error {
	// 0. SSRF guard — reject loopback / link-local / multicast / unspecified
	// BEFORE we make any outbound calls. A hostile registrant must not be able
	// to point ratd at AWS IMDS (169.254.169.254) or localhost services and
	// learn anything from the response or the error. This validation is pure
	// and doesn't touch any shared state, so do it before taking the lock.
	if err := ValidateRegistrationAddress(addr, m.allowLoopback); err != nil {
		return fmt.Errorf("plugin %s address rejected: %w", name, err)
	}

	addr = EnsureScheme(addr)

	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Health check
	pluginClient := pluginv1connect.NewPluginServiceClient(m.httpClient, addr)
	healthCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	_, err := checkHealth(healthCtx, pluginClient)
	if err != nil {
		return fmt.Errorf("plugin %s health check failed: %w", name, err)
	}

	// 2. Describe (with fallback for legacy plugins)
	capabilities, eventTypes, version, descriptor := m.describePlugin(ctx, name, pluginClient)

	// 2.5. Evaluate policies (if store is available)
	if m.policies != nil {
		kind := inferKind(capabilities)
		if err := m.evaluatePolicies(ctx, name, kind); err != nil {
			return err
		}
	}

	// 3. Build the in-memory plugin
	p := &Plugin{
		Name:         name,
		Addr:         addr,
		Version:      version,
		Status:       domain.PluginStatusEnabled,
		Capabilities: capabilities,
		EventTypes:   eventTypes,
		Descriptor:   descriptor,
		PluginClient: pluginClient,
		HTTPClient:   m.httpClient,
		Token:        platformTokenFromDescriptor(descriptor),
	}

	// 4. Register in memory (validates capability conflicts)
	if err := m.registry.Register(p); err != nil {
		return fmt.Errorf("register plugin %s: %w", name, err)
	}

	// 5. Persist to catalog. If this fails we ROLL BACK the in-memory
	// registration so the next ratd restart and the live registry agree about
	// the plugin's existence. Previously the failure was logged + swallowed,
	// leaving the plugin live in memory but absent from the catalog — it would
	// silently vanish on the next restart.
	if m.catalog != nil {
		var descriptorJSON json.RawMessage
		if descriptor != nil {
			// Marshal the descriptor for storage.
			data, _ := json.Marshal(descriptor)
			descriptorJSON = data
		}

		entry := domain.PluginEntry{
			Name:       name,
			Kind:       inferKind(capabilities),
			Version:    version,
			Status:     domain.PluginStatusEnabled,
			Addr:       addr,
			Healthy:    true,
			Descriptor: descriptorJSON,
		}
		if _, err := m.catalog.UpsertPlugin(ctx, entry); err != nil {
			m.registry.Remove(name)
			return fmt.Errorf("persist plugin %s to catalog: %w", name, err)
		}
	}

	slog.Info("plugin registered", "name", name, "addr", addr,
		"capabilities", capabilities, "version", version)

	// 6. Fire callbacks for well-known capabilities.
	m.fireCallbacks(capabilities)

	return nil
}

// Enable re-enables a disabled plugin by reconnecting to it.
// Holds the manager mutex to keep registry/catalog mutations in lock-step
// with concurrent Register / Disable / Remove calls.
func (m *Manager) Enable(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.catalog == nil {
		return fmt.Errorf("no catalog available")
	}

	entry, err := m.catalog.GetPlugin(ctx, name)
	if err != nil {
		return fmt.Errorf("get plugin %s: %w", name, err)
	}
	if entry == nil {
		return fmt.Errorf("plugin %s not found", name)
	}

	if err := m.reconnectPlugin(ctx, *entry); err != nil {
		return fmt.Errorf("reconnect plugin %s: %w", name, err)
	}

	if err := m.catalog.UpdatePluginStatus(ctx, name, domain.PluginStatusEnabled, ""); err != nil {
		return fmt.Errorf("update status for %s: %w", name, err)
	}

	slog.Info("plugin enabled", "name", name)
	return nil
}

// Disable disconnects a plugin and marks it as disabled.
// Holds the manager mutex — same reason as Register.
func (m *Manager) Disable(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.registry.Remove(name)

	if m.catalog != nil {
		if err := m.catalog.UpdatePluginStatus(ctx, name, domain.PluginStatusDisabled, ""); err != nil {
			return fmt.Errorf("update status for %s: %w", name, err)
		}
	}

	slog.Info("plugin disabled", "name", name)
	return nil
}

// Remove deletes a plugin from both catalog and registry.
// Holds the manager mutex — same reason as Register.
func (m *Manager) Remove(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Capture capabilities before removal for callbacks.
	p := m.registry.Get(name)
	var caps []string
	if p != nil {
		caps = p.Capabilities
	}

	m.registry.Remove(name)

	if m.catalog != nil {
		if err := m.catalog.DeletePlugin(ctx, name); err != nil {
			return fmt.Errorf("delete plugin %s: %w", name, err)
		}
	}

	slog.Info("plugin removed", "name", name)
	m.fireCallbacks(caps)
	return nil
}

// UpdateConfig updates a plugin's configuration in the catalog.
//
// expectedVersion supports optimistic concurrency: when non-nil, the write
// is rejected with domain.ErrConfigVersionMismatch if the catalog row's
// config_version has moved on since the caller last read it. Pass nil for
// the legacy unsafe last-write-wins path (preserved for backward compat
// with clients that don't send the If-Match header).
func (m *Manager) UpdateConfig(ctx context.Context, name string, config json.RawMessage, expectedVersion *int64) (*domain.PluginEntry, error) {
	if m.catalog == nil {
		return nil, fmt.Errorf("no catalog available")
	}
	return m.catalog.UpdatePluginConfig(ctx, name, config, expectedVersion)
}

// NotifyHealthTransition is called by the HealthLoop when a plugin's status
// changes (enabled→error or error→enabled). It looks up the plugin's capabilities
// and fires the appropriate re-wiring callbacks (e.g., OnExecutorChanged).
func (m *Manager) NotifyHealthTransition(pluginName string) {
	p := m.registry.Get(pluginName)
	if p == nil {
		return
	}
	m.fireCallbacks(p.Capabilities)
}

// ── Internal helpers ───────────────────────────────────────────

// reconnectPlugin creates a client connection, health-checks, describes,
// and registers the plugin in memory. Used by LoadFromCatalog and Enable.
func (m *Manager) reconnectPlugin(ctx context.Context, entry domain.PluginEntry) error {
	addr := EnsureScheme(entry.Addr)
	pluginClient := pluginv1connect.NewPluginServiceClient(m.httpClient, addr)

	// Health check
	healthCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	_, err := checkHealth(healthCtx, pluginClient)
	if err != nil {
		// Register with error status so health loop can recover it later.
		p := &Plugin{
			Name:         entry.Name,
			Addr:         addr,
			Version:      entry.Version,
			Status:       domain.PluginStatusError,
			Error:        err.Error(),
			Capabilities: inferCapabilitiesFromEntry(entry),
			PluginClient: pluginClient,
			HTTPClient:   m.httpClient,
		}
		_ = m.registry.Register(p)
		return err
	}

	// Describe
	capabilities, eventTypes, version, descriptor := m.describePlugin(ctx, entry.Name, pluginClient)
	if version == "" {
		version = entry.Version
	}

	p := &Plugin{
		Name:         entry.Name,
		Addr:         addr,
		Version:      version,
		Status:       domain.PluginStatusEnabled,
		Capabilities: capabilities,
		EventTypes:   eventTypes,
		Descriptor:   descriptor,
		PluginClient: pluginClient,
		HTTPClient:   m.httpClient,
		Token:        platformTokenFromDescriptor(descriptor),
	}

	if err := m.registry.Register(p); err != nil {
		return err
	}

	m.fireCallbacks(capabilities)
	return nil
}

// platformTokenFromDescriptor extracts the platform token from a
// DescribeResponse, returning "" if the plugin didn't advertise one.
// A nil descriptor (legacy plugin that returned Unimplemented on
// Describe) yields "" so legacy plugins keep working with no header
// injection.
func platformTokenFromDescriptor(d *pluginv1.DescribeResponse) string {
	if d == nil {
		return ""
	}
	return d.PlatformToken
}

// describePlugin calls the Describe RPC. Falls back to name-based inference
// if the plugin returns Unimplemented.
func (m *Manager) describePlugin(ctx context.Context, name string, client pluginv1connect.PluginServiceClient) (
	capabilities []string, eventTypes []string, version string, descriptor *pluginv1.DescribeResponse,
) {
	descCtx, cancel := context.WithTimeout(ctx, describeTimeout)
	defer cancel()

	resp, err := client.Describe(descCtx, connect.NewRequest(&pluginv1.DescribeRequest{}))
	if err != nil {
		// Legacy plugin — infer capabilities from name.
		slog.Debug("plugin does not implement Describe, inferring capabilities",
			"name", name, "error", err)
		return InferCapabilitiesFromName(name), nil, "", nil
	}

	descriptor = resp.Msg
	return descriptor.Capabilities, descriptor.EventSubscriptions, descriptor.Version, descriptor
}

// inferKind determines the plugin kind from its capabilities.
func inferKind(capabilities []string) domain.PluginKind {
	for _, cap := range capabilities {
		switch cap {
		case CapAuth, CapEnforcement, CapCloud, CapSharing, CapExecutor:
			return domain.PluginKindPlatform
		}
	}
	return domain.PluginKindPlatform
}

// inferCapabilitiesFromEntry extracts capabilities from a stored catalog entry's
// descriptor JSON, falling back to name-based inference.
func inferCapabilitiesFromEntry(entry domain.PluginEntry) []string {
	if len(entry.Descriptor) > 0 {
		var desc struct {
			Capabilities []string `json:"capabilities"`
		}
		if json.Unmarshal(entry.Descriptor, &desc) == nil && len(desc.Capabilities) > 0 {
			return desc.Capabilities
		}
	}
	return InferCapabilitiesFromName(entry.Name)
}

// evaluatePolicies checks plugin allow/deny policies. Policies are evaluated
// in order — first match wins (like firewall rules). If no policies exist,
// registration is allowed (backward compatible).
func (m *Manager) evaluatePolicies(ctx context.Context, name string, kind domain.PluginKind) error {
	policies, err := m.policies.ListPluginPolicies(ctx)
	if err != nil {
		return fmt.Errorf("load plugin policies: %w", err)
	}

	// No policies = default allow (backward compat).
	if len(policies) == 0 {
		return nil
	}

	for _, policy := range policies {
		// Check name pattern match.
		matched, matchErr := path.Match(policy.Pattern, name)
		if matchErr != nil {
			slog.Warn("invalid policy pattern, skipping", "policy_id", policy.ID, "pattern", policy.Pattern, "error", matchErr)
			continue
		}
		if !matched {
			continue
		}

		// If policy is kind-scoped, also match the kind.
		if policy.Kind != "" && policy.Kind != string(kind) {
			continue
		}

		// First match wins.
		if policy.Rule == "deny" {
			return fmt.Errorf("plugin %q denied by policy %s (pattern: %s)", name, policy.ID, policy.Pattern)
		}
		// Rule is "allow" — permit registration.
		return nil
	}

	// No matching policy — default allow.
	return nil
}

// StartReconciler launches a goroutine that periodically compares the
// in-memory registry to the catalog and WARNs on any divergence. It does NOT
// auto-mutate either side — drift suggests a real bug (manual DB edit, lost
// rollback, etc.) and silent reconciliation would just mask it.
//
// The goroutine stops when ctx is cancelled. interval ≤ 0 means use
// DefaultReconcilerInterval.
//
// First tick fires immediately on startup so operators see drift right away
// instead of waiting one full interval.
func (m *Manager) StartReconciler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultReconcilerInterval
	}

	go func() {
		// Eager first tick — easier debugging than waiting one full interval.
		m.reconcileOnce(ctx)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.reconcileOnce(ctx)
			}
		}
	}()
}

// reconcileOnce runs a single comparison pass between the catalog and the
// in-memory registry. Exported indirectly through StartReconciler; broken out
// so tests can drive one tick deterministically.
func (m *Manager) reconcileOnce(ctx context.Context) {
	if m.catalog == nil {
		// Nothing to reconcile against — skip silently.
		return
	}

	entries, err := m.catalog.ListPlugins(ctx, domain.PluginFilter{})
	if err != nil {
		slog.Warn("reconciler: failed to list plugins from catalog", "error", err)
		return
	}

	catalogNames := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		catalogNames[e.Name] = struct{}{}
	}

	registryNames := make(map[string]struct{})
	for _, p := range m.registry.All() {
		registryNames[p.Name] = struct{}{}
	}

	var divergences int

	// Catalog-only: persisted but not live (might be disabled, which is fine).
	for _, e := range entries {
		if _, ok := registryNames[e.Name]; ok {
			continue
		}
		if e.Status == domain.PluginStatusDisabled {
			// Disabled plugins legitimately don't appear in the registry.
			continue
		}
		divergences++
		slog.Warn("reconciler: plugin in catalog but not in registry",
			"plugin", e.Name,
			"status", e.Status,
			"missing_from", "registry",
		)
	}

	// Registry-only: live but not persisted — the case the rollback fix tries
	// to prevent. If it ever appears here, a Register flow likely lost its
	// catalog write somehow (eg. partial restart, manual `DELETE FROM plugins`).
	for name := range registryNames {
		if _, ok := catalogNames[name]; ok {
			continue
		}
		divergences++
		slog.Warn("reconciler: plugin in registry but not in catalog",
			"plugin", name,
			"missing_from", "catalog",
		)
	}

	if divergences == 0 {
		slog.Debug("reconciler: registry and catalog agree",
			"plugins", len(registryNames))
	}
}

// fireCallbacks triggers the appropriate runtime re-wiring callbacks
// when a plugin with well-known capabilities is registered or removed.
func (m *Manager) fireCallbacks(capabilities []string) {
	for _, cap := range capabilities {
		switch cap {
		case CapAuth:
			if m.OnAuthChanged != nil {
				m.OnAuthChanged(m.registry)
			}
		case CapExecutor:
			if m.OnExecutorChanged != nil {
				m.OnExecutorChanged(m.registry)
			}
		case CapEnforcement:
			if m.OnEnforcementChanged != nil {
				m.OnEnforcementChanged(m.registry)
			}
		case CapCloud:
			if m.OnCloudChanged != nil {
				m.OnCloudChanged(m.registry)
			}
		}
	}
}
