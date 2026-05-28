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

	sdk "github.com/rat-data/rat/sdk-go"
)

//go:embed bundle.js
var bundleJS []byte

// bundleHash is computed once at startup over the go:embed'd bundle.js.
// It surfaces in Describe()'s UI descriptor so the portal can set
// <script integrity="sha256-…"> and the browser rejects any tampered
// bundle delivered through the ratd reverse proxy.
var bundleHash = sdk.SRIHash(bundleJS)

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

	env := sdk.LoadPluginEnv("agents", "50096", "agents:50096")

	cfg := newConfigStore(env.RatdURL, env.Name)
	st := newStore(cfg)
	// When the polled config changes, refresh the in-memory store.
	cfg.onChange(st.hydrate)

	platformToken := sdk.RandomToken()

	a := newAPI(st)
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting agents plugin", "port", env.Port, "ratd_url", env.RatdURL)

	ctx := context.Background()
	// Seed + poll must run AFTER phone-home succeeds, because the persist
	// path writes back to ratd and only works once we're in the registry.
	// We chain them in one goroutine so the order is explicit.
	go func() {
		sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
		cfg.refresh(ctx)
		seedIfEmpty(ctx, st)
		cfg.poll(ctx, 15*time.Second)
	}()
	go registerWithInterconnect(env.RatdURL, env.Name)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
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

// Runs forever: re-registers every 60s once steady-state. Same pattern
// as the MCP plugins — protects against interconnect restart wiping
// capabilities.
func registerWithInterconnect(ratdURL, self string) {
	caps := []map[string]string{
		{"name": "agents.list", "provider": self, "method": "GET", "path": "/agents",
			"description": "List all agents (system prompts + tool whitelists + model overrides)."},
	}
	endpoint := ratdURL + "/api/v1/x/interconnect/register"

	var wasUp bool
	for {
		ok := tryRegisterAll(endpoint, caps)
		if ok && !wasUp {
			slog.Info("registered capabilities with interconnect", "count", len(caps))
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

func tryRegisterAll(endpoint string, caps []map[string]string) bool {
	for _, c := range caps {
		body, _ := json.Marshal(c)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			cancel()
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil || resp.StatusCode >= 300 {
			if resp != nil {
				_ = resp.Body.Close()
			}
			return false
		}
		_ = resp.Body.Close()
	}
	return true
}
