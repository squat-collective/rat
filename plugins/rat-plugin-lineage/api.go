package main

import (
	"context"
	"net/http"
	"time"
)

// lineageService is the HTTP wrapper around the graph builder.
type lineageService struct {
	ratd *ratdClient
}

func newLineageService(ratd *ratdClient) *lineageService {
	return &lineageService{ratd: ratd}
}

func (l *lineageService) mux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /graph", l.handleGraph)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return m
}

// handleGraph returns the lineage DAG for the requested namespace
// (or all namespaces if `namespace` query param is omitted).
func (l *lineageService) handleGraph(w http.ResponseWriter, r *http.Request) {
	nsFilter := r.URL.Query().Get("namespace")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	g, err := l.buildGraph(ctx, nsFilter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to build lineage: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, g)
}
