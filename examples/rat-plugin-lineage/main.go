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
// Environment:
//
//	RATD_URL          ratd base URL              (default http://ratd:8080)
//	RATD_INTERNAL_URL ratd internal base URL     (default = RATD_URL)
//	GRPC_PORT    HTTP port to serve on      (default 50098)
//	PLUGIN_NAME  registered plugin name     (default lineage)
//	PLUGIN_ADDR  address ratd dials back    (default lineage:50098)
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

//go:embed ui/dist/bundle.js
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

	name := envOr("PLUGIN_NAME", "lineage")
	port := envOr("GRPC_PORT", "50098")
	selfAddr := envOr("PLUGIN_ADDR", "lineage:50098")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	ratdInternalURL := envOr("RATD_INTERNAL_URL", ratdURL)

	rat := newRatdClient(ratdURL)
	svc := newLineageService(rat)
	h := newHandler(name, "http://"+selfAddr+"/bundle.js")

	mux := http.NewServeMux()
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	mux.Handle("/", svc.mux())

	slog.Info("starting lineage plugin", "port", port, "ratd_url", ratdURL, "bundle_bytes", len(bundleJS))

	go phoneHome(ratdInternalURL, name, selfAddr)

	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func phoneHome(ratdURL, name, addr string) {
	body, _ := json.Marshal(map[string]string{"name": name, "addr": addr})
	endpoint := ratdURL + "/internal/plugins/register"
	var wasUp bool
	for {
		ok := tryPhone(endpoint, body)
		if ok && !wasUp {
			slog.Info("registered with ratd", "endpoint", endpoint)
		} else if !ok && wasUp {
			slog.Warn("ratd registration failed — will retry")
		}
		wasUp = ok
		if ok {
			time.Sleep(60 * time.Second)
		} else {
			time.Sleep(5 * time.Second)
		}
	}
}

func tryPhone(endpoint string, body []byte) bool {
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
