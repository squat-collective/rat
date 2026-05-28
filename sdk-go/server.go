package sdk

import (
	"log/slog"
	"net/http"

	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// MountStandardPluginRoutes attaches the ConnectRPC plugin service +
// the /bundle.js handler to mux, then wraps restMux with TokenAuth and
// mounts it at "/". Returns the mux ready to pass to http.Server.
//
// Layering rationale:
//   - The ConnectRPC plugin-service paths go on the bare mux because
//     that channel is exactly how ratd LEARNS the platform token via
//     Describe — wrapping it with TokenAuth would deadlock startup.
//   - /bundle.js goes on the bare mux because the portal's <script>
//     tag fetches the bundle through ratd's proxy and browsers can't
//     attach custom headers to script-tag requests. TokenAuth's
//     allowlist for /bundle.js is the second line of defence (when
//     callers wrap a tree containing /bundle.js).
//   - Everything else flows through TokenAuth, which itself allow-lists
//     /bundle.js and /health.
//
// This collapses the ~30 LOC of mux setup duplicated across every
// example plugin.
//
// Defensive normalization:
//   - A nil restMux is replaced with http.NotFoundHandler() so the
//     TokenAuth wrap (and its nil dereference) never panics. This is
//     the "no REST" mode — the plugin still gets bundle + ConnectRPC +
//     a TokenAuth-protected 404 for everything else.
//   - An empty bundleJS skips the /bundle.js mount entirely. Plugins
//     without a UI shouldn't carry a zero-byte bundle endpoint.
func MountStandardPluginRoutes(
	mux *http.ServeMux,
	pluginHandler pluginv1connect.PluginServiceHandler,
	bundleJS []byte,
	platformToken string,
	restMux http.Handler,
) http.Handler {
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(pluginHandler)
	mux.Handle(pluginPath, pluginHTTP)
	if len(bundleJS) > 0 {
		mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(bundleJS)
		})
	}
	if restMux == nil {
		slog.Warn("MountStandardPluginRoutes: restMux is nil; mounting NotFoundHandler (no-REST mode)")
		restMux = http.NotFoundHandler()
	}
	mux.Handle("/", TokenAuth(platformToken, restMux))
	return mux
}

// H2CHandler wraps h with h2c support — the standard transport every
// example plugin uses for HTTP/2 over cleartext (ratd → plugin and
// plugin → ratd). Equivalent to:
//
//	h2c.NewHandler(h, &http2.Server{})
//
// Kept here so plugins don't need to import golang.org/x/net/http2
// just for the one-liner.
func H2CHandler(h http.Handler) http.Handler {
	return h2c.NewHandler(h, &http2.Server{})
}
