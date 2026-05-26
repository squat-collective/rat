// Command chat is a RAT plugin: a chat UI that talks to any LLM (via
// rat-plugin-ai-provider) and can call MCP tools served by any plugin that
// registers an "mcp.server.*" capability with the interconnect.
//
// Architecture:
//   - The discoverer polls the interconnect for "mcp.server.*" capabilities,
//     calls initialize + tools/list on each, and caches the result.
//   - The orchestrator runs the tool-use loop: ask ai-provider to chat
//     with the discovered tools attached, execute any tool_calls via the
//     matching MCP server, feed results back, repeat.
//   - The portal UI is a dedicated /x/chat page (a "Chat" entry in the
//     sidebar nav) showing the conversation, with tool calls and results
//     rendered inline as cards.
//
// Environment:
//
//	RATD_URL     ratd base URL                 (default http://ratd:8080)
//	GRPC_PORT    HTTP port to serve on         (default 50095)
//	PLUGIN_NAME  registered plugin name        (default chat)
//	PLUGIN_ADDR  address ratd dials back       (default chat:50095)
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

	name := envOr("PLUGIN_NAME", "chat")
	port := envOr("GRPC_PORT", "50095")
	selfAddr := envOr("PLUGIN_ADDR", "chat:50095")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")

	cfg := newConfigStore(ratdURL, name, chatConfig{})
	mcp := newMCPClient(ratdURL)
	disco := newDiscoverer(ratdURL, mcp)
	agents := newAgentsClient(ratdURL)

	convDir := envOr("CONVERSATIONS_DIR", "/data/conversations")
	convs, err := newConversationStore(convDir)
	if err != nil {
		slog.Error("conversation store init failed", "dir", convDir, "error", err)
		os.Exit(1)
	}
	slog.Info("conversation store ready", "dir", convDir, "loaded", len(convs.list()))

	subRunDir := envOr("SUBAGENT_RUNS_DIR", "/data/subagent_runs")
	subRuns, err := newSubagentRunStore(subRunDir)
	if err != nil {
		slog.Error("subagent run store init failed", "dir", subRunDir, "error", err)
		os.Exit(1)
	}
	slog.Info("subagent run store ready", "dir", subRunDir)

	orch := newOrchestrator(ratdURL, mcp, disco, agents, subRuns)
	a := newAPI(disco, orch, cfg, agents, convs, subRuns)
	h := newHandler(name, "http://"+selfAddr+"/bundle.js")

	mux := http.NewServeMux()
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	mux.Handle("/", a.mux())

	slog.Info("starting chat plugin", "port", port, "ratd_url", ratdURL)

	ctx := context.Background()
	go phoneHome(ratdURL, name, selfAddr)
	go cfg.poll(ctx, 15*time.Second)
	// Initial discovery is fast (no MCP servers yet means an empty loop) and
	// re-runs every 15s so new MCP servers show up live without restarting.
	go disco.poll(ctx, 15*time.Second)

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
