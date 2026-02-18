package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// MountMetadataRoutes registers metadata endpoints on the router.
// Metadata is read from .meta.yaml sidecar files in S3.
// These are nested under the pipeline resource for consistent URL hierarchy.
func MountMetadataRoutes(r chi.Router, srv *Server) {
	// New canonical URLs (nested under pipeline resource).
	r.Get("/pipelines/{namespace}/{layer}/{name}/metadata", srv.HandleGetPipelineMeta)
	r.Get("/pipelines/{namespace}/{layer}/{name}/metadata/quality", srv.HandleGetQualityMeta)

	// Legacy URLs kept for backward compatibility — will be removed in a future version.
	// See docs/api-deprecation-strategy.md for deprecation policy.
	r.Get("/metadata/{namespace}/pipeline/{layer}/{name}", srv.HandleGetPipelineMeta)
	r.Get("/metadata/{namespace}/quality/{layer}/{name}", srv.HandleGetQualityMeta)
}

// HandleGetPipelineMeta reads pipeline.meta.yaml for a pipeline.
func (s *Server) HandleGetPipelineMeta(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	path := namespace + "/pipelines/" + layer + "/" + name + "/pipeline.meta.yaml"
	file, err := s.Storage.ReadFile(r.Context(), path)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if file == nil {
		errorJSON(w, "metadata not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    path,
		"content": file.Content,
	})
}

// HandleGetQualityMeta reads quality.meta.yaml for a pipeline's tests.
func (s *Server) HandleGetQualityMeta(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	path := namespace + "/pipelines/" + layer + "/" + name + "/tests/quality.meta.yaml"
	file, err := s.Storage.ReadFile(r.Context(), path)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if file == nil {
		errorJSON(w, "metadata not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    path,
		"content": file.Content,
	})
}
