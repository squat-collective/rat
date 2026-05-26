// Command mcp-sql is a RAT plugin that exposes the warehouse as a real
// Model Context Protocol (MCP) server. The chat plugin (and any MCP client)
// can connect to /mcp, list this server's tools, and run read-only SQL.
//
// Tools: run_query, sample_table, explain_query — all read-only. Any
// statement that's not SELECT/WITH/SHOW/EXPLAIN/DESCRIBE/PRAGMA is rejected
// before it reaches ratd. The plugin self-registers an "mcp.server.sql"
// capability with the interconnect plugin so the chat plugin discovers it
// without hardcoded URLs.
//
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT    HTTP port to serve on         (default 50101)
//	PLUGIN_NAME  registered plugin name        (default mcp-sql)
//	PLUGIN_ADDR  address ratd dials back       (default mcp-sql:50101)
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

	name := envOr("PLUGIN_NAME", "mcp-sql")
	port := envOr("GRPC_PORT", "50101")
	selfAddr := envOr("PLUGIN_ADDR", "mcp-sql:50101")
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

	slog.Info("starting mcp-sql", "port", port, "ratd_url", ratdURL, "tools", len(mcp.tools))

	go phoneHome(ratdInternalURL, name, selfAddr)
	go registerWithInterconnect(ratdURL, name)

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

// Runs forever: re-registers every 60s once steady-state. Same shape
// as rat-plugin-mcp-docs — see that file for the why.
func registerWithInterconnect(ratdURL, self string) {
	cap := map[string]string{
		"name": "mcp.server.sql", "provider": self, "method": "POST", "path": "/mcp",
		"description": "MCP server: read-only SQL against the warehouse (DuckDB via ratq)",
	}
	endpoint := ratdURL + "/api/v1/x/interconnect/register"

	var wasUp bool
	for {
		ok := tryRegister(endpoint, cap)
		if ok && !wasUp {
			slog.Info("registered mcp.server.sql with interconnect")
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
