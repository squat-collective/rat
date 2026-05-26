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

	h := newHandler(name)
	mux := http.NewServeMux()
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	mux.Handle("/mcp", mcp)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

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
