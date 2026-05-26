// Command agents is a RAT plugin: a registry of "agents" (named personas)
// the chat plugin (and any future consumer) can adopt. An Agent bundles a
// system prompt, an allowed-tool whitelist (namespaced MCP tool names),
// and optional model/temperature overrides. The catalog persists via
// ratd's plugin-config mechanism, so it survives restarts without a
// mounted volume or database schema.
//
// The plugin self-registers two capabilities with the interconnect:
//   - agents.list  →  GET /agents      (catalog snapshot)
//   - agents.get   →  GET /agents/{id} (single agent — note: callers that
//                                       use the broker pass id in the path)
//
// On first run with an empty catalog, the plugin seeds three defaults
// (Generalist, Data Explorer, Analyst) so the chat picker has something
// to switch between out of the box.
//
// Environment:
//
//	RATD_URL     ratd base URL                 (default http://ratd:8080)
//	GRPC_PORT    HTTP port to serve on         (default 50096)
//	PLUGIN_NAME  registered plugin name        (default agents)
//	PLUGIN_ADDR  address ratd dials back       (default agents:50096)
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

	name := envOr("PLUGIN_NAME", "agents")
	port := envOr("GRPC_PORT", "50096")
	selfAddr := envOr("PLUGIN_ADDR", "agents:50096")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")

	cfg := newConfigStore(ratdURL, name)
	st := newStore(cfg)
	// When the polled config changes, refresh the in-memory store.
	cfg.onChange(st.hydrate)

	a := newAPI(st)
	h := newHandler(name, "http://"+selfAddr+"/bundle.js")

	mux := http.NewServeMux()
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	mux.Handle("/", a.mux())

	slog.Info("starting agents plugin", "port", port, "ratd_url", ratdURL)

	ctx := context.Background()
	// Seed + poll must run AFTER phone-home succeeds, because the persist
	// path writes back to ratd and only works once we're in the registry.
	// We chain them in one goroutine so the order is explicit.
	go func() {
		phoneHome(ratdURL, name, selfAddr)
		cfg.refresh(ctx)
		seedIfEmpty(ctx, st)
		cfg.poll(ctx, 15*time.Second)
	}()
	go registerWithInterconnect(ratdURL, name)

	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// seedIfEmpty inserts the default agents on first run, so a fresh install
// has something for the chat picker to show.
func seedIfEmpty(ctx context.Context, st *store) {
	if len(st.list()) > 0 {
		return
	}
	slog.Info("seeding default agents (catalog is empty)")
	for _, a := range seedAgents() {
		if _, err := st.create(ctx, a); err != nil {
			slog.Warn("seed failed for agent", "id", a.ID, "error", err)
		}
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

func registerWithInterconnect(ratdURL, self string) {
	caps := []map[string]string{
		{"name": "agents.list", "provider": self, "method": "GET", "path": "/agents",
			"description": "List all agents (system prompts + tool whitelists + model overrides)."},
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
			slog.Info("registered capability agents.list with interconnect")
			return
		}
	}
	slog.Warn("could not register with interconnect — chat will fall back to direct HTTP (this is optional)")
}
