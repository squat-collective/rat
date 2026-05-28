// Command interconnect is an example RAT platform + portal plugin: a plugin
// interconnection broker.
//
// It turns plugin-to-plugin wiring into a first-class mechanism. Plugins
// register named *capabilities* they offer; any plugin can then invoke a
// capability by name and the broker routes the call to a healthy provider —
// no hardcoded plugin names or routes. The portal UI draws the live "plugin
// mesh": every plugin, its health, and how capabilities wire them together.
//
// Platform-token auth: handled by sdk.MountStandardPluginRoutes +
// sdk.TokenAuth. The per-startup token (sdk.RandomToken) is advertised
// via Describe; ratd's reverse proxy reads it from the registry and
// injects it as X-RAT-Plugin-Token on every forwarded call. /bundle.js,
// /health, and the ConnectRPC plugin-service paths stay unauthenticated
// (see sdk/auth.go for the contract).
//
// Important note for capability invocation: other plugins do not call
// the interconnect's REST surface directly — they go through ratd's
// reverse proxy at /api/v1/x/interconnect/register and /invoke, which
// stamps the platform token on the way in. So the token-auth wrap is
// transparent to the broker's normal operation.
//
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT    port to serve on             (default 50093)
//	PLUGIN_NAME  registered plugin name       (default interconnect)
//	PLUGIN_ADDR  address ratd dials back      (default interconnect:50093)
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

	env := sdk.LoadPluginEnv("interconnect", "50093", "interconnect:50093")

	st := newStore()
	// Self-register one capability so the broker is testable out of the box —
	// interconnect dogfoods its own directory.
	st.register(&Capability{
		Name:        "mesh.describe",
		Provider:    env.Name,
		Method:      "GET",
		Path:        "/mesh",
		Description: "The live plugin mesh — interconnect's directory of plugins and capabilities.",
	})

	a := newAPI(st, newRatdClient(env.RatdURL))

	platformToken := sdk.RandomToken()
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting interconnect plugin", "port", env.Port, "ratd_url", env.RatdURL)

	go sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
