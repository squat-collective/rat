package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// PluginManager defines the interface the API layer uses for plugin lifecycle operations.
// Implemented by plugins.Manager.
//
// UpdateConfig propagates the optimistic-concurrency expectedVersion: callers
// pass nil for legacy last-write-wins, or a non-nil pointer to require the
// current config_version to match (returns domain.ErrConfigVersionMismatch
// on conflict).
type PluginManager interface {
	Register(ctx context.Context, name, addr string) error
	Enable(ctx context.Context, name string) error
	Disable(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	UpdateConfig(ctx context.Context, name string, config json.RawMessage, expectedVersion *int64) (*domain.PluginEntry, error)
}

// PluginLister lists plugins from the catalog (read-only queries).
type PluginLister interface {
	ListPlugins(ctx context.Context, filter domain.PluginFilter) ([]domain.PluginEntry, error)
	GetPlugin(ctx context.Context, name string) (*domain.PluginEntry, error)
}

// PluginSourceStore manages plugin source repositories.
type PluginSourceStore interface {
	ListPluginSources(ctx context.Context) ([]domain.PluginSource, error)
	CreatePluginSource(ctx context.Context, src domain.PluginSource) (*domain.PluginSource, error)
	DeletePluginSource(ctx context.Context, id uuid.UUID) error
}

// PluginPolicyStore manages plugin allow/deny policies.
type PluginPolicyStore interface {
	ListPluginPolicies(ctx context.Context) ([]domain.PluginPolicy, error)
	CreatePluginPolicy(ctx context.Context, policy domain.PluginPolicy) (*domain.PluginPolicy, error)
	DeletePluginPolicy(ctx context.Context, id uuid.UUID) error
}

// pluginConfigWarnLimiter rate-limits the "PUT /config without If-Match" WARN
// to once per plugin name per second so a busy client (e.g. the secrets plugin
// polling its own config) cannot flood the logs. Module-scope so it survives
// per-request handler invocations.
var pluginConfigWarnLimiter = newWarnLimiter(time.Second)

// pluginRegisterDeprecationLimiter rate-limits the "you hit the legacy
// /internal/plugins/register path" WARN to one log line per remote-addr
// per minute. A plugin in a phone-home retry storm on the old SDK could
// otherwise burst a WARN per attempt; one per minute is enough to make
// the deprecation visible without drowning the log.
//
// Keyed on the request's RemoteAddr rather than the plugin name (which we
// can't read without consuming the body before delegating). That gives
// roughly one entry per misbehaving plugin instance, which is what we
// want for operator awareness.
var pluginRegisterDeprecationLimiter = newWarnLimiter(time.Minute)

// pluginRegisterLimiter rate-limits phone-home (POST /internal/plugins/register)
// per plugin name. The /internal listener has no auth (it's bind-localhost),
// so a runaway plugin process — or a compromised one in a crashloop — could
// otherwise spam ratd with register attempts at full HTTP throughput. The
// limiter caps each name to a token bucket that refills at 10/min steady-state
// with a burst of 10, returning 429 + Retry-After above that.
//
// Module-scope so the bucket state survives across HandlePluginRegister calls.
// Bucket entries idle for >10 minutes are swept lazily during register calls
// to bound memory under churn (no separate goroutine — keeps the test path
// deterministic and avoids a process-lifetime daemon for an infrequent path).
var pluginRegisterLimiter = newRegisterLimiter(registerLimiterConfig{
	RatePerMinute: 10,
	Burst:         10,
	IdleTTL:       10 * time.Minute,
})

type warnLimiter struct {
	window time.Duration
	mu     sync.Mutex
	last   map[string]time.Time
}

func newWarnLimiter(window time.Duration) *warnLimiter {
	return &warnLimiter{window: window, last: make(map[string]time.Time)}
}

// allow reports whether key has not been warned within the current window.
// On allow it advances the timestamp atomically so concurrent callers cannot
// both pass the same gate.
func (l *warnLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if prev, ok := l.last[key]; ok && now.Sub(prev) < l.window {
		return false
	}
	l.last[key] = now
	return true
}

// parseIfMatch extracts a non-negative int64 from the If-Match header, accepting
// the standard quoted form (`"42"`), the W/ weak-validator form, or a bare
// integer. Returns (nil, false, nil) when the header is absent (legacy path),
// (nil, true, err) when the header is present but unparseable so the caller
// can reject with 400.
func parseIfMatch(h string) (*int64, bool, error) {
	if h == "" {
		return nil, false, nil
	}
	raw := strings.TrimSpace(h)
	raw = strings.TrimPrefix(raw, "W/")
	raw = strings.Trim(raw, `"`)
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, true, fmt.Errorf("If-Match must be a quoted integer (config_version), got %q", h)
	}
	if v < 0 {
		return nil, true, fmt.Errorf("If-Match must be non-negative, got %d", v)
	}
	return &v, true, nil
}

// setETag sets the ETag response header from a config_version. The value is
// quoted per RFC 7232 so clients can echo it back verbatim in If-Match.
func setETag(w http.ResponseWriter, configVersion int64) {
	w.Header().Set("ETag", `"`+strconv.FormatInt(configVersion, 10)+`"`)
}

// MountPluginInternalRoutes mounts the phone-home endpoint outside the auth middleware.
// Called by internal services and plugins to self-register.
func MountPluginInternalRoutes(r chi.Router, srv *Server) {
	r.Post("/internal/plugins/register", srv.HandlePluginRegister)
}

// MountPluginRoutes mounts the authenticated plugin management endpoints.
func MountPluginRoutes(r chi.Router, srv *Server) {
	r.Get("/plugins", srv.HandleListPlugins)
	r.Get("/plugins/{name}", srv.HandleGetPlugin)
	r.Put("/plugins/{name}/enable", srv.HandleEnablePlugin)
	r.Put("/plugins/{name}/disable", srv.HandleDisablePlugin)
	r.Put("/plugins/{name}/config", srv.HandleUpdatePluginConfig)
	r.Delete("/plugins/{name}", srv.HandleDeletePlugin)
}

// HandlePluginRegister handles POST /internal/plugins/register (phone-home).
// Body: {"name": "...", "addr": "..."}
func (srv *Server) HandlePluginRegister(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Name string `json:"name"`
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.Addr == "" {
		errorJSON(w, "name and addr are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(body.Name) {
		errorJSON(w, "name must be a lowercase slug", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Cap per-name register frequency BEFORE doing the (potentially slow,
	// SSRF-validating) Register call. If a plugin is in a crashloop or has a
	// runaway retry, we want to absorb the storm here, not push it down into
	// the plugin manager / health checker.
	if res := pluginRegisterLimiter.allow(body.Name, time.Now()); !res.Allowed {
		w.Header().Set("Retry-After", strconv.FormatInt(res.RetryAfterSecs, 10))
		errorJSON(w, "too many register attempts for this plugin name", "RESOURCE_EXHAUSTED", http.StatusTooManyRequests)
		return
	}

	if err := srv.PluginManager.Register(r.Context(), body.Name, body.Addr); err != nil {
		// SSRF guard rejected the address — that's a client problem, not a
		// server problem, so return 400 with the validator's message so the
		// caller can see why their address was unacceptable.
		if errors.Is(err, plugins.ErrAddressRejected) {
			errorJSON(w, err.Error(), "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		internalError(w, "plugin registration failed", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "registered",
		"name":   body.Name,
	})
}

// HandleListPlugins handles GET /api/v1/plugins.
func (srv *Server) HandleListPlugins(w http.ResponseWriter, r *http.Request) {
	if srv.PluginCatalog == nil {
		writeJSON(w, http.StatusOK, []domain.PluginEntry{})
		return
	}

	filter := domain.PluginFilter{
		Status: r.URL.Query().Get("status"),
		Kind:   r.URL.Query().Get("kind"),
	}

	plugins, err := srv.PluginCatalog.ListPlugins(r.Context(), filter)
	if err != nil {
		internalError(w, "failed to list plugins", err)
		return
	}

	writeJSON(w, http.StatusOK, plugins)
}

// HandleGetPlugin handles GET /api/v1/plugins/{name}.
//
// Always sets ETag to the plugin's current config_version so clients can
// capture it once with GET and reuse it on a subsequent PUT /config without
// reading the body.
func (srv *Server) HandleGetPlugin(w http.ResponseWriter, r *http.Request) {
	if srv.PluginCatalog == nil {
		errorJSON(w, "plugin catalog not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")
	plugin, err := srv.PluginCatalog.GetPlugin(r.Context(), name)
	if err != nil {
		internalError(w, "failed to get plugin", err)
		return
	}
	if plugin == nil {
		errorJSON(w, "plugin not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	setETag(w, plugin.ConfigVersion)
	writeJSON(w, http.StatusOK, plugin)
}

// HandleEnablePlugin handles PUT /api/v1/plugins/{name}/enable.
func (srv *Server) HandleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")
	if err := srv.PluginManager.Enable(r.Context(), name); err != nil {
		internalError(w, "failed to enable plugin", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "name": name})
}

// HandleDisablePlugin handles PUT /api/v1/plugins/{name}/disable.
func (srv *Server) HandleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")
	if err := srv.PluginManager.Disable(r.Context(), name); err != nil {
		internalError(w, "failed to disable plugin", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled", "name": name})
}

// HandleUpdatePluginConfig handles PUT /api/v1/plugins/{name}/config.
//
// Optimistic concurrency contract:
//   - If-Match present and parseable → require config_version to match;
//     return 409 Conflict with the current ETag on mismatch.
//   - If-Match absent → legacy last-write-wins; logs a rate-limited WARN so
//     operators can find stragglers before we tighten the contract.
//   - If-Match present but malformed → 400 Bad Request (client bug).
//
// The response always carries the NEW config_version in both the JSON body
// and the ETag header so the client has the value ready for its next write.
func (srv *Server) HandleUpdatePluginConfig(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")

	expectedVersion, hadIfMatch, ifMatchErr := parseIfMatch(r.Header.Get("If-Match"))
	if ifMatchErr != nil {
		errorJSON(w, ifMatchErr.Error(), "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !hadIfMatch && pluginConfigWarnLimiter.allow(name) {
		// Loud enough that operators notice unsafe writers, quiet enough
		// that a busy poller can't spam the log. Drop to Debug once all
		// in-tree clients send If-Match.
		slog.Warn("plugin config update without If-Match (last-write-wins, unsafe)",
			"plugin", name)
	}

	var config json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	entry, err := srv.PluginManager.UpdateConfig(r.Context(), name, config, expectedVersion)
	if errors.Is(err, domain.ErrConfigVersionMismatch) {
		// entry holds the CURRENT row (the wrapper returns it alongside the
		// sentinel) so we can echo the live version in both the body and
		// the ETag header for the client's next attempt.
		setETag(w, entry.ConfigVersion)
		msg := fmt.Sprintf("config_version mismatch; expected %d, current %d",
			*expectedVersion, entry.ConfigVersion)
		errorJSON(w, msg, "CONFIG_VERSION_MISMATCH", http.StatusConflict)
		return
	}
	if err != nil {
		internalError(w, "failed to update plugin config", err)
		return
	}
	if entry == nil {
		errorJSON(w, "plugin not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	setETag(w, entry.ConfigVersion)
	writeJSON(w, http.StatusOK, entry)
}

// HandleDeletePlugin handles DELETE /api/v1/plugins/{name}.
func (srv *Server) HandleDeletePlugin(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")
	if err := srv.PluginManager.Remove(r.Context(), name); err != nil {
		internalError(w, "failed to delete plugin", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Plugin Sources ─────────────────────────────────────────────────────────

// MountPluginSourceRoutes mounts the authenticated plugin source endpoints.
func MountPluginSourceRoutes(r chi.Router, srv *Server) {
	r.Get("/plugin-sources", srv.HandleListPluginSources)
	r.Post("/plugin-sources", srv.HandleCreatePluginSource)
	r.Delete("/plugin-sources/{sourceID}", srv.HandleDeletePluginSource)
}

// HandleListPluginSources handles GET /api/v1/plugin-sources.
func (srv *Server) HandleListPluginSources(w http.ResponseWriter, r *http.Request) {
	if srv.PluginSources == nil {
		writeJSON(w, http.StatusOK, []domain.PluginSource{})
		return
	}

	sources, err := srv.PluginSources.ListPluginSources(r.Context())
	if err != nil {
		internalError(w, "failed to list plugin sources", err)
		return
	}

	writeJSON(w, http.StatusOK, sources)
}

// HandleCreatePluginSource handles POST /api/v1/plugin-sources.
// Body: {"type": "oci"|"local"|"git", "url": "...", "trusted"?: bool, "enabled"?: bool}
func (srv *Server) HandleCreatePluginSource(w http.ResponseWriter, r *http.Request) {
	if srv.PluginSources == nil {
		errorJSON(w, "plugin sources not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Type    string `json:"type"`
		URL     string `json:"url"`
		Trusted *bool  `json:"trusted"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if body.Type == "" || body.URL == "" {
		errorJSON(w, "type and url are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	src := domain.PluginSource{
		ID:      uuid.New(),
		Type:    body.Type,
		URL:     body.URL,
		Trusted: body.Trusted != nil && *body.Trusted,
		Enabled: body.Enabled == nil || *body.Enabled, // default true
	}

	created, err := srv.PluginSources.CreatePluginSource(r.Context(), src)
	if err != nil {
		internalError(w, "failed to create plugin source", err)
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// HandleDeletePluginSource handles DELETE /api/v1/plugin-sources/{sourceID}.
func (srv *Server) HandleDeletePluginSource(w http.ResponseWriter, r *http.Request) {
	if srv.PluginSources == nil {
		errorJSON(w, "plugin sources not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	idStr := chi.URLParam(r, "sourceID")
	id, err := uuid.Parse(idStr)
	if err != nil {
		errorJSON(w, "invalid source ID", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if err := srv.PluginSources.DeletePluginSource(r.Context(), id); err != nil {
		internalError(w, "failed to delete plugin source", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Plugin Policies ────────────────────────────────────────────────────────

// MountPluginPolicyRoutes mounts the authenticated plugin policy endpoints.
func MountPluginPolicyRoutes(r chi.Router, srv *Server) {
	r.Get("/plugin-policies", srv.HandleListPluginPolicies)
	r.Post("/plugin-policies", srv.HandleCreatePluginPolicy)
	r.Delete("/plugin-policies/{policyID}", srv.HandleDeletePluginPolicy)
}

// HandleListPluginPolicies handles GET /api/v1/plugin-policies.
func (srv *Server) HandleListPluginPolicies(w http.ResponseWriter, r *http.Request) {
	if srv.PluginPolicies == nil {
		writeJSON(w, http.StatusOK, []domain.PluginPolicy{})
		return
	}

	policies, err := srv.PluginPolicies.ListPluginPolicies(r.Context())
	if err != nil {
		internalError(w, "failed to list plugin policies", err)
		return
	}

	writeJSON(w, http.StatusOK, policies)
}

// HandleCreatePluginPolicy handles POST /api/v1/plugin-policies.
// Body: {"rule": "allow"|"deny", "pattern": "...", "kind"?: "platform"|"runner"|"portal"}
func (srv *Server) HandleCreatePluginPolicy(w http.ResponseWriter, r *http.Request) {
	if srv.PluginPolicies == nil {
		errorJSON(w, "plugin policies not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Rule    string `json:"rule"`
		Pattern string `json:"pattern"`
		Kind    string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if body.Rule == "" || body.Pattern == "" {
		errorJSON(w, "rule and pattern are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if body.Rule != "allow" && body.Rule != "deny" {
		errorJSON(w, "rule must be 'allow' or 'deny'", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	policy := domain.PluginPolicy{
		ID:      uuid.New(),
		Rule:    body.Rule,
		Pattern: body.Pattern,
		Kind:    body.Kind,
	}

	created, err := srv.PluginPolicies.CreatePluginPolicy(r.Context(), policy)
	if err != nil {
		internalError(w, "failed to create plugin policy", err)
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// HandleDeletePluginPolicy handles DELETE /api/v1/plugin-policies/{policyID}.
func (srv *Server) HandleDeletePluginPolicy(w http.ResponseWriter, r *http.Request) {
	if srv.PluginPolicies == nil {
		errorJSON(w, "plugin policies not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	idStr := chi.URLParam(r, "policyID")
	id, err := uuid.Parse(idStr)
	if err != nil {
		errorJSON(w, "invalid policy ID", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if err := srv.PluginPolicies.DeletePluginPolicy(r.Context(), id); err != nil {
		internalError(w, "failed to delete plugin policy", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
