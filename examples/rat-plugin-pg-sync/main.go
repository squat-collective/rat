// Command pg-sync is a RAT plugin that mirrors external Postgres tables
// into the Iceberg lake by generating SQL pipelines + cron schedules
// against ratd. Connection URLs are pulled from rat-plugin-secrets via
// the interconnect broker, so credentials never live in this plugin's
// state.
//
// Platform-token auth: handled by sdk.MountStandardPluginRoutes +
// sdk.TokenAuth. The per-startup token (sdk.RandomToken) is advertised
// via Describe; ratd's reverse proxy injects it as X-RAT-Plugin-Token
// on every forwarded call. /bundle.js, /health, and the ConnectRPC
// plugin-service paths stay unauthenticated (see sdk/auth.go for the
// contract).
//
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT     HTTP port to serve on      (default 50100)
//	PLUGIN_NAME   registered plugin name     (default pg-sync)
//	PLUGIN_ADDR   address ratd dials back    (default pg-sync:50100)
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

	env := sdk.LoadPluginEnv("pg-sync", "50100", "pg-sync:50100")

	st := newStore()
	cfg := newConfigStore(env.RatdURL, env.Name)
	cfg.onChange(st.hydrate)

	secrets := newSecretsClient(env.RatdURL)
	ratd := newRatdClient(env.RatdURL)
	engine := newSyncEngine(ratd, secrets, st)

	globalStoreLookup = st.getConnection // wire the sql.renderer to the store

	platformToken := sdk.RandomToken()

	a := newAPI(st, cfg, engine, secrets)
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting pg-sync plugin", "port", env.Port, "ratd_url", env.RatdURL)

	ctx := context.Background()
	go func() {
		sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
		cfg.refresh(ctx)
		cfg.poll(ctx, 15*time.Second)
	}()

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
