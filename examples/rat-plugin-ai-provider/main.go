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
// Environment (initial defaults — change them live in the portal settings):
//
//	RATD_URL         ratd base URL                (default http://ratd:8080)
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

	name := envOr("PLUGIN_NAME", "ai-provider")
	port := envOr("GRPC_PORT", "50094")
	selfAddr := envOr("PLUGIN_ADDR", "ai-provider:50094")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")

	// Initial config defaults from env. Anything set in the portal's plugin
	// settings overrides these (picked up by the config poll).
	defaults := aiConfig{
		BaseURL: envOr("OPENAI_BASE_URL", ""),
		APIKey:  envOr("OPENAI_API_KEY", "ollama"),
		Model:   envOr("AI_MODEL", ""),
	}

	cfg := newConfigStore(ratdURL, name, defaults)
	a := newAPI(cfg, newLLM(cfg))
	h := newHandler(name, "http://"+selfAddr+"/bundle.js")

	mux := http.NewServeMux()
	// ConnectRPC: the PluginService ratd calls (HealthCheck, Describe).
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	// The portal UI bundle, served to ratd's bundle proxy.
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	// The provider REST API — ratd proxies it at /api/v1/x/ai-provider/*.
	mux.Handle("/", a.mux())

	slog.Info("starting ai-provider plugin", "port", port, "ratd_url", ratdURL)

	go phoneHome(ratdURL, name, selfAddr)
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
// The interconnect plugin is optional — if it is not installed this gives up
// quietly after a few retries.
func registerWithInterconnect(ratdURL, self string) {
	caps := []map[string]string{
		{"name": "ai.complete", "provider": self, "method": "POST", "path": "/complete",
			"description": "One-shot LLM completion from the AI provider."},
		{"name": "ai.chat", "provider": self, "method": "POST", "path": "/chat",
			"description": "Raw multi-message chat completion from the AI provider."},
		{"name": "ai.chat-with-tools", "provider": self, "method": "POST", "path": "/chat-with-tools",
			"description": "Chat with OpenAI-style tool/function calling — returns tool_calls + finish_reason."},
	}
	endpoint := ratdURL + "/api/v1/x/interconnect/register"

	for attempt := 1; attempt <= 15; attempt++ {
		time.Sleep(3 * time.Second)
		ok := true
		for _, c := range caps {
			body, _ := json.Marshal(c)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
			if err != nil {
				cancel()
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			cancel()
			if err != nil || resp.StatusCode >= 300 {
				if resp != nil {
					_ = resp.Body.Close()
				}
				ok = false
				break
			}
			_ = resp.Body.Close()
		}
		if ok {
			slog.Info("registered capabilities with the interconnect plugin")
			return
		}
		slog.Info("interconnect not ready, retrying capability registration", "attempt", attempt)
	}
	slog.Warn("could not register with interconnect — it may not be installed (this is optional)")
}
