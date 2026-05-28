// Command docs-assistant is an example RAT platform + portal plugin: an AI
// documentation writer for datasets.
//
// It is a thin consumer plugin with no LLM code or API keys of its own. It
// assembles a documentation-writer prompt plus the target table's columns and
// an optional data sample, then brokers the request to the ai-provider plugin
// through the interconnect capability broker (capability "ai.chat"), falling
// back to a direct ai-provider call if the broker is absent.
//
// The portal UI is a button rendered into the table-actions slot of the
// table-detail page; clicking it opens a modal of AI-generated suggestions
// the user can edit and save through the core table-metadata API.
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
//	GRPC_PORT    port to serve on             (default 50096)
//	PLUGIN_NAME  registered plugin name       (default docs-assistant)
//	PLUGIN_ADDR  address ratd dials back      (default docs-assistant:50096)
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

	env := sdk.LoadPluginEnv("docs-assistant", "50096", "docs-assistant:50096")

	api := &suggestAPI{ratd: newRatdClient(env.RatdURL)}

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	// restMux serves the REST endpoints that ratd proxies under
	// /api/v1/x/docs-assistant/*. sdk.MountStandardPluginRoutes wraps it with
	// TokenAuth so a peer-to-peer hit from another container on the docker
	// network is rejected with 401.
	restMux := http.NewServeMux()
	restMux.HandleFunc("POST /suggest", api.handle)
	restMux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, restMux)

	slog.Info("starting docs-assistant plugin", "port", env.Port, "ratd_url", env.RatdURL)

	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
