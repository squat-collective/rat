package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	sharingv1 "github.com/rat-data/rat/platform/gen/sharing/v1"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// ShareRequest is the JSON body for POST /api/v1/sharing.
type ShareRequest struct {
	GranteeID    string `json:"grantee_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Permission   string `json:"permission"` // "read", "write", "admin"
}

// TransferRequest is the JSON body for POST /api/v1/sharing/transfer.
type TransferRequest struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	ToUserID     string `json:"to_user_id"`
}

// MountSharingRoutes registers sharing endpoints on the router.
func MountSharingRoutes(r chi.Router, srv *Server) {
	r.Post("/sharing", srv.HandleShareResource)
	r.Get("/sharing", srv.HandleListAccess)
	r.Delete("/sharing/{grantID}", srv.HandleRevokeAccess)
	r.Post("/sharing/transfer", srv.HandleTransferOwnership)
}

// HandleShareResource grants access to a resource.
func (s *Server) HandleShareResource(w http.ResponseWriter, r *http.Request) {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		errorJSON(w, "authentication required", "UNAUTHENTICATED", http.StatusUnauthorized)
		return
	}

	if s.Plugins == nil || !s.sharingEnabled() {
		errorJSON(w, "sharing not available", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return
	}

	var req ShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.GranteeID == "" || req.ResourceType == "" || req.ResourceID == "" || req.Permission == "" {
		errorJSON(w, "grantee_id, resource_type, resource_id, and permission are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	reg := s.sharingProvider()
	if reg == nil {
		errorJSON(w, "sharing not available", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return
	}

	resp, err := reg.ShareResource(r.Context(), user.UserId, req.GranteeID, req.ResourceType, req.ResourceID, req.Permission)
	if err != nil {
		errorJSON(w, "failed to share resource", "INTERNAL", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, resp)
}

// HandleListAccess lists access grants for a resource.
func (s *Server) HandleListAccess(w http.ResponseWriter, r *http.Request) {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		errorJSON(w, "authentication required", "UNAUTHENTICATED", http.StatusUnauthorized)
		return
	}

	if s.Plugins == nil || !s.sharingEnabled() {
		errorJSON(w, "sharing not available", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return
	}

	resourceType := r.URL.Query().Get("resource_type")
	resourceID := r.URL.Query().Get("resource_id")
	if resourceType == "" || resourceID == "" {
		errorJSON(w, "resource_type and resource_id query params are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	reg := s.sharingProvider()
	if reg == nil {
		errorJSON(w, "sharing not available", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return
	}

	resp, err := reg.ListAccess(r.Context(), resourceType, resourceID)
	if err != nil {
		errorJSON(w, "failed to list access", "INTERNAL", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleRevokeAccess revokes an access grant.
func (s *Server) HandleRevokeAccess(w http.ResponseWriter, r *http.Request) {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		errorJSON(w, "authentication required", "UNAUTHENTICATED", http.StatusUnauthorized)
		return
	}

	if s.Plugins == nil || !s.sharingEnabled() {
		errorJSON(w, "sharing not available", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return
	}

	reg := s.sharingProvider()
	if reg == nil {
		errorJSON(w, "sharing not available", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return
	}

	grantID := chi.URLParam(r, "grantID")
	if err := reg.RevokeAccess(r.Context(), grantID, user.UserId); err != nil {
		errorJSON(w, "failed to revoke access", "INTERNAL", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleTransferOwnership transfers resource ownership.
// Ownership lives in ratd's Postgres, but we route through here for the API.
func (s *Server) HandleTransferOwnership(w http.ResponseWriter, r *http.Request) {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		errorJSON(w, "authentication required", "UNAUTHENTICATED", http.StatusUnauthorized)
		return
	}

	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.ResourceType != "pipeline" {
		errorJSON(w, "only pipeline transfer is supported", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Verify the requester is the current owner.
	pipeline, err := s.Pipelines.GetPipelineByID(r.Context(), req.ResourceID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}
	if pipeline.Owner == nil || *pipeline.Owner != user.UserId {
		errorJSON(w, "only the owner can transfer ownership", "FORBIDDEN", http.StatusForbidden)
		return
	}

	// Update ownership in Postgres.
	_, err = s.Pipelines.UpdatePipeline(r.Context(), pipeline.Namespace, string(pipeline.Layer), pipeline.Name, UpdatePipelineRequest{Owner: &req.ToUserID})
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"transferred": true,
		"resource_id": req.ResourceID,
		"new_owner":   req.ToUserID,
	})
}

// SharingProvider defines the interface for sharing operations.
// Uses interface assertion instead of concrete type to enable testing
// and decouple from the plugins package.
type SharingProvider interface {
	SharingEnabled() bool
	ShareResource(ctx context.Context, grantorID, granteeID, resourceType, resourceID, permission string) (*sharingv1.ShareResourceResponse, error)
	ListAccess(ctx context.Context, resourceType, resourceID string) (*sharingv1.ListAccessResponse, error)
	RevokeAccess(ctx context.Context, grantID, revokedBy string) error
}

// sharingEnabled checks if the sharing plugin is loaded.
func (s *Server) sharingEnabled() bool {
	sp := s.sharingProvider()
	if sp == nil {
		return false
	}
	return sp.SharingEnabled()
}

// sharingProvider returns the SharingProvider if the plugin registry implements it.
func (s *Server) sharingProvider() SharingProvider {
	if sp, ok := s.Plugins.(SharingProvider); ok {
		return sp
	}
	return nil
}
