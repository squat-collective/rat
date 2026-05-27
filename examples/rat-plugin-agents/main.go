// Command agents is a RAT plugin: a registry of "agents" (named personas)
// the chat plugin (and any future consumer) can adopt. An Agent bundles a
// system prompt, an allowed-tool whitelist (namespaced MCP tool names),
// and optional model/temperature overrides. The catalog persists via
// ratd's plugin-config mechanism, so it survives restarts without a
// mounted volume or database schema.
//
// The plugin self-registers two capabilities with the interconnect:
//   - agents.list  →  GET /agents      (catalog snapshot)
//   - agents.get   →  GET /agents/{id} (single agent — note: callers that
//                                       use the broker pass id in the path)
//
// On first run with an empty catalog, the plugin seeds three defaults
// (Generalist, Data Explorer, Analyst) so the chat picker has something
// to switch between out of the box.
//
// Platform-token auth (see DescribeResponse.platform_token in
// proto/plugin/v1/plugin.proto): on startup we generate a fresh
// 32-byte hex token, advertise it via Describe, and wrap the REST mux
// with middleware that rejects any inbound request lacking the
// matching X-RAT-Plugin-Token header. ratd's reverse proxy reads the
// token from the registry and injects it on every forwarded call, so
// traffic via /api/v1/x/agents/* keeps working — but a direct
// peer-to-peer call to http://agents:50096/... from another container
// on the docker network now gets 401. The token regenerates on every
// plugin startup. /bundle.js, /health, and the ConnectRPC
// plugin-service paths stay unauthenticated for the usual reasons
// (script-tag fetch, container liveness, and "that's how ratd LEARNS
// the token").
//
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT    HTTP port to serve on         (default 50096)
//	PLUGIN_NAME  registered plugin name        (default agents)
//	PLUGIN_ADDR  address ratd dials back       (default agents:50096)
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

	name := envOr("PLUGIN_NAME", "agents")
	port := envOr("GRPC_PORT", "50096")
	selfAddr := envOr("PLUGIN_ADDR", "agents:50096")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	ratdInternalURL := envOr("RATD_INTERNAL_URL", ratdURL)

	cfg := newConfigStore(ratdURL, name)
	st := newStore(cfg)
	// When the polled config changes, refresh the in-memory store.
	cfg.onChange(st.hydrate)

	platformToken := randomToken()

	a := newAPI(st)
	h := newHandler(name, "http://"+selfAddr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	// ConnectRPC plugin-service paths — NOT wrapped: ratd reaches
	// these via direct gRPC, not the reverse proxy, and learns the
	// token from Describe over this very channel.
	mux.Handle(pluginPath, pluginHTTP)
	// Bundle endpoint — NOT wrapped: the portal's <script> tag can't
	// add custom headers to script-tag requests.
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	// REST endpoints — wrapped. /health inside this mux is allow-listed.
	mux.Handle("/", tokenAuth(platformToken, a.mux()))

	slog.Info("starting agents plugin", "port", port, "ratd_url", ratdURL)

	ctx := context.Background()
	// Seed + poll must run AFTER phone-home succeeds, because the persist
	// path writes back to ratd and only works once we're in the registry.
	// We chain them in one goroutine so the order is explicit.
	go func() {
		phoneHome(ratdInternalURL, name, selfAddr)
		cfg.refresh(ctx)
		seedIfEmpty(ctx, st)
		cfg.poll(ctx, 15*time.Second)
	}()
	go registerWithInterconnect(ratdURL, name)

	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// seedIfEmpty inserts the default agents on first run, so a fresh install
// has something for the chat picker to show.
func seedIfEmpty(ctx context.Context, st *store) {
	if len(st.list()) > 0 {
		return
	}
	slog.Info("seeding default agents (catalog is empty)")
	for _, a := range seedAgents() {
		if _, err := st.create(ctx, a); err != nil {
			slog.Warn("seed failed for agent", "id", a.ID, "error", err)
		}
	}
}

func phoneHome(ratdURL, name, addr string) {
	body, _ := json.Marshal(map[string]string{"name": name, "addr": addr})
	endpoint := ratdURL + "/internal/plugins/register"

	for attempt := 1; attempt <= 30; attempt++ {
		time.Sleep(2 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			slog.Info("phone-home: ratd not reachable yet", "attempt", attempt)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			slog.Info("registered with ratd", "endpoint", endpoint)
			return
		}
	}
	slog.Error("phone-home failed after retries")
}

// Runs forever: re-registers every 60s once steady-state. Same pattern
// as the MCP plugins — protects against interconnect restart wiping
// capabilities.
func registerWithInterconnect(ratdURL, self string) {
	caps := []map[string]string{
		{"name": "agents.list", "provider": self, "method": "GET", "path": "/agents",
			"description": "List all agents (system prompts + tool whitelists + model overrides)."},
	}
	endpoint := ratdURL + "/api/v1/x/interconnect/register"

	var wasUp bool
	for {
		ok := tryRegisterAll(endpoint, caps)
		if ok && !wasUp {
			slog.Info("registered capabilities with interconnect", "count", len(caps))
		} else if !ok && wasUp {
			slog.Warn("interconnect registration failed — will retry")
		}
		wasUp = ok
		if ok {
			time.Sleep(60 * time.Second)
		} else {
			time.Sleep(5 * time.Second)
		}
	}
}

func tryRegisterAll(endpoint string, caps []map[string]string) bool {
	for _, c := range caps {
		body, _ := json.Marshal(c)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil || resp.StatusCode >= 300 {
			if resp != nil {
				_ = resp.Body.Close()
			}
			return false
		}
		_ = resp.Body.Close()
	}
	return true
}
