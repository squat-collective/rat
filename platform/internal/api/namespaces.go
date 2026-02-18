package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// NamespaceStore defines the persistence interface for namespaces.
type NamespaceStore interface {
	ListNamespaces(ctx context.Context) ([]domain.Namespace, error)
	CreateNamespace(ctx context.Context, name string, createdBy *string) error
	DeleteNamespace(ctx context.Context, name string) error
	UpdateNamespace(ctx context.Context, name, description string) error
}

// CreateNamespaceRequest is the JSON body for POST /api/v1/namespaces.
type CreateNamespaceRequest struct {
	Name string `json:"name"`
}

// UpdateNamespaceRequest is the JSON body for PUT /api/v1/namespaces/{name}.
type UpdateNamespaceRequest struct {
	Description *string `json:"description"`
}

// MountNamespaceRoutes registers namespace endpoints on the router.
func MountNamespaceRoutes(r chi.Router, srv *Server) {
	r.Get("/namespaces", srv.HandleListNamespaces)
	r.Post("/namespaces", srv.HandleCreateNamespace)
	r.Put("/namespaces/{name}", srv.HandleUpdateNamespace)
	r.Delete("/namespaces/{name}", srv.HandleDeleteNamespace)
}

// HandleListNamespaces returns all namespaces with pagination support.
// Results are cached because namespace lists rarely change and are fetched on every portal page load.
// Pagination is applied in-memory since namespace counts are typically small (< 100).
func (s *Server) HandleListNamespaces(w http.ResponseWriter, r *http.Request) {
	const cacheKey = "all"

	var namespaces []domain.Namespace

	// Try cache first.
	if s.NamespaceCache != nil {
		if cached, ok := s.NamespaceCache.Get(cacheKey); ok {
			namespaces = cached
		}
	}

	if namespaces == nil {
		var err error
		namespaces, err = s.Namespaces.ListNamespaces(r.Context())
		if err != nil {
			internalError(w, "internal error", err)
			return
		}

		// Populate cache.
		if s.NamespaceCache != nil {
			s.NamespaceCache.Set(cacheKey, namespaces)
		}
	}

	total := len(namespaces)
	limit, offset := parsePagination(r)
	namespaces = paginate(namespaces, limit, offset)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"namespaces": namespaces,
		"total":      total,
	})
}

// HandleCreateNamespace creates a new namespace.
func (s *Server) HandleCreateNamespace(w http.ResponseWriter, r *http.Request) {
	var req CreateNamespaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		errorJSON(w, "name is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(req.Name) {
		errorJSON(w, "name must be a lowercase slug (a-z, 0-9, hyphens, underscores; must start with a letter)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Set created_by from auth context (nil in community mode).
	var createdBy *string
	if user := plugins.UserFromContext(r.Context()); user != nil {
		createdBy = &user.UserId
	}

	if err := s.Namespaces.CreateNamespace(r.Context(), req.Name, createdBy); err != nil {
		errorJSON(w, err.Error(), "ALREADY_EXISTS", http.StatusConflict)
		return
	}

	// Invalidate namespace cache after mutation.
	if s.NamespaceCache != nil {
		s.NamespaceCache.Clear()
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"name": req.Name,
	})
}

// HandleUpdateNamespace updates a namespace's description.
func (s *Server) HandleUpdateNamespace(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var req UpdateNamespaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.Description == nil {
		errorJSON(w, "description is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if err := s.Namespaces.UpdateNamespace(r.Context(), name, *req.Description); err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Invalidate namespace cache after mutation.
	if s.NamespaceCache != nil {
		s.NamespaceCache.Clear()
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleDeleteNamespace deletes a namespace.
// The "default" namespace cannot be deleted.
func (s *Server) HandleDeleteNamespace(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if name == "default" {
		errorJSON(w, "cannot delete the default namespace", "FORBIDDEN", http.StatusForbidden)
		return
	}

	if !s.requireAccess(w, r, "namespace", name, "delete") {
		return
	}

	if err := s.Namespaces.DeleteNamespace(r.Context(), name); err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Invalidate namespace cache after mutation.
	if s.NamespaceCache != nil {
		s.NamespaceCache.Clear()
	}

	w.WriteHeader(http.StatusNoContent)
}
