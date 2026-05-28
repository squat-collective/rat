// Command dev-assistant is an example RAT platform + portal plugin: an AI dev
// assistant docked into the pipeline editor.
//
// It is a thin consumer plugin — it has no LLM code or API keys of its own.
// It assembles a RAT pipeline-development prompt plus the user's editing
// context, then brokers the request to the ai-provider plugin through the
// interconnect capability broker (capability "ai.chat"), falling back to a
// direct ai-provider call if the broker is absent.
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
//	GRPC_PORT    port to serve on             (default 50095)
//	PLUGIN_NAME  registered plugin name       (default dev-assistant)
//	PLUGIN_ADDR  address ratd dials back      (default dev-assistant:50095)
package main

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

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

	env := sdk.LoadPluginEnv("dev-assistant", "50095", "dev-assistant:50095")

	api := &chatAPI{ratd: newRatdClient(env.RatdURL)}

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	// restMux serves the REST endpoints that ratd proxies under
	// /api/v1/x/dev-assistant/*. sdk.MountStandardPluginRoutes wraps it with
	// TokenAuth so a peer-to-peer hit from another container on the docker
	// network is rejected with 401.
	restMux := http.NewServeMux()
	restMux.HandleFunc("POST /chat", api.handle)
	restMux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, restMux)

	slog.Info("starting dev-assistant plugin", "port", env.Port, "ratd_url", env.RatdURL)

	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
