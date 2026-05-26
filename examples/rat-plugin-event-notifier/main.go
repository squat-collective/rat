// Command event-notifier is an example RAT platform plugin (Layer 2 + Layer 3).
//
// It demonstrates the full v3 platform-plugin contract:
//   - implements PluginService over ConnectRPC (HealthCheck, Describe, HandleEvent)
//   - phones home to ratd's open plugin registry on startup
//   - subscribes to platform events and records them
//   - exposes an HTTP route (proxied by ratd) and a portal UI bundle
//
// Environment:
//
//	GRPC_PORT    port to serve on            (default 50090)
//	PLUGIN_NAME  registered plugin name      (default event-notifier)
//	PLUGIN_ADDR  address ratd dials back     (default event-notifier:50090)
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL for phone-home (default = RATD_URL)
//	WEBHOOK_URL  initial webhook URL — override it (and the other settings)
//	             live in the portal's plugin configuration
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

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	var (
		name       = envOr("PLUGIN_NAME", "event-notifier")
		port       = envOr("GRPC_PORT", "50090")
		selfAddr   = envOr("PLUGIN_ADDR", "event-notifier:50090")
		ratdURL         = envOr("RATD_URL", "http://ratd:8080")
		ratdInternalURL = envOr("RATD_INTERNAL_URL", ratdURL)
		webhookURL      = os.Getenv("WEBHOOK_URL")
	)

	// WEBHOOK_URL is only the initial default — the webhook URL, buffer size
	// and failure filter are all editable live in the portal's plugin settings.
	cfg := newConfigStore(ratdURL, name, notifierConfig{
		WebhookURL: webhookURL,
		MaxEvents:  defaultMaxEvents,
	})
	h := newHandler(name, "http://"+selfAddr+"/bundle.js", cfg)

	mux := http.NewServeMux()
	// ConnectRPC: the PluginService ratd calls — HealthCheck, Describe, HandleEvent.
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	// Plain HTTP: the portal UI bundle (ratd reverse-proxies it) and the
	// /events route (ratd proxies it at /api/v1/x/{name}/events).
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	mux.HandleFunc("/events", h.ServeEvents)

	slog.Info("starting event-notifier plugin",
		"port", port, "ratd_url", ratdURL, "webhook_configured", webhookURL != "")

	// Register with ratd once the server is up (runs concurrently with Serve).
	go phoneHome(ratdInternalURL, name, selfAddr)
	// Poll ratd for this plugin's config — RAT stores config but does not
	// push it, so a configurable plugin pulls its own.
	go cfg.poll(context.Background(), 15*time.Second)

	// h2c — ratd dials plugins over HTTP/2 cleartext.
	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// phoneHome registers this plugin with ratd's open plugin registry. ratd then
// calls back HealthCheck + Describe, so the HTTP server must already be
// listening. Retries because ratd may still be starting up.
func phoneHome(ratdURL, name, addr string) {
	body, _ := json.Marshal(map[string]string{"name": name, "addr": addr})
	url := ratdURL + "/internal/plugins/register"

	for attempt := 1; attempt <= 30; attempt++ {
		time.Sleep(2 * time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
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
			slog.Info("registered with ratd", "url", url)
			return
		}
		slog.Warn("phone-home rejected", "status", resp.StatusCode, "attempt", attempt)
	}
	slog.Error("phone-home failed after retries — plugin is running but unregistered")
}
