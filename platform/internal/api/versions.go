package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// rollbackRequest is the JSON body for POST .../rollback.
type rollbackRequest struct {
	Version int    `json:"version"`
	Message string `json:"message"`
}

// MountVersionRoutes registers version history and rollback endpoints.
func MountVersionRoutes(r chi.Router, srv *Server) {
	r.Get("/pipelines/{namespace}/{layer}/{name}/versions", srv.HandleListVersions)
	r.Get("/pipelines/{namespace}/{layer}/{name}/versions/{number}", srv.HandleGetVersion)
	r.Post("/pipelines/{namespace}/{layer}/{name}/rollback", srv.HandleRollback)
}

// HandleListVersions returns the version history for a pipeline.
func (s *Server) HandleListVersions(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	pipeline, err := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	versions, err := s.Versions.ListVersions(r.Context(), pipeline.ID)
	if err != nil {
		internalError(w, "failed to list versions", err)
		return
	}

	// Ensure non-null JSON array
	if versions == nil {
		versions = []domain.PipelineVersion{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"versions": versions,
		"total":    len(versions),
	})
}

// HandleGetVersion returns a single version by number.
func (s *Server) HandleGetVersion(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")
	numberStr := chi.URLParam(r, "number")

	number, err := strconv.Atoi(numberStr)
	if err != nil || number < 1 {
		errorJSON(w, "invalid version number", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	pipeline, err := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	version, err := s.Versions.GetVersion(r.Context(), pipeline.ID, number)
	if err != nil {
		internalError(w, "failed to get version", err)
		return
	}
	if version == nil {
		errorJSON(w, "version not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, version)
}

// HandleRollback creates a new version that re-pins an old version's snapshot.
func (s *Server) HandleRollback(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.Version < 1 {
		errorJSON(w, "version must be a positive integer", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	pipeline, err := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Look up the target version's snapshot
	targetVersion, err := s.Versions.GetVersion(r.Context(), pipeline.ID, req.Version)
	if err != nil {
		internalError(w, "failed to get target version", err)
		return
	}
	if targetVersion == nil {
		errorJSON(w, "version not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Get next version number
	latest, err := s.Versions.LatestVersionNumber(r.Context(), pipeline.ID)
	if err != nil {
		internalError(w, "failed to get latest version number", err)
		return
	}
	newVersionNumber := latest + 1

	// Default message
	message := req.Message
	if message == "" {
		message = fmt.Sprintf("Rollback to v%d", req.Version)
	}

	maxVersions := pipeline.MaxVersions
	if maxVersions <= 0 {
		maxVersions = 50
	}

	// Create new version record with the old snapshot
	pv := &domain.PipelineVersion{
		PipelineID:        pipeline.ID,
		VersionNumber:     newVersionNumber,
		Message:           message,
		PublishedVersions: targetVersion.PublishedVersions,
	}

	if s.Publisher != nil {
		// Transactional path: version + publish + prune in one atomic operation.
		if err := s.Publisher.RollbackPipelineTx(r.Context(), namespace, layer, name, targetVersion.PublishedVersions, pv, maxVersions); err != nil {
			internalError(w, "failed to rollback pipeline", err)
			return
		}
	} else {
		// Non-transactional fallback (e.g. tests without a real DB).
		if err := s.Versions.CreateVersion(r.Context(), pv); err != nil {
			internalError(w, "failed to create rollback version", err)
			return
		}
		if err := s.Pipelines.PublishPipeline(r.Context(), namespace, layer, name, targetVersion.PublishedVersions); err != nil {
			internalError(w, "failed to apply rollback", err)
			return
		}
		if err := s.Versions.PruneVersions(r.Context(), pipeline.ID, maxVersions); err != nil {
			internalError(w, "failed to prune old versions", err)
			return
		}
	}

	// Invalidate pipeline cache after rollback changes published_versions.
	if s.PipelineCache != nil {
		s.PipelineCache.Delete(pipelineCacheKey(namespace, layer, name))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "rolled_back",
		"from_version": req.Version,
		"new_version":  newVersionNumber,
		"message":      message,
	})
}
