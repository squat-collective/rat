// Command lineage is a RAT plugin: the pipeline-lineage DAG, moved
// out of ratd core into a standalone plugin. Same data model as the
// in-process version, same UX (React Flow), just delivered through
// the plugin system so installations that don't want lineage can
// skip it.
//
// First plugin in this repo to use a real JS bundler: the UI in ui/
// is built with esbuild → dist/bundle.js, then embedded into the Go
// binary via //go:embed. React + ReactDOM are marked external and
// resolved from window.React + window.ReactDOM at runtime, so the
// host portal's React instance is reused (no duplicate copies,
// hooks across the boundary still work).
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
//	GRPC_PORT    HTTP port to serve on      (default 50098)
//	PLUGIN_NAME  registered plugin name     (default lineage)
//	PLUGIN_ADDR  address ratd dials back    (default lineage:50098)
package main

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	sdk "github.com/rat-data/rat/sdk-go"
)

//go:embed ui/dist/bundle.js
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

	env := sdk.LoadPluginEnv("lineage", "50098", "lineage:50098")

	rat := newRatdClient(env.RatdURL)
	svc := newLineageService(rat)

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, svc.mux())

	slog.Info("starting lineage plugin", "port", env.Port, "ratd_url", env.RatdURL, "bundle_bytes", len(bundleJS))

	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
