// Command mcp-docs is a RAT plugin that exposes the data catalog as a real
// Model Context Protocol (MCP) server. Any MCP-compatible AI client — the
// rat-plugin-chat included — can connect to /mcp, list this server's tools,
// and call them to learn about the warehouse.
//
// Tools: list_namespaces, list_tables, get_table_schema, get_table_description,
// describe_warehouse. All read-only — this plugin never writes back.
//
// The plugin self-registers an "mcp.server.docs" capability with the
// interconnect plugin (if present); the chat plugin discovers MCP servers by
// looking up capabilities under that prefix.
//
// Platform-token auth (see DescribeResponse.platform_token in
// proto/plugin/v1/plugin.proto): on startup we generate a fresh
// 32-byte hex token, advertise it via Describe, and wrap the REST
// surface (notably /mcp) with middleware that rejects any inbound
// request lacking the matching X-RAT-Plugin-Token header. ratd's
// reverse proxy reads the token from the registry and injects it on
// every forwarded call, so capability invocations brokered through
// the interconnect keep working — but a direct peer-to-peer call to
// http://mcp-docs:50100/mcp from another container on the docker
// network now gets 401. /health and the ConnectRPC plugin-service
// paths stay unauthenticated. No UI bundle ⇒ no bundle_hash.
//
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT    HTTP port to serve on         (default 50100)
//	PLUGIN_NAME  registered plugin name        (default mcp-docs)
//	PLUGIN_ADDR  address ratd dials back       (default mcp-docs:50100)
package main

import (
	"bytes"
	"context"
	"crypto/rand"
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
// portal's <script> tag can't add custom headers — this plugin has no
// bundle but we keep the carve-out for consistency) and /health (used
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

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	name := envOr("PLUGIN_NAME", "mcp-docs")
	port := envOr("GRPC_PORT", "50100")
	selfAddr := envOr("PLUGIN_ADDR", "mcp-docs:50100")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	ratdInternalURL := envOr("RATD_INTERNAL_URL", ratdURL)

	rat := newRatdClient(ratdURL)
	mcp := NewServer(name, pluginVersion)
	registerTools(mcp, rat)

	platformToken := randomToken()
	h := newHandler(name, platformToken)

	// restMux serves the REST endpoints that ratd proxies under
	// /api/v1/x/mcp-docs/*. We wrap it with tokenAuth so a
	// peer-to-peer hit from another container on the docker network is
	// rejected with 401.
	restMux := http.NewServeMux()
	restMux.Handle("/mcp", mcp)
	restMux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux := http.NewServeMux()
	// ConnectRPC plugin-service paths — NOT wrapped: ratd reaches
	// these via direct gRPC, not the reverse proxy, and learns the
	// token from Describe over this very channel.
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	// REST endpoints — wrapped. /health inside restMux is allow-listed
	// by tokenAuth so container liveness probes still work.
	mux.Handle("/", tokenAuth(platformToken, restMux))

	slog.Info("starting mcp-docs", "port", port, "ratd_url", ratdURL, "tools", len(mcp.tools))

	go phoneHome(ratdInternalURL, name, selfAddr)
	go registerWithInterconnect(ratdURL, name)

	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// phoneHome registers the plugin with ratd's open registry, retrying until
// ratd is reachable.
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
		slog.Warn("phone-home rejected", "status", resp.StatusCode)
	}
	slog.Error("phone-home failed after retries")
}

// registerWithInterconnect publishes our MCP endpoint as a brokered
// capability so the chat plugin can discover it without hardcoding URLs.
// The "mcp.server." prefix is the convention chat scans for.
//
// Runs forever: re-registers every 60s once steady-state. This is the
// belt-and-braces fix for "interconnect restarted and silently lost
// every capability" — within 60s, the plugin re-advertises itself.
func registerWithInterconnect(ratdURL, self string) {
	cap := map[string]string{
		"name": "mcp.server.docs", "provider": self, "method": "POST", "path": "/mcp",
		"description": "MCP server: RAT catalog & metadata (list/describe tables, descriptions)",
	}
	endpoint := ratdURL + "/api/v1/x/interconnect/register"

	var wasUp bool
	for {
		ok := tryRegister(endpoint, cap)
		if ok && !wasUp {
			slog.Info("registered mcp.server.docs with interconnect")
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

// tryRegister does one POST /register attempt. Returns true on 2xx.
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
