// Command ai is an example RAT platform + portal plugin: an AI data navigator.
//
// It exposes a chat endpoint backed by any OpenAI-compatible LLM API (Ollama,
// OpenAI, vLLM, …). The model is given tools to list tables, inspect schemas,
// and run read-only queries, so it can genuinely explore and analyse data —
// not just chat. The portal UI bundle adds an /x/ai chat page.
//
// Environment:
//
//	OPENAI_BASE_URL  OpenAI-compatible API base   (default http://localhost:11434/v1)
//	OPENAI_API_KEY   API key                      (default "ollama"; Ollama ignores it)
//	AI_MODEL         model name                   (default gpt-oss:20b)
//	RATD_URL         ratd base URL                (default http://ratd:8080)
//	GRPC_PORT        port to serve on             (default 50091)
//	PLUGIN_NAME      registered plugin name       (default ai)
//	PLUGIN_ADDR      address ratd dials back      (default ai:50091)
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

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	name := envOr("PLUGIN_NAME", "ai")
	port := envOr("GRPC_PORT", "50091")
	selfAddr := envOr("PLUGIN_ADDR", "ai:50091")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	baseURL := envOr("OPENAI_BASE_URL", "http://localhost:11434/v1")
	apiKey := envOr("OPENAI_API_KEY", "ollama")
	model := envOr("AI_MODEL", "gpt-oss:20b")

	chat := newChatService(newAIClient(baseURL, apiKey, model), newDataTools(ratdURL))
	h := newHandler(name, "http://"+selfAddr+"/bundle.js")

	mux := http.NewServeMux()
	// ConnectRPC: the PluginService ratd calls.
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	// Plain HTTP: the chat route (ratd proxies it at /api/v1/x/ai/chat) and
	// the portal UI bundle.
	mux.HandleFunc("/chat", chat.HandleChat)
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})

	slog.Info("starting ai plugin", "port", port, "model", model, "ai_base_url", baseURL)

	go phoneHome(ratdURL, name, selfAddr)

	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// phoneHome registers the plugin with ratd's open registry, retrying until ratd
// is reachable. The HTTP server must already be listening when this runs.
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
