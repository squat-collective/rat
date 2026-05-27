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
// Platform-token auth: handled by sdk.MountStandardPluginRoutes +
// sdk.TokenAuth. The per-startup token (sdk.RandomToken) is advertised
// via Describe; ratd's reverse proxy reads it from the registry and
// injects it as X-RAT-Plugin-Token on every forwarded call. /bundle.js,
// /health, and the ConnectRPC plugin-service paths stay unauthenticated
// (see sdk/auth.go for the contract).
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

	env := sdk.LoadPluginEnv("ai-provider", "50094", "ai-provider:50094")

	// Initial config defaults from env. Anything set in the portal's plugin
	// settings overrides these (picked up by the config poll).
	defaults := aiConfig{
		BaseURL: envOr("OPENAI_BASE_URL", ""),
		APIKey:  envOr("OPENAI_API_KEY", "ollama"),
		Model:   envOr("AI_MODEL", ""),
	}

	cfg := newConfigStore(env.RatdURL, env.Name, defaults)
	a := newAPI(cfg, newLLM(cfg))

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting ai-provider plugin", "port", env.Port, "ratd_url", env.RatdURL)

	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
	// Poll ratd for this plugin's config — RAT stores config but does not push
	// it, so a configurable plugin pulls its own.
	go cfg.poll(context.Background(), 15*time.Second)
	// Advertise the AI capabilities to the interconnect plugin (optional).
	go registerWithInterconnect(env.RatdURL, env.Name)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
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
