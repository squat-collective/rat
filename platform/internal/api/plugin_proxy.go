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

			// Forward X-Request-ID for tracing.
			if reqID := r.Header.Get("X-Request-ID"); reqID != "" {
				req.Header.Set("X-Request-ID", reqID)
			}
			// Forward the authenticated user ID for plugins that need it.
			if reqID := r.Header.Get("X-Forwarded-User"); reqID != "" {
				req.Header.Set("X-Forwarded-User", reqID)
			}
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
