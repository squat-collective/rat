// Command interconnect is an example RAT platform + portal plugin: a plugin
// interconnection broker.
//
// It turns plugin-to-plugin wiring into a first-class mechanism. Plugins
// register named *capabilities* they offer; any plugin can then invoke a
// capability by name and the broker routes the call to a healthy provider —
// no hardcoded plugin names or routes. The portal UI draws the live "plugin
// mesh": every plugin, its health, and how capabilities wire them together.
//
// Platform-token auth (see DescribeResponse.platform_token in
// proto/plugin/v1/plugin.proto): on startup we generate a fresh
// 32-byte hex token, advertise it via Describe, and wrap the REST
// surface with middleware that rejects any inbound request lacking
// the matching X-RAT-Plugin-Token header. ratd's reverse proxy reads
// the token from the registry and injects it on every forwarded call,
// so traffic via /api/v1/x/interconnect/* keeps working — but a
// direct peer-to-peer call to http://interconnect:50093/... from
// another container on the docker network now gets 401. /bundle.js,
// /health, and the ConnectRPC plugin-service paths stay
// unauthenticated.
//
// Important note for capability invocation: other plugins do not call
// the interconnect's REST surface directly — they go through ratd's
// reverse proxy at /api/v1/x/interconnect/register and /invoke, which
// stamps the platform token on the way in. So the token-auth wrap is
// transparent to the broker's normal operation.
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

	platformToken := randomToken()
	h := newHandler(name, "http://"+selfAddr+"/bundle.js", bundleHash, platformToken)

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
	// REST endpoints — wrapped. /health inside a.mux() is allow-listed.
	mux.Handle("/", tokenAuth(platformToken, a.mux()))

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
