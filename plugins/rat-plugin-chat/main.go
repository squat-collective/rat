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
//	GRPC_PORT    HTTP port to serve on         (default 50095)
//	PLUGIN_NAME  registered plugin name        (default chat)
//	PLUGIN_ADDR  address ratd dials back       (default chat:50095)
package main

import (
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

	env := sdk.LoadPluginEnv("chat", "50095", "chat:50095")

	cfg := newConfigStore(env.RatdURL, env.Name, chatConfig{})
	mcp := newMCPClient(env.RatdURL)
	disco := newDiscoverer(env.RatdURL, mcp)
	agents := newAgentsClient(env.RatdURL)

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

	continuations := newContinuationStore()
	orch := newOrchestrator(env.RatdURL, mcp, disco, agents, subRuns, continuations)
	a := newAPI(disco, orch, cfg, agents, convs, subRuns, continuations)

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting chat plugin", "port", env.Port, "ratd_url", env.RatdURL)

	ctx := context.Background()
	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
	go cfg.poll(ctx, 15*time.Second)
	// Initial discovery is fast (no MCP servers yet means an empty loop) and
	// re-runs every 15s so new MCP servers show up live without restarting.
	go disco.poll(ctx, 15*time.Second)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
