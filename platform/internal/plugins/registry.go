package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	connect "connectrpc.com/connect"
	cloudv1 "github.com/rat-data/rat/platform/gen/cloud/v1"
	"github.com/rat-data/rat/platform/gen/cloud/v1/cloudv1connect"
	pluginv1 "github.com/rat-data/rat/platform/gen/plugin/v1"
	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	"github.com/rat-data/rat/platform/internal/auth"
	"github.com/rat-data/rat/platform/internal/domain"
)

// Per-RPC timeout on the auth plugin's Authenticate call — a slow/hung
// auth plugin must NOT block every request to ratd. On timeout we fail
// closed (401).
const authPluginRPCTimeout = 5 * time.Second

// Well-known capability names. A plugin declares these in DescribeResponse.capabilities.
const (
	CapAuth        = "auth"
	CapExecutor    = "executor"
	CapSharing     = "sharing"
	CapEnforcement = "enforcement"
	CapCloud       = "cloud"
)

// Plugin represents a live, in-memory plugin connection in the registry.
type Plugin struct {
	Name         string
	Addr         string
	Version      string
	Error        string
	Status       domain.PluginStatus
	Capabilities []string
	EventTypes   []string
	Descriptor   *pluginv1.DescribeResponse
	PluginClient pluginv1connect.PluginServiceClient
	HTTPClient   connect.HTTPClient
	// Token is the plugin's per-startup random shared secret, advertised
	// in DescribeResponse.platform_token. When non-empty, the plugin
	// proxy injects it as X-RAT-Plugin-Token on every forwarded request,
	// and the plugin's middleware rejects any inbound request that lacks
	// the matching value. Empty string means the plugin opted out (legacy
	// behaviour — no header injected, no validation expected).
	Token string
}

// HasCapability returns true if the plugin declares the given capability.
func (p *Plugin) HasCapability(capName string) bool {
	for _, c := range p.Capabilities {
		if c == capName {
			return true
		}
	}
	return false
}

// Registry is the open, dynamic plugin registry.
// It replaces the old slot-based Registry from loader.go.
// Thread-safe — all mutations go through the exported methods.
type Registry struct {
	mu      sync.RWMutex
	edition string
	plugins map[string]*Plugin

	// Capability indexes: store the plugin NAME (not pointer) for the
	// well-known capability slots. Lookup goes through the plugins map.
	authPlugin        string
	executorPlugin    string
	sharingPlugin     string
	enforcementPlugin string
	cloudPlugin       string
}

// NewRegistry creates an empty plugin registry for the given edition.
func NewRegistry(edition string) *Registry {
	return &Registry{
		edition: edition,
		plugins: make(map[string]*Plugin),
	}
}

// Register adds or replaces a plugin in the registry.
// Returns an error if the plugin claims a capability already held by another plugin.
func (r *Registry) Register(p *Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for capability conflicts.
	for _, cap := range p.Capabilities {
		holder := r.capabilityHolder(cap)
		if holder != "" && holder != p.Name {
			return fmt.Errorf("capability %q already held by plugin %q", cap, holder)
		}
	}

	// If replacing an existing plugin, clear its old capability indexes.
	if old, ok := r.plugins[p.Name]; ok {
		r.clearCapabilities(old)
	}

	r.plugins[p.Name] = p

	// Update capability indexes.
	for _, cap := range p.Capabilities {
		r.setCapabilityHolder(cap, p.Name)
	}

	return nil
}

// Remove deletes a plugin from the registry and clears its capability indexes.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, ok := r.plugins[name]; ok {
		r.clearCapabilities(p)
		delete(r.plugins, name)
	}
}

// Get returns a plugin by name, or nil if not found.
func (r *Registry) Get(name string) *Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[name]
}

// All returns a snapshot of all registered plugins.
func (r *Registry) All() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		result = append(result, p)
	}
	return result
}

// ByCapability returns the plugin that holds the given capability, or nil.
func (r *Registry) ByCapability(capName string) *Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name := r.capabilityHolder(capName)
	if name == "" {
		return nil
	}
	return r.plugins[name]
}

// ── Backward-compatible helpers ────────────────────────────────
// These match the old Registry interface so existing code continues to work.

// AuthEnabled returns true if an auth plugin is registered and enabled.
func (r *Registry) AuthEnabled() bool {
	p := r.ByCapability(CapAuth)
	return p != nil && p.Status == domain.PluginStatusEnabled
}

// ExecutorEnabled returns true if an executor plugin is registered and enabled.
func (r *Registry) ExecutorEnabled() bool {
	p := r.ByCapability(CapExecutor)
	return p != nil && p.Status == domain.PluginStatusEnabled
}

// GetExecutorAddr returns the executor plugin's address, or empty string.
func (r *Registry) GetExecutorAddr() string {
	p := r.ByCapability(CapExecutor)
	if p == nil {
		return ""
	}
	return p.Addr
}

// SharingEnabled returns true if a sharing plugin is registered and enabled.
func (r *Registry) SharingEnabled() bool {
	p := r.ByCapability(CapSharing)
	return p != nil && p.Status == domain.PluginStatusEnabled
}

// EnforcementEnabled returns true if an enforcement plugin is registered and enabled.
func (r *Registry) EnforcementEnabled() bool {
	p := r.ByCapability(CapEnforcement)
	return p != nil && p.Status == domain.PluginStatusEnabled
}

// CloudEnabled returns true if a cloud plugin is registered and enabled.
func (r *Registry) CloudEnabled() bool {
	p := r.ByCapability(CapCloud)
	return p != nil && p.Status == domain.PluginStatusEnabled
}

// Features returns the dynamic feature set based on registered plugins.
// The Plugins map is empty in Community Edition — runner plugins are exposed
// via the dedicated GET /api/v1/runner/plugins endpoint instead.
func (r *Registry) Features() domain.Features {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return domain.Features{
		Edition:    r.edition,
		Namespaces: r.edition != "community",
		MultiUser:  r.authPlugin != "",
		Plugins:    map[string]domain.PluginFeature{},
	}
}

// ── Auth (calls into auth plugin via core proto) ───────────────

// Authenticate delegates to the auth plugin's Authenticate RPC.
func (r *Registry) Authenticate(ctx context.Context, token string) (*pluginv1.AuthenticateResponse, error) {
	p := r.ByCapability(CapAuth)
	if p == nil || p.PluginClient == nil {
		return nil, fmt.Errorf("auth plugin not loaded")
	}

	resp, err := p.PluginClient.Authenticate(ctx, connect.NewRequest(&pluginv1.AuthenticateRequest{
		Token: token,
	}))
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	return resp.Msg, nil
}

// AuthMiddleware returns HTTP middleware that delegates authentication to the auth plugin.
// If no auth plugin is loaded, returns auth.Noop().
func (r *Registry) AuthMiddleware() func(http.Handler) http.Handler {
	if !r.AuthEnabled() {
		return auth.Noop()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			token := extractBearerToken(req)
			if token == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "missing or invalid Authorization header",
				})
				return
			}

			authCtx, cancel := context.WithTimeout(req.Context(), authPluginRPCTimeout)
			defer cancel()

			resp, err := r.Authenticate(authCtx, token)
			if err != nil {
				// A slow/hung auth plugin must not stall every request to ratd.
				// On deadline-exceeded we fail closed (401) and log WARN with
				// the plugin name; the plugin name is intentionally not exposed
				// in the response to avoid leaking deployment details.
				if isDeadlineExceeded(err) {
					pluginName := ""
					if p := r.ByCapability(CapAuth); p != nil {
						pluginName = p.Name
					}
					slog.Warn("auth plugin Authenticate timed out",
						"plugin", pluginName,
						"timeout", authPluginRPCTimeout,
					)
					writeJSON(w, http.StatusUnauthorized, map[string]string{
						"error": "authentication timed out",
					})
					return
				}
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "authentication failed",
				})
				return
			}

			if !resp.Authenticated {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": resp.ErrorMessage,
				})
				return
			}

			// Convert proto UserIdentity to domain UserIdentity and store in context.
			user := protoUserToDomain(resp.User)
			ctx := ContextWithUser(req.Context(), user)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// isDeadlineExceeded reports whether err is (or wraps) a context-deadline
// timeout. ConnectRPC surfaces deadline-exceeded as connect.CodeDeadlineExceeded
// but the wrapped context error still satisfies errors.Is, so we check both.
func isDeadlineExceeded(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if connectErr := new(connect.Error); errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeDeadlineExceeded
	}
	return false
}

// ── Enforcement (calls into enforcement plugin via core proto) ──

// CanAccess delegates to the enforcement plugin's Authorize RPC.
func (r *Registry) CanAccess(ctx context.Context, userID, resourceType, resourceID, action string) (bool, error) {
	p := r.ByCapability(CapEnforcement)
	if p == nil || p.PluginClient == nil {
		return false, fmt.Errorf("enforcement plugin not loaded")
	}

	resp, err := p.PluginClient.Authorize(ctx, connect.NewRequest(&pluginv1.AuthorizeRequest{
		UserId:       userID,
		ResourceType: resourceType,
		ResourceId:   resourceID,
		Action:       action,
	}))
	if err != nil {
		return false, fmt.Errorf("authorize: %w", err)
	}
	return resp.Msg.Allowed, nil
}

// ── Cloud (calls into cloud plugin via cloudv1 proto) ──────────

// GetCredentials delegates to the cloud plugin's GetCredentials RPC and
// converts the wire response to a platform domain type.
//
// Returns an error when no cloud plugin is loaded, when the plugin call
// fails, or when the plugin's underlying HTTP client has not been wired
// (defensive — should not happen in production where Manager.Register
// always sets HTTPClient).
//
// Expiry is converted from Unix seconds to time.Time; a zero expires_at
// translates to a zero time.Time (no expiry hint), which callers should
// treat as "refresh on first use".
func (r *Registry) GetCredentials(ctx context.Context, userID, namespace string) (*domain.CloudCredentials, error) {
	p := r.ByCapability(CapCloud)
	if p == nil {
		return nil, fmt.Errorf("cloud plugin not loaded")
	}
	if p.HTTPClient == nil {
		return nil, fmt.Errorf("cloud plugin %q has no HTTP client wired", p.Name)
	}

	client := cloudv1connect.NewCloudServiceClient(p.HTTPClient, EnsureScheme(p.Addr))
	resp, err := client.GetCredentials(ctx, connect.NewRequest(&cloudv1.GetCredentialsRequest{
		UserId:    userID,
		Namespace: namespace,
	}))
	if err != nil {
		return nil, fmt.Errorf("cloud plugin %q GetCredentials: %w", p.Name, err)
	}
	if resp == nil || resp.Msg == nil {
		return nil, fmt.Errorf("cloud plugin %q returned empty response", p.Name)
	}

	var expiry time.Time
	if resp.Msg.ExpiresAt > 0 {
		expiry = time.Unix(resp.Msg.ExpiresAt, 0).UTC()
	}
	return &domain.CloudCredentials{
		AccessKey:    resp.Msg.AccessKey,
		SecretKey:    resp.Msg.SecretKey,
		SessionToken: resp.Msg.SessionToken,
		Region:       resp.Msg.Region,
		Expiry:       expiry,
	}, nil
}

// ── Internal helpers ───────────────────────────────────────────

func (r *Registry) capabilityHolder(capName string) string {
	switch capName {
	case CapAuth:
		return r.authPlugin
	case CapExecutor:
		return r.executorPlugin
	case CapSharing:
		return r.sharingPlugin
	case CapEnforcement:
		return r.enforcementPlugin
	case CapCloud:
		return r.cloudPlugin
	default:
		return ""
	}
}

func (r *Registry) setCapabilityHolder(capName, name string) {
	switch capName {
	case CapAuth:
		r.authPlugin = name
	case CapExecutor:
		r.executorPlugin = name
	case CapSharing:
		r.sharingPlugin = name
	case CapEnforcement:
		r.enforcementPlugin = name
	case CapCloud:
		r.cloudPlugin = name
	}
}

func (r *Registry) clearCapabilities(p *Plugin) {
	for _, cap := range p.Capabilities {
		if r.capabilityHolder(cap) == p.Name {
			r.setCapabilityHolder(cap, "")
		}
	}
}

// protoUserToDomain converts a proto UserIdentity to a domain UserIdentity.
func protoUserToDomain(u *pluginv1.UserIdentity) *domain.UserIdentity {
	if u == nil {
		return nil
	}
	return &domain.UserIdentity{
		UserID:      u.UserId,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Roles:       u.Roles,
	}
}

// EnsureScheme adds http:// if the address has no scheme.
// Exported for use by the plugin proxy.
func EnsureScheme(addr string) string {
	if addr != "" && !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		return "http://" + addr
	}
	return addr
}

// checkHealth calls PluginService.HealthCheck with a timeout.
func checkHealth(ctx context.Context, client healthChecker) (string, error) {
	resp, err := client.HealthCheck(ctx, connect.NewRequest(&pluginv1.HealthCheckRequest{}))
	if err != nil {
		return "", fmt.Errorf("health check failed: %w", err)
	}
	if resp.Msg.Status != pluginv1.Status_STATUS_SERVING {
		return "", fmt.Errorf("plugin not serving: %s", resp.Msg.Message)
	}
	return resp.Msg.Message, nil
}

// healthChecker is the interface for plugin health checks.
type healthChecker interface {
	HealthCheck(context.Context, *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error)
}

// InferCapabilitiesFromName provides backward-compatible capability inference
// for legacy plugins that don't implement Describe. Maps well-known plugin
// names to their expected capabilities.
func InferCapabilitiesFromName(name string) []string {
	switch name {
	case "auth":
		return []string{CapAuth}
	case "executor":
		return []string{CapExecutor}
	case "sharing":
		return []string{CapSharing}
	case "enforcement":
		return []string{CapEnforcement}
	case "cloud":
		return []string{CapCloud}
	default:
		// Unknown plugins get no capabilities — they can still register routes.
		slog.Debug("no inferred capabilities for plugin", "name", name)
		return nil
	}
}
