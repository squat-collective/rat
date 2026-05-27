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
// Platform-token auth: handled by sdk.MountStandardPluginRoutes +
// sdk.TokenAuth. The per-startup token (sdk.RandomToken) is advertised
// via Describe; ratd's reverse proxy reads it from the registry and
// injects it as X-RAT-Plugin-Token on every forwarded call. /bundle.js,
// /health, and the ConnectRPC plugin-service paths stay unauthenticated
// (see sdk/auth.go for the contract).
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
	_ "embed"
	"encoding/json"
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	env := sdk.LoadPluginEnv("secrets", "50099", "secrets:50099")
	keyFile := envOr("SECRETS_KEY_FILE", "/data/secrets.key")

	key, err := loadOrCreateKey(os.Getenv("RAT_SECRETS_KEY"), keyFile)
	if err != nil {
		slog.Error("could not initialise encryption key", "error", err)
		os.Exit(1)
	}

	cfg := newConfigStore(env.RatdURL, env.Name)
	st, err := newStore(key, cfg)
	if err != nil {
		slog.Error("could not initialise secret store", "error", err)
		os.Exit(1)
	}
	cfg.onChange(st.hydrate)

	platformToken := sdk.RandomToken()

	a := newAPI(st)
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting secrets plugin", "port", env.Port, "ratd_url", env.RatdURL)

	ctx := context.Background()
	go func() {
		sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
		cfg.refresh(ctx) // pull encrypted list ASAP so /resolve can work pre-poll
		cfg.poll(ctx, 15*time.Second)
	}()
	go registerWithInterconnect(env.RatdURL, env.Name)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
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
