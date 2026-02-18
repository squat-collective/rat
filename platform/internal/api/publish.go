package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// publishRequest is the optional JSON body for POST .../publish.
type publishRequest struct {
	Message string `json:"message"`
}

// MountPublishRoutes registers the publish endpoint on the router.
func MountPublishRoutes(r chi.Router, srv *Server) {
	r.Post("/pipelines/{namespace}/{layer}/{name}/publish", srv.HandlePublishPipeline)
}

// HandlePublishPipeline snapshots the current HEAD S3 version IDs as the
// "published" versions for a pipeline. Creates a version history record.
func (s *Server) HandlePublishPipeline(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	// Parse optional message from body
	var req publishRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // ignore errors — body is optional
	}

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

	// Validate templates if executor is available (soft dependency)
	if s.Executor != nil {
		result, err := s.Executor.ValidatePipeline(r.Context(), pipeline)
		if err != nil {
			// Runner unavailable — log and proceed (don't block publish)
			slog.Warn("template validation skipped: runner unavailable", "error", err)
		} else if !result.Valid {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"error":      "template validation failed",
				"validation": result,
			})
			return
		}
	}

	// List all files under the pipeline's S3 prefix
	prefix := namespace + "/pipelines/" + layer + "/" + name + "/"
	files, err := s.Storage.ListFiles(r.Context(), prefix)
	if err != nil {
		internalError(w, "failed to list pipeline files", err)
		return
	}

	// Snapshot the current HEAD version ID for each file
	versions := make(map[string]string, len(files))
	for _, f := range files {
		info, err := s.Storage.StatFile(r.Context(), f.Path)
		if err != nil || info == nil {
			continue
		}
		if info.VersionID != "" {
			versions[f.Path] = info.VersionID
		}
	}

	// Determine version number (needed regardless of tx path)
	var versionNumber int
	if s.Versions != nil {
		latest, err := s.Versions.LatestVersionNumber(r.Context(), pipeline.ID)
		if err != nil {
			internalError(w, "failed to get latest version number", err)
			return
		}
		versionNumber = latest + 1
	}

	maxVersions := pipeline.MaxVersions
	if maxVersions <= 0 {
		maxVersions = 50
	}

	if s.Versions != nil {
		pv := &domain.PipelineVersion{
			PipelineID:        pipeline.ID,
			VersionNumber:     versionNumber,
			Message:           req.Message,
			PublishedVersions: versions,
		}

		if s.Publisher != nil {
			// Transactional path: publish + version + prune in one atomic operation.
			if err := s.Publisher.PublishPipelineTx(r.Context(), namespace, layer, name, versions, pv, maxVersions); err != nil {
				internalError(w, "failed to publish pipeline", err)
				return
			}
		} else {
			// Non-transactional fallback (e.g. tests without a real DB).
			if err := s.Pipelines.PublishPipeline(r.Context(), namespace, layer, name, versions); err != nil {
				internalError(w, "failed to publish pipeline", err)
				return
			}
			if err := s.Versions.CreateVersion(r.Context(), pv); err != nil {
				internalError(w, "failed to create version record", err)
				return
			}
			if err := s.Versions.PruneVersions(r.Context(), pipeline.ID, maxVersions); err != nil {
				internalError(w, "failed to prune old versions", err)
				return
			}
		}
	} else {
		// No version store — just update the pipeline's published state.
		if err := s.Pipelines.PublishPipeline(r.Context(), namespace, layer, name, versions); err != nil {
			internalError(w, "failed to publish pipeline", err)
			return
		}
	}

	// Invalidate pipeline cache after publish changes published_versions.
	if s.PipelineCache != nil {
		s.PipelineCache.Delete(pipelineCacheKey(namespace, layer, name))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "published",
		"version":  versionNumber,
		"message":  req.Message,
		"versions": versions,
	})
}
