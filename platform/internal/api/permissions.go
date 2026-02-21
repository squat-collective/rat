package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	permissionv1 "github.com/rat-data/rat/platform/gen/permission/v1"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// PermissionProvider defines the interface for permission operations.
// Implemented by the plugins.Registry when the permission plugin is loaded.
type PermissionProvider interface {
	PermissionEnabled() bool
	ListVerbs(ctx context.Context) (*permissionv1.ListVerbsResponse, error)
	RegisterVerb(ctx context.Context, name string, implies []string, description string) error
	ListGrants(ctx context.Context, resource string, principalType permissionv1.PrincipalType, principalID string) (*permissionv1.ListGrantsResponse, error)
	CreatePermissionGrant(ctx context.Context, req *permissionv1.CreateGrantRequest) (*permissionv1.CreateGrantResponse, error)
	RevokePermissionGrant(ctx context.Context, grantID string) (*permissionv1.RevokeGrantResponse, error)
	ListGroups(ctx context.Context) (*permissionv1.ListGroupsResponse, error)
	CreatePermissionGroup(ctx context.Context, name, description string) (*permissionv1.CreateGroupResponse, error)
	DeletePermissionGroup(ctx context.Context, groupID string) (*permissionv1.DeleteGroupResponse, error)
	ListGroupMembers(ctx context.Context, groupID string) (*permissionv1.ListGroupMembersResponse, error)
	AddGroupMember(ctx context.Context, groupID string, memberType permissionv1.PrincipalType, memberID string) error
	RemoveGroupMember(ctx context.Context, groupID string, memberType permissionv1.PrincipalType, memberID string) (*permissionv1.RemoveGroupMemberResponse, error)
	CheckPermissionAccess(ctx context.Context, userID string, userGroups []string, resource, verb string) (*permissionv1.CheckAccessResponse, error)
	ListResourceAccess(ctx context.Context, resource string) (*permissionv1.ListResourceAccessResponse, error)
	ListPrincipalAccess(ctx context.Context, userID string, userGroups []string, resourcePrefix string) (*permissionv1.ListPrincipalAccessResponse, error)
	RemovePermissionResource(ctx context.Context, resource string, cascade bool) (*permissionv1.RemoveResourceResponse, error)
}

// MountPermissionRoutes registers permission management endpoints on the router.
func MountPermissionRoutes(r chi.Router, srv *Server) {
	// Verbs
	r.Get("/permissions/verbs", srv.HandleListVerbs)
	r.Post("/permissions/verbs", srv.HandleRegisterVerb)

	// Grants
	r.Get("/permissions/grants", srv.HandleListGrants)
	r.Post("/permissions/grants", srv.HandleCreateGrant)
	r.Delete("/permissions/grants/{grantID}", srv.HandleRevokeGrant)

	// Groups
	r.Get("/permissions/groups", srv.HandleListGroups)
	r.Post("/permissions/groups", srv.HandleCreateGroup)
	r.Delete("/permissions/groups/{groupID}", srv.HandleDeleteGroup)
	r.Get("/permissions/groups/{groupID}/members", srv.HandleListGroupMembers)
	r.Post("/permissions/groups/{groupID}/members", srv.HandleAddGroupMember)
	r.Delete("/permissions/groups/{groupID}/members", srv.HandleRemoveGroupMember)

	// Access checks
	r.Post("/permissions/check", srv.HandleCheckAccess)
	r.Get("/permissions/access/resource", srv.HandleListResourceAccess)
	r.Get("/permissions/access/principal", srv.HandleListPrincipalAccess)

	// Resource cleanup
	r.Delete("/permissions/resources", srv.HandleRemoveResource)
}

// permissionProvider returns the PermissionProvider if the plugin registry implements it.
func (s *Server) permissionProvider() PermissionProvider {
	if pp, ok := s.Plugins.(PermissionProvider); ok {
		return pp
	}
	return nil
}

// requirePermission is a helper that checks auth + permission plugin availability.
// Returns the permission provider and user, or writes an error response and returns nil.
func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request) (PermissionProvider, *domain.UserIdentity) {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		errorJSON(w, "authentication required", "UNAUTHENTICATED", http.StatusUnauthorized)
		return nil, nil
	}

	pp := s.permissionProvider()
	if pp == nil || !pp.PermissionEnabled() {
		errorJSON(w, "permission management not available", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return nil, nil
	}

	return pp, user
}

// ── Verbs ──────────────────────────────────────────────────────────────────

func (s *Server) HandleListVerbs(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	resp, err := pp.ListVerbs(r.Context())
	if err != nil {
		internalError(w, "failed to list verbs", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

type registerVerbRequest struct {
	Name        string   `json:"name"`
	Implies     []string `json:"implies"`
	Description string   `json:"description"`
}

func (s *Server) HandleRegisterVerb(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	var req registerVerbRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		errorJSON(w, "name is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if err := pp.RegisterVerb(r.Context(), req.Name, req.Implies, req.Description); err != nil {
		internalError(w, "failed to register verb", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Grants ─────────────────────────────────────────────────────────────────

func (s *Server) HandleListGrants(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	resource := r.URL.Query().Get("resource")
	principalID := r.URL.Query().Get("principal_id")
	principalType := parsePrincipalType(r.URL.Query().Get("principal_type"))

	resp, err := pp.ListGrants(r.Context(), resource, principalType, principalID)
	if err != nil {
		internalError(w, "failed to list grants", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

type createGrantRequest struct {
	PrincipalType string `json:"principal_type"` // "user", "group", "role"
	PrincipalID   string `json:"principal_id"`
	Resource      string `json:"resource"`
	Verb          string `json:"verb"`
}

func (s *Server) HandleCreateGrant(w http.ResponseWriter, r *http.Request) {
	pp, user := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	var req createGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.PrincipalID == "" || req.Resource == "" || req.Verb == "" {
		errorJSON(w, "principal_id, resource, and verb are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	pType := parsePrincipalType(req.PrincipalType)
	if pType == permissionv1.PrincipalType_PRINCIPAL_TYPE_UNSPECIFIED {
		errorJSON(w, "valid principal_type is required (user, group, or role)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := pp.CreatePermissionGrant(r.Context(), &permissionv1.CreateGrantRequest{
		PrincipalType: pType,
		PrincipalId:   req.PrincipalID,
		Resource:      req.Resource,
		Verb:          req.Verb,
		GrantedBy:     user.UserID,
	})
	if err != nil {
		internalError(w, "failed to create grant", err)
		return
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) HandleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	grantID := chi.URLParam(r, "grantID")
	if grantID == "" {
		errorJSON(w, "grant ID is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := pp.RevokePermissionGrant(r.Context(), grantID)
	if err != nil {
		internalError(w, "failed to revoke grant", err)
		return
	}

	if !resp.Revoked {
		errorJSON(w, "grant not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Groups ─────────────────────────────────────────────────────────────────

func (s *Server) HandleListGroups(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	resp, err := pp.ListGroups(r.Context())
	if err != nil {
		internalError(w, "failed to list groups", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

type createGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) HandleCreateGroup(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		errorJSON(w, "name is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := pp.CreatePermissionGroup(r.Context(), req.Name, req.Description)
	if err != nil {
		internalError(w, "failed to create group", err)
		return
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) HandleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	groupID := chi.URLParam(r, "groupID")
	if groupID == "" {
		errorJSON(w, "group ID is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := pp.DeletePermissionGroup(r.Context(), groupID)
	if err != nil {
		internalError(w, "failed to delete group", err)
		return
	}

	if !resp.Deleted {
		errorJSON(w, "group not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) HandleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	groupID := chi.URLParam(r, "groupID")
	resp, err := pp.ListGroupMembers(r.Context(), groupID)
	if err != nil {
		internalError(w, "failed to list group members", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

type addGroupMemberRequest struct {
	MemberType string `json:"member_type"` // "user" or "group"
	MemberID   string `json:"member_id"`
}

func (s *Server) HandleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	groupID := chi.URLParam(r, "groupID")
	var req addGroupMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.MemberID == "" {
		errorJSON(w, "member_id is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	mType := parsePrincipalType(req.MemberType)
	if mType != permissionv1.PrincipalType_PRINCIPAL_TYPE_USER && mType != permissionv1.PrincipalType_PRINCIPAL_TYPE_GROUP {
		errorJSON(w, "member_type must be user or group", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if err := pp.AddGroupMember(r.Context(), groupID, mType, req.MemberID); err != nil {
		internalError(w, "failed to add group member", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type removeGroupMemberRequest struct {
	MemberType string `json:"member_type"` // "user" or "group"
	MemberID   string `json:"member_id"`
}

func (s *Server) HandleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	groupID := chi.URLParam(r, "groupID")
	var req removeGroupMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.MemberID == "" {
		errorJSON(w, "member_id is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	mType := parsePrincipalType(req.MemberType)
	if mType != permissionv1.PrincipalType_PRINCIPAL_TYPE_USER && mType != permissionv1.PrincipalType_PRINCIPAL_TYPE_GROUP {
		errorJSON(w, "member_type must be user or group", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := pp.RemoveGroupMember(r.Context(), groupID, mType, req.MemberID)
	if err != nil {
		internalError(w, "failed to remove group member", err)
		return
	}

	if !resp.Removed {
		errorJSON(w, "member not found in group", "NOT_FOUND", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Access Checks ──────────────────────────────────────────────────────────

type checkAccessRequest struct {
	UserID   string   `json:"user_id"`
	Groups   []string `json:"groups"`
	Resource string   `json:"resource"`
	Verb     string   `json:"verb"`
}

func (s *Server) HandleCheckAccess(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	var req checkAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.UserID == "" || req.Resource == "" || req.Verb == "" {
		errorJSON(w, "user_id, resource, and verb are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := pp.CheckPermissionAccess(r.Context(), req.UserID, req.Groups, req.Resource, req.Verb)
	if err != nil {
		internalError(w, "failed to check access", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) HandleListResourceAccess(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	resource := r.URL.Query().Get("resource")
	if resource == "" {
		errorJSON(w, "resource query param is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := pp.ListResourceAccess(r.Context(), resource)
	if err != nil {
		internalError(w, "failed to list resource access", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) HandleListPrincipalAccess(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		errorJSON(w, "user_id query param is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	resourcePrefix := r.URL.Query().Get("resource_prefix")

	resp, err := pp.ListPrincipalAccess(r.Context(), userID, nil, resourcePrefix)
	if err != nil {
		internalError(w, "failed to list principal access", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ── Resource Cleanup ───────────────────────────────────────────────────────

type removeResourceRequest struct {
	Resource string `json:"resource"`
	Cascade  bool   `json:"cascade"`
}

func (s *Server) HandleRemoveResource(w http.ResponseWriter, r *http.Request) {
	pp, _ := s.requirePermission(w, r)
	if pp == nil {
		return
	}

	var req removeResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.Resource == "" {
		errorJSON(w, "resource is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := pp.RemovePermissionResource(r.Context(), req.Resource, req.Cascade)
	if err != nil {
		internalError(w, "failed to remove resource", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ── Helpers ────────────────────────────────────────────────────────────────

// parsePrincipalType converts a string to a PrincipalType enum value.
func parsePrincipalType(s string) permissionv1.PrincipalType {
	switch s {
	case "user":
		return permissionv1.PrincipalType_PRINCIPAL_TYPE_USER
	case "group":
		return permissionv1.PrincipalType_PRINCIPAL_TYPE_GROUP
	case "role":
		return permissionv1.PrincipalType_PRINCIPAL_TYPE_ROLE
	default:
		return permissionv1.PrincipalType_PRINCIPAL_TYPE_UNSPECIFIED
	}
}
