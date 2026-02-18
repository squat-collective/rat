package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// PipelineStore defines the persistence interface for pipelines.
// Implemented by postgres store (production) and memory store (tests).
type PipelineStore interface {
	ListPipelines(ctx context.Context, filter PipelineFilter) ([]domain.Pipeline, error)
	CountPipelines(ctx context.Context, filter PipelineFilter) (int, error)
	GetPipeline(ctx context.Context, namespace, layer, name string) (*domain.Pipeline, error)
	GetPipelineByID(ctx context.Context, id string) (*domain.Pipeline, error)
	CreatePipeline(ctx context.Context, p *domain.Pipeline) error
	UpdatePipeline(ctx context.Context, namespace, layer, name string, update UpdatePipelineRequest) (*domain.Pipeline, error)
	DeletePipeline(ctx context.Context, namespace, layer, name string) error
	SetDraftDirty(ctx context.Context, namespace, layer, name string, dirty bool) error
	PublishPipeline(ctx context.Context, namespace, layer, name string, versions map[string]string) error
	UpdatePipelineRetention(ctx context.Context, pipelineID uuid.UUID, config json.RawMessage) error
	ListSoftDeletedPipelines(ctx context.Context, olderThan time.Time) ([]domain.Pipeline, error)
	HardDeletePipeline(ctx context.Context, pipelineID uuid.UUID) error
}

// VersionStore defines the persistence interface for pipeline version history.
type VersionStore interface {
	ListVersions(ctx context.Context, pipelineID uuid.UUID) ([]domain.PipelineVersion, error)
	GetVersion(ctx context.Context, pipelineID uuid.UUID, versionNumber int) (*domain.PipelineVersion, error)
	CreateVersion(ctx context.Context, v *domain.PipelineVersion) error
	PruneVersions(ctx context.Context, pipelineID uuid.UUID, keepCount int) error
	LatestVersionNumber(ctx context.Context, pipelineID uuid.UUID) (int, error)
}

// PipelinePublisher wraps multi-step publish and rollback operations in a
// database transaction so all steps either succeed together or roll back
// atomically. Prevents inconsistent state when a step fails partway through.
type PipelinePublisher interface {
	// PublishPipelineTx atomically: updates published_versions on the pipeline,
	// creates a version history record, and prunes old versions beyond keepCount.
	PublishPipelineTx(ctx context.Context, ns, layer, name string, versions map[string]string, pv *domain.PipelineVersion, keepCount int) error

	// RollbackPipelineTx atomically: creates a new version record with the old
	// snapshot, applies that snapshot as the pipeline's published_versions,
	// and prunes old versions beyond keepCount.
	RollbackPipelineTx(ctx context.Context, ns, layer, name string, versions map[string]string, pv *domain.PipelineVersion, keepCount int) error
}

// Allowed sort fields for pipeline list endpoints.
var pipelineSortFields = map[string]bool{
	"name":       true,
	"namespace":  true,
	"layer":      true,
	"created_at": true,
	"updated_at": true,
	"type":       true,
}

// PipelineFilter holds optional filters for listing pipelines.
// Limit and Offset enable SQL-level pagination. Zero Limit means no limit (return all).
type PipelineFilter struct {
	Namespace string
	Layer     string
	Search    string // substring match on pipeline name (P10-102)
	Limit     int
	Offset    int
	Sort      *SortOrder // optional sort directive
}

// CreatePipelineRequest is the JSON body for POST /api/v1/pipelines.
type CreatePipelineRequest struct {
	Namespace   string `json:"namespace"`
	Layer       string `json:"layer"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Source      string `json:"source"`
	UniqueKey   string `json:"unique_key"`
	Description string `json:"description"`
}

// UpdatePipelineRequest is the JSON body for PUT /api/v1/pipelines/:ns/:layer/:name.
type UpdatePipelineRequest struct {
	Description *string `json:"description"`
	Type        *string `json:"type"`
	Owner       *string `json:"owner"`
}

// MountPipelineRoutes registers pipeline CRUD endpoints on the router.
func MountPipelineRoutes(r chi.Router, srv *Server) {
	r.Get("/pipelines", srv.HandleListPipelines)
	r.Post("/pipelines", srv.HandleCreatePipeline)
	r.Get("/pipelines/{namespace}/{layer}/{name}", srv.HandleGetPipeline)
	r.Put("/pipelines/{namespace}/{layer}/{name}", srv.HandleUpdatePipeline)
	r.Delete("/pipelines/{namespace}/{layer}/{name}", srv.HandleDeletePipeline)
}

// HandleListPipelines returns pipelines, optionally filtered by namespace, layer, and search term.
// Pagination is pushed to SQL via LIMIT/OFFSET for efficiency.
// Supports sorting via ?sort=field or ?sort=-field (descending).
// Supports search via ?search=term (substring match on pipeline name).
func (s *Server) HandleListPipelines(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	filter := PipelineFilter{
		Namespace: r.URL.Query().Get("namespace"),
		Layer:     r.URL.Query().Get("layer"),
		Search:    r.URL.Query().Get("search"),
		Limit:     limit,
		Offset:    offset,
		Sort:      parseSorting(r, pipelineSortFields),
	}

	pipelines, err := s.Pipelines.ListPipelines(r.Context(), filter)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	total, err := s.Pipelines.CountPipelines(r.Context(), filter)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pipelines": pipelines,
		"total":     total,
	})
}

// pipelineCacheKey builds the cache key for a pipeline: "ns/layer/name".
func pipelineCacheKey(namespace, layer, name string) string {
	return namespace + "/" + layer + "/" + name
}

// HandleGetPipeline returns a single pipeline by namespace/layer/name.
// Results are cached because pipeline metadata rarely changes between edits.
func (s *Server) HandleGetPipeline(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	cacheKey := pipelineCacheKey(namespace, layer, name)

	// Try cache first.
	if s.PipelineCache != nil {
		if cached, ok := s.PipelineCache.Get(cacheKey); ok {
			if cached == nil {
				errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, cached)
			return
		}
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

	// Populate cache.
	if s.PipelineCache != nil {
		s.PipelineCache.Set(cacheKey, pipeline)
	}

	writeJSON(w, http.StatusOK, pipeline)
}

// HandleCreatePipeline creates a new pipeline and scaffolds S3 files.
func (s *Server) HandleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var req CreatePipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Namespace == "" || req.Layer == "" || req.Name == "" {
		errorJSON(w, "namespace, layer, and name are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(req.Namespace) || !validName(req.Name) {
		errorJSON(w, "namespace and name must be a lowercase slug (a-z, 0-9, hyphens, underscores; must start with a letter)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !domain.ValidLayer(req.Layer) {
		errorJSON(w, "layer must be bronze, silver, or gold", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		req.Type = "sql"
	}
	if len(req.Description) > maxDescriptionLength {
		errorJSON(w, fmt.Sprintf("description too long (%d chars, max %d)", len(req.Description), maxDescriptionLength), "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	s3Path := req.Namespace + "/pipelines/" + req.Layer + "/" + req.Name + "/"

	pipeline := &domain.Pipeline{
		Namespace:   req.Namespace,
		Layer:       domain.Layer(req.Layer),
		Name:        req.Name,
		Type:        req.Type,
		S3Path:      s3Path,
		Description: req.Description,
	}

	// Set owner from auth context (nil in community mode).
	if user := plugins.UserFromContext(r.Context()); user != nil {
		pipeline.Owner = &user.UserId
	}

	if err := s.Pipelines.CreatePipeline(r.Context(), pipeline); err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			// Return a generic conflict message instead of the raw error which
			// may contain internal details (e.g., SQL constraint names).
			errorJSON(w, "a pipeline with this namespace, layer, and name already exists", "ALREADY_EXISTS", http.StatusConflict)
		} else {
			internalError(w, "internal error", err)
		}
		return
	}

	// Invalidate pipeline cache after creation.
	if s.PipelineCache != nil {
		s.PipelineCache.Delete(pipelineCacheKey(req.Namespace, req.Layer, req.Name))
	}

	// Auto-publish: snapshot initial file versions so first run has something to use.
	// Errors are logged but do not fail the pipeline creation (best-effort).
	if s.Storage != nil {
		if files, err := s.Storage.ListFiles(r.Context(), s3Path); err != nil {
			slog.Warn("auto-publish: failed to list files for initial snapshot",
				"pipeline", pipeline.Namespace+"/"+string(pipeline.Layer)+"/"+pipeline.Name,
				"error", err)
		} else {
			versions := make(map[string]string, len(files))
			for _, f := range files {
				if info, err := s.Storage.StatFile(r.Context(), f.Path); err != nil {
					slog.Warn("auto-publish: failed to stat file", "path", f.Path, "error", err)
				} else if info != nil && info.VersionID != "" {
					versions[f.Path] = info.VersionID
				}
			}
			if len(versions) > 0 {
				if err := s.Pipelines.PublishPipeline(r.Context(), pipeline.Namespace, string(pipeline.Layer), pipeline.Name, versions); err != nil {
					slog.Error("auto-publish: failed to publish initial versions",
						"pipeline", pipeline.Namespace+"/"+string(pipeline.Layer)+"/"+pipeline.Name,
						"error", err)
				}
			}
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"namespace":     pipeline.Namespace,
		"layer":         pipeline.Layer,
		"name":          pipeline.Name,
		"s3_path":       pipeline.S3Path,
		"files_created": []string{"pipeline.sql", "config.yaml"},
	})
}

// HandleUpdatePipeline updates a pipeline's mutable fields (description, type).
func (s *Server) HandleUpdatePipeline(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	// Look up the pipeline to get its ID for authorization.
	existing, err := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if existing == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	if !s.requireAccess(w, r, "pipeline", existing.ID.String(), "write") {
		return
	}

	var req UpdatePipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	pipeline, err := s.Pipelines.UpdatePipeline(r.Context(), namespace, layer, name, req)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Invalidate pipeline cache after update.
	if s.PipelineCache != nil {
		s.PipelineCache.Delete(pipelineCacheKey(namespace, layer, name))
	}

	writeJSON(w, http.StatusOK, pipeline)
}

// HandleDeletePipeline deletes a pipeline by namespace/layer/name.
func (s *Server) HandleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	// Look up the pipeline to get its ID for authorization.
	existing, err := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if existing == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	if !s.requireAccess(w, r, "pipeline", existing.ID.String(), "delete") {
		return
	}

	if err := s.Pipelines.DeletePipeline(r.Context(), namespace, layer, name); err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Invalidate pipeline cache after deletion.
	if s.PipelineCache != nil {
		s.PipelineCache.Delete(pipelineCacheKey(namespace, layer, name))
	}

	// Best-effort S3 cleanup — deletion already succeeded in Postgres, so
	// S3 errors are logged but do not fail the request. Orphaned files can
	// be cleaned up by the reaper.
	if s.Storage != nil {
		s3Prefix := namespace + "/pipelines/" + layer + "/" + name + "/"
		files, err := s.Storage.ListFiles(r.Context(), s3Prefix)
		if err != nil {
			slog.Warn("delete pipeline: failed to list S3 files for cleanup",
				"pipeline", namespace+"/"+layer+"/"+name, "error", err)
		} else {
			for _, f := range files {
				if err := s.Storage.DeleteFile(r.Context(), f.Path); err != nil {
					slog.Warn("delete pipeline: failed to delete S3 file",
						"path", f.Path, "error", err)
				}
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
