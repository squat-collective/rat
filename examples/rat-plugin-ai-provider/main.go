// Command ai-provider is an example RAT platform + portal plugin: a reusable,
// configurable AI provider.
//
// It is a backend AI service — not a chat app. It wraps any OpenAI-compatible
// LLM and exposes /complete and /chat, the primitives other AI plugins reuse.
// It is the first RAT plugin to be *configurable*: it declares a
// config_schema_json, the portal renders a settings form from it, and the
// plugin polls ratd for its own config (RAT stores config but does not push
// it). It also registers ai.complete / ai.chat capabilities with the
// interconnect plugin, so other plugins can broker to it by capability.
//
// Platform-token auth (see DescribeResponse.platform_token in
// proto/plugin/v1/plugin.proto): on startup we generate a fresh
// 32-byte hex token, advertise it via Describe, and wrap the REST mux
// with middleware that rejects any inbound request lacking the
// matching X-RAT-Plugin-Token header. ratd's reverse proxy reads the
// token from the registry and injects it on every forwarded call, so
// traffic via /api/v1/x/ai-provider/* keeps working — but a direct
// peer-to-peer call to http://ai-provider:50094/... from another
// container on the docker network now gets 401. The token regenerates
// on every plugin startup. /bundle.js, /health, and the ConnectRPC
// plugin-service paths stay unauthenticated for the usual reasons
// (script-tag fetch, container liveness, and "that's how ratd LEARNS
// the token").
//
// Environment (initial defaults — change them live in the portal settings):
//
//	RATD_URL         ratd base URL                (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL      (default = RATD_URL)
//	OPENAI_BASE_URL  initial API base URL         (default empty)
//	OPENAI_API_KEY   initial API key              (default "ollama")
//	AI_MODEL         initial model name           (default empty)
//	GRPC_PORT        port to serve on             (default 50094)
//	PLUGIN_NAME      registered plugin name       (default ai-provider)
//	PLUGIN_ADDR      address ratd dials back      (default ai-provider:50094)
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

	name := envOr("PLUGIN_NAME", "ai-provider")
	port := envOr("GRPC_PORT", "50094")
	selfAddr := envOr("PLUGIN_ADDR", "ai-provider:50094")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	ratdInternalURL := envOr("RATD_INTERNAL_URL", ratdURL)

	// Initial config defaults from env. Anything set in the portal's plugin
	// settings overrides these (picked up by the config poll).
	defaults := aiConfig{
		BaseURL: envOr("OPENAI_BASE_URL", ""),
		APIKey:  envOr("OPENAI_API_KEY", "ollama"),
		Model:   envOr("AI_MODEL", ""),
	}

	cfg := newConfigStore(ratdURL, name, defaults)
	a := newAPI(cfg, newLLM(cfg))

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
	// The provider REST API — ratd proxies it at /api/v1/x/ai-provider/*.
	// Wrapped with tokenAuth so peer-to-peer calls are rejected.
	mux.Handle("/", tokenAuth(platformToken, a.mux()))

	slog.Info("starting ai-provider plugin", "port", port, "ratd_url", ratdURL)

	go phoneHome(ratdInternalURL, name, selfAddr)
	// Poll ratd for this plugin's config — RAT stores config but does not push
	// it, so a configurable plugin pulls its own.
	go cfg.poll(context.Background(), 15*time.Second)
	// Advertise the AI capabilities to the interconnect plugin (optional).
	go registerWithInterconnect(ratdURL, name)

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

// registerWithInterconnect advertises the provider's capabilities to the
// interconnect plugin, so other plugins can broker to it by capability name.
// Runs forever: re-registers every 60s once steady-state. This handles the
// "interconnect restarted and lost everything" case — within 60s the
// capabilities are back. Idempotent: register is treated as upsert by
// the interconnect store.
func registerWithInterconnect(ratdURL, self string) {
	caps := []map[string]string{
		{"name": "ai.complete", "provider": self, "method": "POST", "path": "/complete",
			"description": "One-shot LLM completion from the AI provider."},
		{"name": "ai.chat", "provider": self, "method": "POST", "path": "/chat",
			"description": "Raw multi-message chat completion from the AI provider."},
		{"name": "ai.chat-with-tools", "provider": self, "method": "POST", "path": "/chat-with-tools",
			"description": "Chat with OpenAI-style tool/function calling — returns tool_calls + finish_reason."},
		{"name": "ai.chat-with-tools-stream", "provider": self, "method": "POST", "path": "/chat-with-tools-stream",
			"description": "Streaming variant of chat-with-tools — SSE events 'delta' / 'done' / 'error'."},
	}
	endpoint := ratdURL + "/api/v1/x/interconnect/register"

	var wasUp bool
	for {
		ok := tryRegisterAll(endpoint, caps)
		if ok && !wasUp {
			slog.Info("registered capabilities with the interconnect plugin", "count", len(caps))
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
