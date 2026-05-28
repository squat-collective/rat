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
// Platform-token auth: handled by sdk.MountStandardPluginRoutes +
// sdk.TokenAuth. The per-startup token (sdk.RandomToken) is advertised
// via Describe; ratd's reverse proxy reads it from the registry and
// injects it as X-RAT-Plugin-Token on every forwarded call. /health and
// the ConnectRPC plugin-service paths stay unauthenticated. No UI
// bundle ⇒ no bundle_hash (see sdk/auth.go for the contract).
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
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	sdk "github.com/rat-data/rat/sdk-go"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	env := sdk.LoadPluginEnv("mcp-docs", "50100", "mcp-docs:50100")

	rat := newRatdClient(env.RatdURL)
	mcp := NewServer(env.Name, pluginVersion)
	registerTools(mcp, rat)

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, platformToken)

	// restMux serves the REST endpoints that ratd proxies under
	// /api/v1/x/mcp-docs/*. sdk.MountStandardPluginRoutes wraps it with
	// TokenAuth so a peer-to-peer hit from another container on the docker
	// network is rejected with 401.
	restMux := http.NewServeMux()
	restMux.Handle("/mcp", mcp)
	restMux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux := http.NewServeMux()
	// No UI bundle: pass nil so MountStandardPluginRoutes skips /bundle.js.
	handler := sdk.MountStandardPluginRoutes(mux, h, nil, platformToken, restMux)

	slog.Info("starting mcp-docs", "port", env.Port, "ratd_url", env.RatdURL, "tools", len(mcp.tools))

	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
	go registerWithInterconnect(env.RatdURL, env.Name)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
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
