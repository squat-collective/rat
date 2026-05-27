package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// statusClientClosedRequest is nginx's convention for "the client gave
// up while we were still trying to forward". Distinguishes a real
// upstream failure (502) from a "user hit STOP / browser disconnected"
// scenario — those happen all the time on long SSE streams from chat
// and shouldn't be alarming in logs.
const statusClientClosedRequest = 499

// safeResponseHeaders is the case-insensitive whitelist of response
// headers we forward from a plugin back to the browser. Anything not
// in this set is stripped by sanitizeResponseHeaders to prevent a
// plugin from hijacking the portal session (Set-Cookie), overriding
// portal security policy (Content-Security-Policy / HSTS), redirecting
// users off-domain (Location), or injecting arbitrary auth-relevant
// headers (Authorization, WWW-Authenticate, Access-Control-*, etc.).
//
// Keys are stored in http.CanonicalMIMEHeaderKey form so lookups
// against http.Header (which canonicalizes on Set/Get) are exact.
var safeResponseHeaders = map[string]struct{}{
	"Content-Type":        {},
	"Content-Length":      {},
	"Content-Encoding":    {},
	"Content-Disposition": {},
	"Cache-Control":       {},
	"Etag":                {}, // http.CanonicalMIMEHeaderKey lowercases "TAG"
	"Last-Modified":       {},
	"Expires":             {},
	"Vary":                {},
	"Accept-Ranges":       {},
	"Content-Range":       {},
	"X-Request-Id":        {}, // we set this on the request side; mirror back for correlation
}

// sanitizeResponseHeaders strips every header from resp that isn't in
// safeResponseHeaders. Plugins are untrusted: a malicious or buggy one
// could otherwise set Set-Cookie (session hijack), override the
// portal's Content-Security-Policy, return a Location redirect to an
// off-domain host, or inject any X-* header into the portal session.
//
// Headers that are particularly dangerous to leak through —
// Set-Cookie and Location — also emit a WARN so we can spot
// misbehaving plugins in logs.
//
// Note: if a plugin returns a 3xx response, the Location header is
// stripped here. A 3xx without a Location is technically malformed
// HTTP, but the portal does not trust plugin-driven redirects, so
// that's the plugin's problem to fix on its end (e.g. return a JSON
// body the portal can act on instead).
func sanitizeResponseHeaders(resp *http.Response, pluginName string) {
	if resp == nil || resp.Header == nil {
		return
	}
	path := ""
	if resp.Request != nil && resp.Request.URL != nil {
		path = resp.Request.URL.Path
	}
	for name := range resp.Header {
		canonical := http.CanonicalHeaderKey(name)
		if _, ok := safeResponseHeaders[canonical]; ok {
			continue
		}
		switch canonical {
		case "Set-Cookie", "Set-Cookie2":
			slog.Warn("plugin proxy: dropping Set-Cookie from plugin response",
				"plugin", pluginName, "path", path)
		case "Location":
			slog.Warn("plugin proxy: dropping Location from plugin response",
				"plugin", pluginName, "path", path, "status", resp.StatusCode)
		}
		resp.Header.Del(name)
	}
}

// MountPluginProxyRoutes mounts the catch-all plugin proxy under /api/v1/x/{plugin}/*.
// Requests are forwarded to the plugin's address with the prefix stripped.
func MountPluginProxyRoutes(r chi.Router, srv *Server) {
	r.HandleFunc("/x/{plugin}/*", srv.HandlePluginProxy)
	// Also handle requests to the plugin root (no trailing path).
	r.HandleFunc("/x/{plugin}", srv.HandlePluginProxy)
}

// HandlePluginProxy forwards requests to the addressed plugin.
// Path: /api/v1/x/{plugin}/... → {plugin.Addr}/...
func (srv *Server) HandlePluginProxy(w http.ResponseWriter, r *http.Request) {
	pluginName := chi.URLParam(r, "plugin")
	if pluginName == "" {
		errorJSON(w, "plugin name required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if srv.PluginRegistry == nil {
		errorJSON(w, "plugin registry not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	p := srv.PluginRegistry.Get(pluginName)
	if p == nil {
		errorJSON(w, "plugin not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	if p.Status != domain.PluginStatusEnabled {
		errorJSON(w, "plugin is not enabled", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	target, err := url.Parse(plugins.EnsureScheme(p.Addr))
	if err != nil {
		slog.Error("invalid plugin address", "plugin", pluginName, "addr", p.Addr, "error", err)
		errorJSON(w, "invalid plugin address", "INTERNAL", http.StatusInternalServerError)
		return
	}

	// Strip the /api/v1/x/{plugin} prefix and forward the rest.
	prefix := "/api/v1/x/" + pluginName
	originalPath := r.URL.Path
	forwardPath := strings.TrimPrefix(originalPath, prefix)
	if forwardPath == "" {
		forwardPath = "/"
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = forwardPath
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = target.Host

			// Strip any inbound X-RAT-Plugin-Token before we forward.
			// A client must not be able to spoof the token by setting
			// the header themselves; only ratd's proxy may set it.
			req.Header.Del("X-RAT-Plugin-Token")

			// Forward X-Request-ID for tracing.
			if reqID := r.Header.Get("X-Request-ID"); reqID != "" {
				req.Header.Set("X-Request-ID", reqID)
			}
			// Forward the authenticated user ID for plugins that need it.
			if reqID := r.Header.Get("X-Forwarded-User"); reqID != "" {
				req.Header.Set("X-Forwarded-User", reqID)
			}
			// Inject the per-startup platform token if the plugin
			// advertised one in Describe. Plugins that opt in (empty
			// token means opted out — backward compat) reject any
			// inbound REST request that doesn't carry this header, so
			// a direct peer-to-peer call on the docker network gets
			// 401 while calls via this proxy keep working.
			if p.Token != "" {
				req.Header.Set("X-RAT-Plugin-Token", p.Token)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			// Plugins are untrusted code paths — strip every response
			// header except a small safe whitelist before it reaches
			// the browser. See sanitizeResponseHeaders for the
			// rationale and the full list.
			sanitizeResponseHeaders(resp, pluginName)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			// Distinguish "client gave up" (extremely common on long SSE
			// streams when the user hits stop or navigates away) from a
			// genuine upstream failure. context.Canceled is what
			// httputil surfaces when the *inbound* request's context is
			// done before the upstream call finishes; we don't escalate
			// that to ERROR because nothing's broken.
			if errors.Is(err, context.Canceled) || errors.Is(req.Context().Err(), context.Canceled) {
				slog.Info("plugin proxy: client canceled", "plugin", pluginName)
				// Writing the JSON body is best-effort — the client may
				// already be gone. errorJSON degrades gracefully there.
				errorJSON(w, "client closed request", "CANCELED", statusClientClosedRequest)
				return
			}
			slog.Error("plugin proxy error", "plugin", pluginName, "error", err)
			errorJSON(w, "plugin unavailable", "UNAVAILABLE", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}
