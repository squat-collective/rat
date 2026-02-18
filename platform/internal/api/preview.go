package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// PreviewRequest is the optional JSON body for POST /api/v1/pipelines/{namespace}/{layer}/{name}/preview.
type PreviewRequest struct {
	Limit       int      `json:"limit"`
	SampleFiles []string `json:"sample_files,omitempty"`
	Code        string   `json:"code,omitempty"`
}

// MountPreviewRoutes registers the preview endpoint on the router.
func MountPreviewRoutes(r chi.Router, srv *Server) {
	r.Post("/pipelines/{namespace}/{layer}/{name}/preview", srv.HandlePreviewPipeline)
}

// HandlePreviewPipeline executes a pipeline in preview mode (dry-run).
// Returns sample rows, profiling stats, EXPLAIN ANALYZE, and logs — without
// writing to the data lake or creating a run record.
func (s *Server) HandlePreviewPipeline(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	// Verify pipeline exists
	pipeline, err := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Parse optional request body
	var req PreviewRequest
	req.Limit = 100 // default
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
	}

	// Allow limit override via query param
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			req.Limit = l
		}
	}

	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	if s.Executor == nil {
		errorJSON(w, "executor not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	result, err := s.Executor.Preview(r.Context(), pipeline, req.Limit, req.SampleFiles, req.Code)
	if err != nil {
		slog.Error("preview failed", "pipeline", namespace+"/"+layer+"/"+name, "error", err)
		errorJSON(w, "preview execution failed", "INTERNAL", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
