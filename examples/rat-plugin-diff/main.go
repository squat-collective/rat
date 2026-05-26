// Command diff is a RAT plugin: an activity feed of every change in
// the platform, derived from a 15-second poll of ratd's catalog
// endpoints, plus a row-level Iceberg diff drill-in.
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

	name := envOr("PLUGIN_NAME", "diff")
	port := envOr("GRPC_PORT", "50101")
	selfAddr := envOr("PLUGIN_ADDR", "diff:50101")
	ratdURL := envOr("RATD_URL", "http://ratd:8080")
	nessieURL := envOr("NESSIE_URL", "http://nessie:19120")
	pollSecs := 15
	if v := os.Getenv("DIFF_POLL_SECS"); v != "" {
		if n, err := time.ParseDuration(v + "s"); err == nil && n > 0 {
			pollSecs = int(n / time.Second)
		}
	}

	st := newStore(500) // ring buffer cap
	cfg := newConfigStore(ratdURL, name)
	cfg.onChange(st.hydrate)

	ratd := newRatdClient(ratdURL)
	nessie := newNessieClient(nessieURL)
	ib := newIcebergDiffer(ratd, nessie)
	p := newPoller(ratd, st, cfg, name, time.Duration(pollSecs)*time.Second)

	a := newAPI(st, ib)
	h := newHandler(name, "http://"+selfAddr+"/bundle.js")

	mux := http.NewServeMux()
	pluginPath, pluginHTTP := pluginv1connect.NewPluginServiceHandler(h)
	mux.Handle(pluginPath, pluginHTTP)
	mux.HandleFunc("/bundle.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundleJS)
	})
	mux.Handle("/", a.mux())

	slog.Info("starting diff plugin", "port", port, "ratd_url", ratdURL, "poll_secs", pollSecs)

	ctx := context.Background()
	go func() {
		phoneHome(ratdURL, name, selfAddr)
		cfg.refresh(ctx)
		go p.run(ctx)
		cfg.poll(ctx, 30*time.Second)
	}()

	server := &http.Server{Addr: ":" + port, Handler: h2c.NewHandler(mux, &http2.Server{})}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
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
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			slog.Info("registered with ratd")
			return
		}
	}
	slog.Error("phone-home failed after retries")
}
