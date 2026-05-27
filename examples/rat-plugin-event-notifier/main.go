// Command event-notifier is an example RAT platform plugin (Layer 2 + Layer 3).
//
// It demonstrates the full v3 platform-plugin contract:
//   - implements PluginService over ConnectRPC (HealthCheck, Describe, HandleEvent)
//   - phones home to ratd's open plugin registry on startup
//   - subscribes to platform events and records them
//   - exposes an HTTP route (proxied by ratd) and a portal UI bundle
//
// Platform-token auth: handled by sdk.MountStandardPluginRoutes +
// sdk.TokenAuth. The per-startup token (sdk.RandomToken) is advertised
// via Describe; ratd's reverse proxy reads it from the registry and
// injects it as X-RAT-Plugin-Token on every forwarded call. /bundle.js,
// /health, and the ConnectRPC plugin-service paths stay unauthenticated
// (see sdk/auth.go for the contract).
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
	"context"
	_ "embed"
	"log/slog"
	"net/http"
	"os"
	"time"

	sdk "github.com/rat-data/rat/sdk-go"
)

//go:embed bundle.js
var bundleJS []byte

// bundleHash is computed once at startup over the go:embed'd bundle.js.
// It surfaces in Describe()'s UI descriptor so the portal can set
// <script integrity="sha256-…"> and the browser rejects any tampered
// bundle delivered through the ratd reverse proxy.
var bundleHash = sdk.SRIHash(bundleJS)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	env := sdk.LoadPluginEnv("event-notifier", "50090", "event-notifier:50090")
	webhookURL := os.Getenv("WEBHOOK_URL")

	// WEBHOOK_URL is only the initial default — the webhook URL, buffer size
	// and failure filter are all editable live in the portal's plugin settings.
	cfg := newConfigStore(env.RatdURL, env.Name, notifierConfig{
		WebhookURL: webhookURL,
		MaxEvents:  defaultMaxEvents,
	})

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken, cfg)

	// restMux serves the REST endpoints that ratd proxies under
	// /api/v1/x/event-notifier/*. sdk.MountStandardPluginRoutes wraps it with
	// TokenAuth so a peer-to-peer hit from another container on the docker
	// network is rejected with 401.
	restMux := http.NewServeMux()
	restMux.HandleFunc("/events", h.ServeEvents)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, restMux)

	slog.Info("starting event-notifier plugin",
		"port", env.Port, "ratd_url", env.RatdURL, "webhook_configured", webhookURL != "")

	// Register with ratd once the server is up (runs concurrently with Serve).
	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
	// Poll ratd for this plugin's config — RAT stores config but does not
	// push it, so a configurable plugin pulls its own.
	go cfg.poll(context.Background(), 15*time.Second)

	// h2c — ratd dials plugins over HTTP/2 cleartext.
	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
