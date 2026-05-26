// Command interconnect is an example RAT platform + portal plugin: a plugin
// interconnection broker.
//
// It turns plugin-to-plugin wiring into a first-class mechanism. Plugins
// register named *capabilities* they offer; any plugin can then invoke a
// capability by name and the broker routes the call to a healthy provider —
// no hardcoded plugin names or routes. The portal UI draws the live "plugin
// mesh": every plugin, its health, and how capabilities wire them together.
//
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT    port to serve on             (default 50093)
//	PLUGIN_NAME  registered plugin name       (default interconnect)
//	PLUGIN_ADDR  address ratd dials back      (default interconnect:50093)
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/rat-data/rat/platform/gen/plugin/v1/pluginv1connect"
)

//go:embed bundle.js
var bundleJS []byte

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	name := envOr("PLUGIN_NAME", "interconnect")
	port := envOr("GRPC_PORT", "50093")
	selfAddr := envOr("PLUGIN_ADDR", "interconnect:50093")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	ratdInternalURL := envOr("RATD_INTERNAL_URL", ratdURL)

	st := newStore()
	// Self-register one capability so the broker is testable out of the box —
	// interconnect dogfoods its own directory.
	st.register(&Capability{
		Name:        "mesh.describe",
		Provider:    name,
		Method:      "GET",
		Path:        "/mesh",
		Description: "The live plugin mesh — interconnect's directory of plugins and capabilities.",
	})

	a := newAPI(st, newRatdClient(ratdURL))
	h := newHandler(name, "http://"+selfAddr+"/bundle.js")

	mux := http.NewServeMux()
	// ConnectRPC: the PluginService ratd calls (HealthCheck, Describe).
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	// The portal UI bundle, served to ratd's bundle proxy.
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	// The REST API — ratd proxies it at /api/v1/x/interconnect/*.
	mux.Handle("/", a.mux())

	slog.Info("starting interconnect plugin", "port", port, "ratd_url", ratdURL)

	go phoneHome(ratdInternalURL, name, selfAddr)

	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// phoneHome registers the plugin with ratd's open registry, retrying until
// ratd is reachable. The HTTP server must already be listening when this runs.
func phoneHome(ratdURL, name, addr string) {
	body, _ := json.Marshal(map[string]string{"name": name, "addr": addr})
	endpoint := ratdURL + "/internal/plugins/register"

	for attempt := 1; attempt <= 30; attempt++ {
		time.Sleep(2 * time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			slog.Error("phone-home: bad request", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			slog.Info("phone-home: ratd not reachable yet, retrying", "attempt", attempt)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			slog.Info("registered with ratd", "endpoint", endpoint)
			return
		}
		slog.Warn("phone-home rejected", "status", resp.StatusCode, "attempt", attempt)
	}
	slog.Error("phone-home failed after retries — plugin is running but unregistered")
}
