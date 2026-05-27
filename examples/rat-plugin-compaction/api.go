package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// api.go exposes the plugin's HTTP surface that the portal UI consumes.
// All routes are mounted under /api/v1/x/compaction/* by ratd's reverse
// proxy; the platform_token middleware (sdk-go) is already in front of
// us when requests arrive here.

type api struct {
	det *detector
	cmp *compactor
}

func newAPI(det *detector, cmp *compactor) *api {
	return &api{det: det, cmp: cmp}
}

func (a *api) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /tables", a.handleListTables)
	m.HandleFunc("POST /scan", a.handleScan)
	m.HandleFunc("POST /tables/{namespace}/{layer}/{name}/compact", a.handleCompact)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

func (a *api) handleListTables(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"tables": a.det.snapshot(),
	})
}

func (a *api) handleScan(w http.ResponseWriter, r *http.Request) {
	// Run synchronously so the UI's optimistic refresh sees fresh data.
	// Detection completes in seconds for typical workloads.
	if err := a.det.scan(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "scanned"})
}

func (a *api) handleCompact(w http.ResponseWriter, r *http.Request) {
	ns := r.PathValue("namespace")
	layer := r.PathValue("layer")
	name := r.PathValue("name")
	if ns == "" || layer == "" || name == "" {
		writeErr(w, http.StatusBadRequest, "namespace, layer, and name are required")
		return
	}

	// Fire-and-forget — compaction can take minutes on a 100k+ row table.
	// Return 202 immediately; the UI polls /tables for status transitions.
	go func() {
		ctx := context.Background()
		_, _ = a.cmp.Compact(ctx, ns, layer, name)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "scheduled",
		"target":  ns + "." + layer + "." + name,
		"message": "compaction running in the background — poll /tables for status",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
