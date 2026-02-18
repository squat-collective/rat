package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// maxUploadSize is the maximum allowed upload size (32 MB).
const maxUploadSize = 32 << 20

// FileInfo represents metadata about a file in S3.
type FileInfo struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	Type      string    `json:"type"` // pipeline-sql, config, meta, doc, test, hook
	VersionID string    `json:"version_id,omitempty"`
}

// FileContent represents a file's content and metadata.
type FileContent struct {
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	VersionID string    `json:"version_id,omitempty"`
}

// WriteFileRequest is the JSON body for PUT /api/v1/files/*.
type WriteFileRequest struct {
	Content string `json:"content"`
}

// StorageStore defines the persistence interface for S3 file operations.
type StorageStore interface {
	ListFiles(ctx context.Context, prefix string) ([]FileInfo, error)
	ReadFile(ctx context.Context, path string) (*FileContent, error)
	WriteFile(ctx context.Context, path string, content []byte) (versionID string, err error)
	DeleteFile(ctx context.Context, path string) error
	StatFile(ctx context.Context, path string) (*FileInfo, error)
	ReadFileVersion(ctx context.Context, path, versionID string) (*FileContent, error)
}

// MountStorageRoutes registers file/storage endpoints on the router.
func MountStorageRoutes(r chi.Router, srv *Server) {
	r.Get("/files", srv.HandleListFiles)
	r.Post("/files/upload", srv.HandleUploadFile)
	r.Get("/files/*", srv.HandleReadFile)
	r.Put("/files/*", srv.HandleWriteFile)
	r.Delete("/files/*", srv.HandleDeleteFile)
}

// HandleListFiles lists files in an S3 prefix.
// The prefix must start with a valid namespace (no cross-namespace listing).
// The optional "exclude" query param is a comma-separated list of path segments
// to filter out (e.g. exclude=landing,data removes paths containing /landing/ or /data/).
func (s *Server) HandleListFiles(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	exclude := r.URL.Query().Get("exclude")

	// P1-05: Validate prefix to prevent cross-namespace access.
	if prefix != "" {
		if msg := validateFilePath(prefix); msg != "" {
			errorJSON(w, msg, "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		// Require prefix to start with a namespace segment.
		ns := namespaceFromPath(prefix)
		if ns == "" {
			errorJSON(w, "prefix must start with a namespace", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		if !s.requireAccess(w, r, "namespace", ns, "read") {
			return
		}
	} else {
		errorJSON(w, "prefix query parameter is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	files, err := s.Storage.ListFiles(r.Context(), prefix)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if files == nil {
		files = []FileInfo{}
	}

	if exclude != "" {
		segments := strings.Split(exclude, ",")
		filtered := make([]FileInfo, 0, len(files))
		for _, f := range files {
			skip := false
			for _, seg := range segments {
				if strings.Contains(f.Path, "/"+seg+"/") {
					skip = true
					break
				}
			}
			if !skip {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"files": files,
	})
}

// HandleReadFile reads a single file's content.
func (s *Server) HandleReadFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	if msg := validateFilePath(path); msg != "" {
		errorJSON(w, msg, "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if ns := namespaceFromPath(path); ns != "" {
		if !s.requireAccess(w, r, "namespace", ns, "read") {
			return
		}
	}

	file, err := s.Storage.ReadFile(r.Context(), path)
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

// namespaceFromPath extracts the namespace (first path segment) from an S3 path.
func namespaceFromPath(path string) string {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// validateFilePath rejects paths that could escape their namespace scope.
// Returns an error message if the path is invalid, or "" if it's safe.
func validateFilePath(path string) string {
	if path == "" {
		return "path is required"
	}
	if strings.Contains(path, "..") {
		return "path must not contain '..'"
	}
	if strings.HasPrefix(path, "/") {
		return "path must be relative"
	}
	if strings.ContainsAny(path, "\x00\\") {
		return "path contains invalid characters"
	}
	return ""
}

// HandleWriteFile creates or updates a file.
func (s *Server) HandleWriteFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	if msg := validateFilePath(path); msg != "" {
		errorJSON(w, msg, "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if ns := namespaceFromPath(path); ns != "" {
		if !s.requireAccess(w, r, "namespace", ns, "write") {
			return
		}
	}

	var req WriteFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	versionID, err := s.Storage.WriteFile(r.Context(), path, []byte(req.Content))
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Mark the pipeline as draft-dirty when a pipeline file is written.
	if pipelineRef := parsePipelinePath(path); pipelineRef != nil && s.Pipelines != nil {
		_ = s.Pipelines.SetDraftDirty(r.Context(), pipelineRef.Namespace, pipelineRef.Layer, pipelineRef.Name, true)
		// Invalidate pipeline cache since draft_dirty changed.
		if s.PipelineCache != nil {
			s.PipelineCache.Delete(pipelineCacheKey(pipelineRef.Namespace, pipelineRef.Layer, pipelineRef.Name))
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":       path,
		"status":     "written",
		"version_id": versionID,
	})
}

// pipelineRef holds the namespace/layer/name parsed from a pipeline file path.
type pipelineRef struct {
	Namespace string
	Layer     string
	Name      string
}

// parsePipelinePath extracts namespace, layer, and name from a pipeline file path.
// Expects format: {namespace}/pipelines/{layer}/{name}/...
// Returns nil if the path doesn't match a pipeline file.
func parsePipelinePath(path string) *pipelineRef {
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[1] != "pipelines" {
		return nil
	}
	return &pipelineRef{
		Namespace: parts[0],
		Layer:     parts[2],
		Name:      parts[3],
	}
}

// HandleDeleteFile deletes a file from S3.
func (s *Server) HandleDeleteFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	if msg := validateFilePath(path); msg != "" {
		errorJSON(w, msg, "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if ns := namespaceFromPath(path); ns != "" {
		if !s.requireAccess(w, r, "namespace", ns, "write") {
			return
		}
	}

	if err := s.Storage.DeleteFile(r.Context(), path); err != nil {
		internalError(w, "internal error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleUploadFile handles multipart file uploads to S3.
// The "path" form field sets the destination, "file" is the uploaded content.
func (s *Server) HandleUploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		errorJSON(w, "file too large (max 32MB)", "INVALID_ARGUMENT", http.StatusRequestEntityTooLarge)
		return
	}

	destPath := r.FormValue("path")
	if destPath == "" {
		errorJSON(w, "path form field is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if msg := validateFilePath(destPath); msg != "" {
		errorJSON(w, msg, "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if ns := namespaceFromPath(destPath); ns != "" {
		if !s.requireAccess(w, r, "namespace", ns, "write") {
			return
		}
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

	versionID, err := s.Storage.WriteFile(r.Context(), destPath, content)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Mark the pipeline as draft-dirty when a pipeline file is uploaded.
	if pipelineRef := parsePipelinePath(destPath); pipelineRef != nil && s.Pipelines != nil {
		_ = s.Pipelines.SetDraftDirty(r.Context(), pipelineRef.Namespace, pipelineRef.Layer, pipelineRef.Name, true)
		// Invalidate pipeline cache since draft_dirty changed.
		if s.PipelineCache != nil {
			s.PipelineCache.Delete(pipelineCacheKey(pipelineRef.Namespace, pipelineRef.Layer, pipelineRef.Name))
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"path":       destPath,
		"filename":   header.Filename,
		"size":       header.Size,
		"status":     "uploaded",
		"version_id": versionID,
	})
}
