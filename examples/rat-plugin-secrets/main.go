// Command secrets is a RAT plugin: an encrypted key-value store for
// shared credentials (Postgres URLs, API keys, etc) that other plugins
// fetch via the interconnect 'secrets.get' capability.
//
// AES-256-GCM with a key resolved from (in order):
//   - RAT_SECRETS_KEY env var (hex-encoded 32 bytes)
//   - /data/secrets.key — auto-generated + persisted on first run
//
// Storage: the encrypted list is the plugin's own config (PUT
// /api/v1/plugins/secrets/config) — same persistence trick as
// rat-plugin-agents. Losing the key bricks the ciphertexts, which is
// the correct behaviour for a secret store.
//
// Platform-token auth (see DescribeResponse.platform_token in
// proto/plugin/v1/plugin.proto): on startup we generate a fresh
// 32-byte hex token, advertise it via Describe, and wrap the REST mux
// with middleware that rejects any inbound request lacking the
// matching X-RAT-Plugin-Token header. ratd's reverse proxy reads the
// token from the registry and injects it on every forwarded call, so
// browser/CLI/portal traffic via /api/v1/x/secrets/* keeps working —
// but a direct peer-to-peer hit like `curl http://secrets:50099/resolve`
// from another container on the docker network now gets 401. The
// token regenerates on every plugin startup; brief 401 window during
// restart is acceptable (callers retry). /bundle.js, /health, and the
// ConnectRPC plugin-service paths stay unauthenticated — the first so
// the portal's <script> tag can fetch the bundle, the second for
// liveness, the third because that's how ratd LEARNS the token.
//
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT         HTTP port to serve on      (default 50099)
//	PLUGIN_NAME       registered plugin name     (default secrets)
//	PLUGIN_ADDR       address ratd dials back    (default secrets:50099)
//	RAT_SECRETS_KEY   hex-encoded 32-byte AES key (optional)
//	SECRETS_KEY_FILE  fallback path             (default /data/secrets.key)
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
// expected == "" disables the check (opt-out). Our plugins always
// pass a non-empty token so this branch is mostly a safety net.
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

	name := envOr("PLUGIN_NAME", "secrets")
	port := envOr("GRPC_PORT", "50099")
	selfAddr := envOr("PLUGIN_ADDR", "secrets:50099")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	ratdInternalURL := envOr("RATD_INTERNAL_URL", ratdURL)
	keyFile := envOr("SECRETS_KEY_FILE", "/data/secrets.key")

	key, err := loadOrCreateKey(os.Getenv("RAT_SECRETS_KEY"), keyFile)
	if err != nil {
		slog.Error("could not initialise encryption key", "error", err)
		os.Exit(1)
	}

	cfg := newConfigStore(ratdURL, name)
	st, err := newStore(key, cfg)
	if err != nil {
		slog.Error("could not initialise secret store", "error", err)
		os.Exit(1)
	}
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
	// Bundle endpoint — NOT wrapped: the portal's <script> tag fetches
	// the bundle via ratd's proxy and the browser can't add custom
	// headers to script-tag requests.
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	// REST endpoints — wrapped. /health inside this mux is allow-listed
	// by tokenAuth so container liveness probes still work.
	mux.Handle("/", tokenAuth(platformToken, a.mux()))

	slog.Info("starting secrets plugin", "port", port, "ratd_url", ratdURL)

	ctx := context.Background()
	go func() {
		phoneHome(ratdInternalURL, name, selfAddr)
		cfg.refresh(ctx) // pull encrypted list ASAP so /resolve can work pre-poll
		cfg.poll(ctx, 15*time.Second)
	}()
	go registerWithInterconnect(ratdURL, name)

	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
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
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			slog.Info("registered with ratd")
			return
		}
	}
	slog.Error("phone-home failed after retries")
}

// registerWithInterconnect loops forever so an interconnect restart
// doesn't silently lose the secrets.get capability. Same pattern the
// other plugins use.
func registerWithInterconnect(ratdURL, self string) {
	cap := map[string]string{
		"name": "secrets.get", "provider": self, "method": "POST", "path": "/resolve",
		"description": "Resolve a secret by name. Body: {\"name\": \"...\"}. Returns {\"name\": ..., \"value\": ...}.",
	}
	endpoint := ratdURL + "/api/v1/x/interconnect/register"
	var wasUp bool
	for {
		ok := tryRegister(endpoint, cap)
		if ok && !wasUp {
			slog.Info("registered secrets.get with interconnect")
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

func tryRegister(endpoint string, cap map[string]string) bool {
	body, _ := json.Marshal(cap)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 300
}
