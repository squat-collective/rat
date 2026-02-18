package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// LandingZoneStore defines the persistence interface for landing zones.
type LandingZoneStore interface {
	ListZones(ctx context.Context, filter LandingZoneFilter) ([]LandingZoneListItem, error)
	GetZone(ctx context.Context, namespace, name string) (*LandingZoneDetail, error)
	CreateZone(ctx context.Context, z *domain.LandingZone) error
	DeleteZone(ctx context.Context, namespace, name string) error
	UpdateZone(ctx context.Context, namespace, name string, description, owner, expectedSchema *string) (*domain.LandingZone, error)
	ListFiles(ctx context.Context, zoneID uuid.UUID) ([]domain.LandingFile, error)
	CreateFile(ctx context.Context, f *domain.LandingFile) error
	GetFile(ctx context.Context, fileID uuid.UUID) (*domain.LandingFile, error)
	DeleteFile(ctx context.Context, fileID uuid.UUID) error
	GetZoneByID(ctx context.Context, zoneID uuid.UUID) (*domain.LandingZone, error)
	UpdateZoneLifecycle(ctx context.Context, zoneID uuid.UUID, processedMaxAgeDays *int, autoPurge *bool) error
	ListZonesWithAutoPurge(ctx context.Context) ([]domain.LandingZone, error)
}

// LandingZoneFilter holds optional filters for listing landing zones.
type LandingZoneFilter struct {
	Namespace string
}

// LandingZoneListItem is a zone with aggregated file stats.
type LandingZoneListItem struct {
	domain.LandingZone
	FileCount  int   `json:"file_count"`
	TotalBytes int64 `json:"total_bytes"`
}

// LandingZoneDetail is a zone with aggregated file stats (same shape as list item).
type LandingZoneDetail struct {
	domain.LandingZone
	FileCount  int   `json:"file_count"`
	TotalBytes int64 `json:"total_bytes"`
}

// CreateLandingZoneRequest is the JSON body for POST /api/v1/landing-zones.
type CreateLandingZoneRequest struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateLandingZoneRequest is the JSON body for PUT /api/v1/landing-zones/{namespace}/{name}.
type UpdateLandingZoneRequest struct {
	Description    *string `json:"description,omitempty"`
	Owner          *string `json:"owner,omitempty"`
	ExpectedSchema *string `json:"expected_schema,omitempty"`
}

// MountLandingZoneRoutes registers landing zone endpoints on the router.
func MountLandingZoneRoutes(r chi.Router, srv *Server) {
	r.Get("/landing-zones", srv.HandleListLandingZones)
	r.Post("/landing-zones", srv.HandleCreateLandingZone)
	r.Get("/landing-zones/{namespace}/{name}", srv.HandleGetLandingZone)
	r.Put("/landing-zones/{namespace}/{name}", srv.HandleUpdateLandingZone)
	r.Delete("/landing-zones/{namespace}/{name}", srv.HandleDeleteLandingZone)
	r.Get("/landing-zones/{namespace}/{name}/files", srv.HandleListLandingFiles)
	r.Post("/landing-zones/{namespace}/{name}/files", srv.HandleUploadLandingFile)
	r.Get("/landing-zones/{namespace}/{name}/files/{fileID}", srv.HandleGetLandingFile)
	r.Delete("/landing-zones/{namespace}/{name}/files/{fileID}", srv.HandleDeleteLandingFile)
	r.Get("/landing-zones/{namespace}/{name}/samples", srv.HandleListLandingSamples)
	r.Post("/landing-zones/{namespace}/{name}/samples", srv.HandleUploadLandingSample)
	r.Delete("/landing-zones/{namespace}/{name}/samples/{filename}", srv.HandleDeleteLandingSample)
}

// HandleListLandingZones returns all landing zones with file stats.
func (s *Server) HandleListLandingZones(w http.ResponseWriter, r *http.Request) {
	filter := LandingZoneFilter{
		Namespace: r.URL.Query().Get("namespace"),
	}

	zones, err := s.LandingZones.ListZones(r.Context(), filter)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	total := len(zones)
	limit, offset := parsePagination(r)
	zones = paginate(zones, limit, offset)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"zones": zones,
		"total": total,
	})
}

// HandleCreateLandingZone creates a new landing zone.
func (s *Server) HandleCreateLandingZone(w http.ResponseWriter, r *http.Request) {
	var req CreateLandingZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.Namespace == "" || req.Name == "" {
		errorJSON(w, "namespace and name are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(req.Namespace) || !validName(req.Name) {
		errorJSON(w, "namespace and name must be a lowercase slug (a-z, 0-9, hyphens, underscores; must start with a letter)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	zone := &domain.LandingZone{
		Namespace:   req.Namespace,
		Name:        req.Name,
		Description: req.Description,
	}

	if user := plugins.UserFromContext(r.Context()); user != nil {
		zone.Owner = &user.UserId
	}

	if err := s.LandingZones.CreateZone(r.Context(), zone); err != nil {
		errorJSON(w, err.Error(), "ALREADY_EXISTS", http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusCreated, zone)
}

// HandleGetLandingZone returns a single landing zone with file stats.
func (s *Server) HandleGetLandingZone(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	zone, err := s.LandingZones.GetZone(r.Context(), namespace, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, zone)
}

// HandleUpdateLandingZone updates a landing zone's description, owner, or expected schema.
func (s *Server) HandleUpdateLandingZone(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	var req UpdateLandingZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	zone, err := s.LandingZones.UpdateZone(r.Context(), namespace, name, req.Description, req.Owner, req.ExpectedSchema)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, zone)
}

// HandleDeleteLandingZone deletes a landing zone and its files from S3.
func (s *Server) HandleDeleteLandingZone(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	zone, err := s.LandingZones.GetZone(r.Context(), namespace, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Delete files from S3 if storage is available.
	if s.Storage != nil {
		files, err := s.LandingZones.ListFiles(r.Context(), zone.ID)
		if err == nil {
			for _, f := range files {
				_ = s.Storage.DeleteFile(r.Context(), f.S3Path)
			}
		}
		// Also clean up _samples/ files from S3.
		samplesPrefix := namespace + "/landing/" + name + "/_samples/"
		if sampleFiles, err := s.Storage.ListFiles(r.Context(), samplesPrefix); err == nil {
			for _, sf := range sampleFiles {
				_ = s.Storage.DeleteFile(r.Context(), sf.Path)
			}
		}
	}

	if err := s.LandingZones.DeleteZone(r.Context(), namespace, name); err != nil {
		internalError(w, "internal error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleListLandingFiles returns files in a landing zone with pagination support.
func (s *Server) HandleListLandingFiles(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	zone, err := s.LandingZones.GetZone(r.Context(), namespace, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	files, err := s.LandingZones.ListFiles(r.Context(), zone.ID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	total := len(files)
	limit, offset := parsePagination(r)
	files = paginate(files, limit, offset)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"files": files,
		"total": total,
	})
}

// HandleUploadLandingFile handles multipart file upload to a landing zone.
func (s *Server) HandleUploadLandingFile(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	zone, err := s.LandingZones.GetZone(r.Context(), namespace, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		errorJSON(w, "file too large (max 32MB)", "INVALID_ARGUMENT", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		errorJSON(w, "file form field is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		errorJSON(w, "failed to read uploaded file", "INTERNAL", http.StatusInternalServerError)
		return
	}

	// Sanitize filename to prevent path traversal (e.g., "../../pipelines/victim/pipeline.py")
	safeFilename := filepath.Base(header.Filename)
	if safeFilename == "." || safeFilename == "/" || strings.ContainsAny(safeFilename, "\\/\x00") {
		errorJSON(w, "invalid filename", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Prepend UTC timestamp to avoid filename collisions across uploads
	safeFilename = time.Now().UTC().Format("20060102_150405_") + safeFilename

	s3Path := namespace + "/landing/" + name + "/" + safeFilename

	if s.Storage != nil {
		if _, err := s.Storage.WriteFile(r.Context(), s3Path, content); err != nil {
			internalError(w, "internal error", err)
			return
		}
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	lf := &domain.LandingFile{
		ZoneID:      zone.ID,
		Filename:    safeFilename,
		S3Path:      s3Path,
		SizeBytes:   header.Size,
		ContentType: contentType,
	}

	if user := plugins.UserFromContext(r.Context()); user != nil {
		lf.UploadedBy = &user.UserId
	}

	if err := s.LandingZones.CreateFile(r.Context(), lf); err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Evaluate landing zone triggers in the background — never block the upload response.
	// Use a detached context with a timeout rather than context.Background() to bound lifetime.
	if s.Triggers != nil {
		triggerCtx, triggerCancel := context.WithTimeout(context.Background(), 30*time.Second)
		go func() {
			defer triggerCancel()
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("panic in landing zone trigger evaluation", "panic", rec)
				}
			}()
			s.evaluateLandingZoneTriggers(triggerCtx, namespace, name, header.Filename)
		}()
	}

	writeJSON(w, http.StatusCreated, lf)
}

// HandleGetLandingFile returns metadata for a single file.
func (s *Server) HandleGetLandingFile(w http.ResponseWriter, r *http.Request) {
	fileIDStr := chi.URLParam(r, "fileID")
	fileID, err := uuid.Parse(fileIDStr)
	if err != nil {
		errorJSON(w, "invalid file ID", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	file, err := s.LandingZones.GetFile(r.Context(), fileID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if file == nil {
		errorJSON(w, "file not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, file)
}

// HandleDeleteLandingFile deletes a file from S3 and the database.
func (s *Server) HandleDeleteLandingFile(w http.ResponseWriter, r *http.Request) {
	fileIDStr := chi.URLParam(r, "fileID")
	fileID, err := uuid.Parse(fileIDStr)
	if err != nil {
		errorJSON(w, "invalid file ID", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	file, err := s.LandingZones.GetFile(r.Context(), fileID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if file == nil {
		errorJSON(w, "file not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	if s.Storage != nil {
		_ = s.Storage.DeleteFile(r.Context(), file.S3Path)
	}

	if err := s.LandingZones.DeleteFile(r.Context(), fileID); err != nil {
		internalError(w, "internal error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleListLandingSamples returns sample files for a landing zone from S3.
func (s *Server) HandleListLandingSamples(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	zone, err := s.LandingZones.GetZone(r.Context(), namespace, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	var files []FileInfo
	if s.Storage != nil {
		samplesPrefix := namespace + "/landing/" + name + "/_samples/"
		files, err = s.Storage.ListFiles(r.Context(), samplesPrefix)
		if err != nil {
			internalError(w, "internal error", err)
			return
		}
	}
	if files == nil {
		files = []FileInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"files": files,
		"total": len(files),
	})
}

// HandleUploadLandingSample handles multipart file upload to a landing zone's _samples/ folder.
func (s *Server) HandleUploadLandingSample(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	zone, err := s.LandingZones.GetZone(r.Context(), namespace, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		errorJSON(w, "file too large (max 32MB)", "INVALID_ARGUMENT", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		errorJSON(w, "file form field is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		errorJSON(w, "failed to read uploaded file", "INTERNAL", http.StatusInternalServerError)
		return
	}

	// Sanitize filename — no timestamp prefix for samples (curated, not append-only).
	safeFilename := filepath.Base(header.Filename)
	if safeFilename == "." || safeFilename == "/" || strings.ContainsAny(safeFilename, "\\/\x00") {
		errorJSON(w, "invalid filename", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	s3Path := namespace + "/landing/" + name + "/_samples/" + safeFilename

	if s.Storage != nil {
		if _, err := s.Storage.WriteFile(r.Context(), s3Path, content); err != nil {
			internalError(w, "internal error", err)
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"path":     s3Path,
		"filename": safeFilename,
		"size":     len(content),
		"status":   "uploaded",
	})
}

// HandleDeleteLandingSample deletes a sample file from a landing zone's _samples/ folder.
func (s *Server) HandleDeleteLandingSample(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	filename := chi.URLParam(r, "filename")

	// Validate filename to prevent path traversal.
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") {
		errorJSON(w, "invalid filename", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	zone, err := s.LandingZones.GetZone(r.Context(), namespace, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	s3Path := namespace + "/landing/" + name + "/_samples/" + filename

	if s.Storage != nil {
		_ = s.Storage.DeleteFile(r.Context(), s3Path)
	}

	w.WriteHeader(http.StatusNoContent)
}
