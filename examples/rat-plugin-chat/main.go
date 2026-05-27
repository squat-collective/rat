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
// Platform-token auth (see DescribeResponse.platform_token in
// proto/plugin/v1/plugin.proto): on startup we generate a fresh
// 32-byte hex token, advertise it via Describe, and wrap the REST mux
// with middleware that rejects any inbound request lacking the
// matching X-RAT-Plugin-Token header. ratd's reverse proxy reads the
// token from the registry and injects it on every forwarded call, so
// traffic via /api/v1/x/chat/* keeps working — but a direct
// peer-to-peer call to http://chat:50095/... from another container
// on the docker network now gets 401. /bundle.js, /health, and the
// ConnectRPC plugin-service paths stay unauthenticated.
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

	name := envOr("PLUGIN_NAME", "chat")
	port := envOr("GRPC_PORT", "50095")
	selfAddr := envOr("PLUGIN_ADDR", "chat:50095")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	ratdInternalURL := envOr("RATD_INTERNAL_URL", ratdURL)

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

	continuations := newContinuationStore()
	orch := newOrchestrator(ratdURL, mcp, disco, agents, subRuns, continuations)
	a := newAPI(disco, orch, cfg, agents, convs, subRuns, continuations)

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
	// REST endpoints — wrapped. /health inside this mux is allow-listed.
	mux.Handle("/", tokenAuth(platformToken, a.mux()))

	slog.Info("starting chat plugin", "port", port, "ratd_url", ratdURL)

	ctx := context.Background()
	go phoneHome(ratdInternalURL, name, selfAddr)
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
