package api

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// MountPluginBundleRoutes mounts the plugin UI bundle proxy endpoint.
// ratd reverse-proxies the plugin's JS bundle so the portal can load it
// via a same-origin <script> tag without CORS.
func MountPluginBundleRoutes(r chi.Router, srv *Server) {
	r.Get("/plugins/{name}/ui/bundle.js", srv.HandlePluginBundle)
}

// HandlePluginBundle reverse-proxies a plugin's UI bundle.
// Path: GET /api/v1/plugins/{name}/ui/bundle.js
// Returns 404 if the plugin is not found or has no UI descriptor with a bundle_url.
// Returns 503 if the plugin registry is unavailable.
func (srv *Server) HandlePluginBundle(w http.ResponseWriter, r *http.Request) {
	pluginName := chi.URLParam(r, "name")

	if srv.PluginRegistry == nil {
		errorJSON(w, "plugin registry not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	p := srv.PluginRegistry.Get(pluginName)
	if p == nil {
		errorJSON(w, "plugin not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	if p.Descriptor == nil || p.Descriptor.Ui == nil || p.Descriptor.Ui.BundleUrl == "" {
		errorJSON(w, "plugin has no UI bundle", "NOT_FOUND", http.StatusNotFound)
		return
	}

	target, err := url.Parse(plugins.EnsureScheme(p.Descriptor.Ui.BundleUrl))
	if err != nil {
		slog.Error("invalid plugin bundle URL", "plugin", pluginName, "url", p.Descriptor.Ui.BundleUrl, "error", err)
		errorJSON(w, "invalid plugin bundle URL", "INTERNAL", http.StatusInternalServerError)
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL = target
			req.Host = target.Host
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Set("Content-Type", "application/javascript")
			resp.Header.Set("Cache-Control", "public, max-age=300")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			slog.Error("plugin bundle proxy error", "plugin", pluginName, "error", err)
			errorJSON(w, "plugin bundle unavailable", "UNAVAILABLE", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}
