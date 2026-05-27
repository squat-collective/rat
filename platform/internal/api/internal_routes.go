// Package api — single source of truth for the internal listener's route surface.
//
// ────────────────────────────────────────────────────────────────────────────
// TRUST MODEL (READ THIS BEFORE TOUCHING THIS FILE)
// ────────────────────────────────────────────────────────────────────────────
//
// The internal listener (default :8090) has NO authentication, NO CORS, NO
// per-IP rate limiting, and NO authorization. Every endpoint mounted by
// MountAllInternalRoutes trusts whatever calls it. That is intentional:
//
//   - The runner has no bearer token (it's a daemon, not a user).
//   - A plugin doesn't yet exist in the registry when it phone-homes, so
//     there's nothing to authenticate against.
//
// The trust boundary is the NETWORK, not the application. The operator MUST:
//
//   1. Bind the internal listener to loopback (127.0.0.1:8090) or to a
//      private network the public cannot reach (docker bridge, k8s pod
//      network, VPC private subnet).
//   2. NEVER publish port 8090 to the public internet or to the host with
//      a 0.0.0.0 binding when the host is internet-facing.
//   3. NEVER add a `ports: ["8090:8090"]` mapping in production compose
//      files — the reference docker-compose binds to 127.0.0.1:8090 on
//      the host as belt-and-braces; k8s relies on Service typing.
//
// If you are adding a NEW endpoint here, ask first: "would I be comfortable
// if a SSRF in another container could call this?" If the answer is no, the
// endpoint belongs on the PUBLIC router behind auth — not here.
//
// See ADR-019 (docs/adr/019-internal-listener-split.md) for the rationale
// and the full threat model. The public router (NewRouter) intentionally
// 404s every path mounted here so an attacker on the public listener cannot
// even discover that these routes exist.
// ────────────────────────────────────────────────────────────────────────────
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// InternalRouterConfig controls which internal endpoint groups
// MountAllInternalRoutes wires up. The zero value enables ALL groups —
// that is the safe production default. Tests that want to focus on a
// single endpoint can set the relevant flag and leave the rest false
// by constructing the struct explicitly.
//
// Callers should treat new boolean fields as "true by default" (zero =
// enabled) so a future addition cannot accidentally silence an endpoint
// that an operator was relying on.
type InternalRouterConfig struct {
	// RunCallbacks gates POST /api/v1/internal/runs/{runID}/status —
	// the runner's push-based terminal-status callback. Default: enabled.
	RunCallbacks bool
	// FailedMerges gates POST /api/v1/internal/failed-merges —
	// the runner's Phase-5 merge-failure audit callback. Default: enabled.
	FailedMerges bool
	// PluginPhoneHome gates the plugin self-registration endpoint.
	// Mounts BOTH the legacy /internal/plugins/register and the new
	// /api/v1/internal/plugins/register (alias). Default: enabled.
	PluginPhoneHome bool
	// Health gates GET /health, /health/live, /health/ready — mirrored
	// onto the internal listener so container probes pointed at the
	// internal port still work without round-tripping to 8080. Default:
	// enabled.
	Health bool
}

// DefaultInternalRouterConfig returns the safe production default: every
// endpoint group enabled. Exists mainly so callers can be explicit about
// "I want the defaults" without relying on zero-value semantics.
func DefaultInternalRouterConfig() InternalRouterConfig {
	return InternalRouterConfig{
		RunCallbacks:    true,
		FailedMerges:    true,
		PluginPhoneHome: true,
		Health:          true,
	}
}

// resolve normalises an InternalRouterConfig so the zero value enables
// every group. We do NOT want a forgotten field to silently turn an
// endpoint off — the safe default for "the operator didn't say anything"
// is "everything works".
//
// To DISABLE a group in a test, set its flag explicitly via a literal
// like InternalRouterConfig{RunCallbacks: true} (everything else false).
// Tests that want all groups should call DefaultInternalRouterConfig().
//
// The current shape collapses zero-value → all-enabled, which means
// MountAllInternalRoutes(r, InternalRouterConfig{}, srv) and
// MountAllInternalRoutes(r, DefaultInternalRouterConfig(), srv) behave
// identically. This is by design: an empty struct is the most common
// "I just want it to work" call shape.
func (c InternalRouterConfig) resolve() InternalRouterConfig {
	if !c.RunCallbacks && !c.FailedMerges && !c.PluginPhoneHome && !c.Health {
		return DefaultInternalRouterConfig()
	}
	return c
}

// MountAllInternalRoutes is the SINGLE entry point that cmd/ratd/main.go
// (via NewInternalRouter) calls to wire up every endpoint that lives on
// the internal listener. New internal endpoints MUST be added here — not
// scattered across feature files with their own mount calls — so this
// file remains the one place an operator (or auditor) looks to know the
// internal trust-boundary surface.
//
// Order is intentional and matches the in-the-wild call frequency from
// least → most chatty so a wedged endpoint earlier in the chain doesn't
// hide a later one in router introspection: health (probes), run-status
// callback (per-run), failed-merge callback (rare), plugin phone-home
// (boot only). chi's routing is order-independent so this is purely a
// cosmetic / readability choice.
func MountAllInternalRoutes(r chi.Router, cfg InternalRouterConfig, srv *Server) {
	cfg = cfg.resolve()

	if cfg.Health {
		mountInternalHealthRoutes(r, srv)
	}
	if cfg.RunCallbacks {
		MountInternalRoutes(r, srv)
	}
	if cfg.FailedMerges {
		MountInternalFailedMergesRoute(r, srv)
	}
	if cfg.PluginPhoneHome {
		// Mount both the legacy path and the new /api/v1/internal/
		// alias — see the alias comment on its handler below for the
		// deprecation contract.
		mountPluginPhoneHomeRoutes(r, srv)
	}
}

// mountInternalHealthRoutes mirrors the public /health endpoints onto the
// internal listener so a Docker/Kubernetes probe pointed at the internal
// port doesn't need to round-trip to 8080. The handlers are identical to
// the ones the public router uses; they're cheap and safe to expose.
func mountInternalHealthRoutes(r chi.Router, srv *Server) {
	r.Get("/health", srv.HandleHealth)
	r.Get("/health/live", srv.HandleHealthLive)
	r.Get("/health/ready", srv.HandleHealthReady)
}

// mountPluginPhoneHomeRoutes wires BOTH the legacy and the new plugin
// phone-home path so the URL-shape harmonisation (every internal route
// under /api/v1/internal/*) doesn't break the runner SDK or any plugin
// running the old SDK build.
//
// Contract:
//
//   - NEW: POST /api/v1/internal/plugins/register — the canonical path.
//     New SDK builds (sdk-go after this change) write here. All future
//     internal endpoints should follow this shape.
//   - DEPRECATED: POST /internal/plugins/register — still works, but
//     emits a rate-limited WARN log so operators can find stragglers
//     and plan the SDK upgrade. Will be removed in a future release;
//     no removal date is committed to here so the deprecation window
//     can be sized once we have telemetry on remaining old-SDK plugins.
//
// The handler short-circuits if PluginManager is nil so dev-mode without
// a plugin catalog still mounts cleanly (matches NewInternalRouter's
// previous behaviour).
func mountPluginPhoneHomeRoutes(r chi.Router, srv *Server) {
	if srv.PluginManager == nil {
		return
	}
	// Canonical / new path. Shares the same handler so behaviour is
	// identical — no double-counting in the rate-limiter (each path is
	// just a chi route to the SAME *Server method).
	r.Post("/api/v1/internal/plugins/register", srv.HandlePluginRegister)
	// Legacy alias. Wrapped so we can log a deprecation WARN exactly
	// once per request without polluting the canonical handler.
	r.Post("/internal/plugins/register", srv.handlePluginRegisterLegacyAlias)
}

// handlePluginRegisterLegacyAlias is the thin shim that fronts the legacy
// /internal/plugins/register path. It emits a rate-limited deprecation
// WARN (so a crashlooping plugin can't flood the log with one entry per
// retry) and then delegates to the canonical handler.
//
// The shim lives on *Server rather than as a free function so the warn
// limiter state (pluginRegisterDeprecationLimiter, declared in plugins.go
// adjacent to the existing warnLimiter) can be a package-scope var that
// survives across requests, and so the test seam matches the rest of the
// handler family.
func (s *Server) handlePluginRegisterLegacyAlias(w http.ResponseWriter, r *http.Request) {
	// Best-effort name extraction for the WARN log — the body is the
	// caller's JSON, so a malformed body just produces an "unknown"
	// plugin name in the log line. We DO NOT consume the body here
	// because HandlePluginRegister needs to decode it itself; the
	// limiter is keyed by the remote address as a coarse fallback so
	// we don't have to peek at the body before forwarding.
	if pluginRegisterDeprecationLimiter.allow(r.RemoteAddr) {
		slog.Warn(
			"deprecated: POST /internal/plugins/register — use POST /api/v1/internal/plugins/register; "+
				"the legacy path will be removed in a future release",
			"remote_addr", r.RemoteAddr,
			"user_agent", r.Header.Get("User-Agent"),
		)
	}
	s.HandlePluginRegister(w, r)
}
