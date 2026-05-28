// Command demo-loader is an example RAT platform + portal plugin: one-click
// sample-data demos.
//
// Each demo is a self-contained bundle of pipeline SQL files plus a manifest
// describing the namespace, pipelines and quality tests it should create.
// Installing a demo calls ratd's HTTP API to create the namespace, create
// each pipeline, write its SQL file, create its quality tests, and submit
// the initial bronze runs so the synthetic data is populated immediately.
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
//	GRPC_PORT    port to serve on             (default 50097)
//	PLUGIN_NAME  registered plugin name       (default demo-loader)
//	PLUGIN_ADDR  address ratd dials back      (default demo-loader:50097)
package main

import (
	_ "embed"
	"embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	sdk "github.com/rat-data/rat/sdk-go"
)

//go:embed bundle.js
var bundleJS []byte

//go:embed all:demos
var demoFiles embed.FS

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

	env := sdk.LoadPluginEnv("demo-loader", "50097", "demo-loader:50097")

	installer := newInstaller(newRatdClient(env.RatdURL), demoFiles)
	a := newAPI(installer, demoFiles)

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting demo-loader plugin", "port", env.Port, "ratd_url", env.RatdURL)

	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
