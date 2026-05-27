// Command diff is a RAT plugin: an activity feed of every change in
// the platform, derived from a 15-second poll of ratd's catalog
// endpoints, plus a row-level Iceberg diff drill-in.
//
// Platform-token auth: handled by sdk.MountStandardPluginRoutes +
// sdk.TokenAuth. The per-startup token (sdk.RandomToken) is advertised
// via Describe; ratd's reverse proxy injects it as X-RAT-Plugin-Token
// on every forwarded call. /bundle.js, /health, and the ConnectRPC
// plugin-service paths stay unauthenticated (see sdk/auth.go for the
// contract).
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	env := sdk.LoadPluginEnv("diff", "50101", "diff:50101")
	nessieURL := envOr("NESSIE_URL", "http://nessie:19120")
	pollSecs := 15
	if v := os.Getenv("DIFF_POLL_SECS"); v != "" {
		if n, err := time.ParseDuration(v + "s"); err == nil && n > 0 {
			pollSecs = int(n / time.Second)
		}
	}

	st := newStore(500) // ring buffer cap
	cfg := newConfigStore(env.RatdURL, env.Name)
	cfg.onChange(st.hydrate)

	ratd := newRatdClient(env.RatdURL)
	nessie := newNessieClient(nessieURL)
	ib := newIcebergDiffer(ratd, nessie)
	p := newPoller(ratd, st, cfg, env.Name, time.Duration(pollSecs)*time.Second)

	platformToken := sdk.RandomToken()

	a := newAPI(st, ib)
	h := newHandler(env.Name, "http://"+env.Addr+"/bundle.js", bundleHash, platformToken)

	mux := http.NewServeMux()
	handler := sdk.MountStandardPluginRoutes(mux, h, bundleJS, platformToken, a.mux())

	slog.Info("starting diff plugin", "port", env.Port, "ratd_url", env.RatdURL, "poll_secs", pollSecs)

	ctx := context.Background()
	go func() {
		sdk.PhoneHomeLoop(env.RatdInternalURL, env.Name, env.Addr)
		cfg.refresh(ctx)
		go p.run(ctx)
		cfg.poll(ctx, 30*time.Second)
	}()

	server := &http.Server{Addr: ":" + env.Port, Handler: sdk.H2CHandler(handler)}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
