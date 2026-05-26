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
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
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

	name := envOr("PLUGIN_NAME", "secrets")
	port := envOr("GRPC_PORT", "50099")
	selfAddr := envOr("PLUGIN_ADDR", "secrets:50099")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
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

	a := newAPI(st)
	h := newHandler(name, "http://"+selfAddr+"/bundle.js")

	mux := http.NewServeMux()
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	mux.Handle("/", a.mux())

	slog.Info("starting secrets plugin", "port", port, "ratd_url", ratdURL)

	ctx := context.Background()
	go func() {
		phoneHome(ratdURL, name, selfAddr)
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
