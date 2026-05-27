// Command event-notifier is an example RAT platform plugin (Layer 2 + Layer 3).
//
// It demonstrates the full v3 platform-plugin contract:
//   - implements PluginService over ConnectRPC (HealthCheck, Describe, HandleEvent)
//   - phones home to ratd's open plugin registry on startup
//   - subscribes to platform events and records them
//   - exposes an HTTP route (proxied by ratd) and a portal UI bundle
//
// Platform-token auth (see DescribeResponse.platform_token in
// proto/plugin/v1/plugin.proto): on startup we generate a fresh
// 32-byte hex token, advertise it via Describe, and wrap the REST
// surface with middleware that rejects any inbound request lacking
// the matching X-RAT-Plugin-Token header. ratd's reverse proxy reads
// the token from the registry and injects it on every forwarded call,
// so traffic via /api/v1/x/event-notifier/* keeps working — but a
// direct peer-to-peer call to http://event-notifier:50090/events
// from another container on the docker network now gets 401.
// /bundle.js, /health, and the ConnectRPC plugin-service paths stay
// unauthenticated.
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
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
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

// bundleHash is computed once at startup over the go:embed'd bundle.js.
// It surfaces in Describe()'s UI descriptor so the portal can set
// <script integrity="sha256-…"> and the browser rejects any tampered
// bundle delivered through the ratd reverse proxy.
var bundleHash = sriHash(bundleJS)

func sriHash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// randomToken returns a fresh 32-byte hex secret for the
// X-RAT-Plugin-Token contract documented at the top of this file.
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is a critical OS-level problem; we
		// cannot safely continue without a real secret.
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// tokenAuth wraps the REST mux. It rejects any request lacking the
// expected X-RAT-Plugin-Token header EXCEPT for /bundle.js (the
// portal's <script> tag can't add custom headers) and /health (used
// by container orchestration for liveness). The ConnectRPC
// plugin-service paths are NOT routed through this middleware — they
// are registered on their own subtree above and reached by ratd via
// direct gRPC, not the reverse proxy.
//
// expected == "" disables the check (opt-out).
func tokenAuth(expected string, next http.Handler) http.Handler {
	if expected == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bundle.js" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-RAT-Plugin-Token") != expected {
			http.Error(w, "missing or invalid platform token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
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

	platformToken := randomToken()
	h := newHandler(name, "http://"+selfAddr+"/bundle.js", bundleHash, platformToken, cfg)

	// restMux serves the REST endpoints that ratd proxies under
	// /api/v1/x/event-notifier/*. We wrap it with tokenAuth so a
	// peer-to-peer hit from another container on the docker network is
	// rejected with 401.
	restMux := http.NewServeMux()
	restMux.HandleFunc("/events", h.ServeEvents)

	mux := http.NewServeMux()
	// ConnectRPC plugin-service paths — NOT wrapped: ratd reaches
	// these via direct gRPC, not the reverse proxy, and learns the
	// token from Describe over this very channel.
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	// Bundle endpoint — NOT wrapped: the portal's <script> tag can't
	// add custom headers to script-tag requests.
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	// REST endpoints — wrapped. /health inside restMux is allow-listed
	// by tokenAuth so container liveness probes still work.
	mux.Handle("/", tokenAuth(platformToken, restMux))

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
