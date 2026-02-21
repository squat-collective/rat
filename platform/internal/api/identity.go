package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	identityv1 "github.com/rat-data/rat/platform/gen/identity/v1"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// IdentityProvider defines the interface for identity operations.
// Implemented by the plugins.Registry when the identity plugin is loaded.
type IdentityProvider interface {
	IdentityEnabled() bool
	GetIdentityCapabilities(ctx context.Context) (*identityv1.GetCapabilitiesResponse, error)
	ListIdentityUsers(ctx context.Context, search string, limit, offset int32) (*identityv1.ListUsersResponse, error)
	GetIdentityUser(ctx context.Context, userID string) (*identityv1.GetUserResponse, error)
	SearchIdentityUsers(ctx context.Context, query string, limit int32) (*identityv1.SearchUsersResponse, error)
	ListIdentityGroups(ctx context.Context) (*identityv1.ListIdentityGroupsResponse, error)
}

// MountIdentityRoutes registers identity management endpoints on the router.
func MountIdentityRoutes(r chi.Router, srv *Server) {
	r.Get("/identity/capabilities", srv.HandleIdentityCapabilities)
	// search BEFORE {userID} so chi doesn't treat "search" as a user ID
	r.Get("/identity/users/search", srv.HandleSearchIdentityUsers)
	r.Get("/identity/users/{userID}", srv.HandleGetIdentityUser)
	r.Get("/identity/users", srv.HandleListIdentityUsers)
	r.Get("/identity/groups", srv.HandleListIdentityGroups)
}

// identityProvider returns the IdentityProvider if the plugin registry implements it.
func (s *Server) identityProvider() IdentityProvider {
	if ip, ok := s.Plugins.(IdentityProvider); ok {
		return ip
	}
	return nil
}

// requireIdentity is a helper that checks auth + identity plugin availability.
// Returns the identity provider and user, or writes an error response and returns nil.
func (s *Server) requireIdentity(w http.ResponseWriter, r *http.Request) (IdentityProvider, *domain.UserIdentity) {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		errorJSON(w, "authentication required", "UNAUTHENTICATED", http.StatusUnauthorized)
		return nil, nil
	}

	ip := s.identityProvider()
	if ip == nil || !ip.IdentityEnabled() {
		errorJSON(w, "identity management not available", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return nil, nil
	}

	return ip, user
}

// ── Capabilities ───────────────────────────────────────────────────────────

func (s *Server) HandleIdentityCapabilities(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.requireIdentity(w, r)
	if ip == nil {
		return
	}

	resp, err := ip.GetIdentityCapabilities(r.Context())
	if err != nil {
		internalError(w, "failed to get identity capabilities", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ── Users ──────────────────────────────────────────────────────────────────

func (s *Server) HandleListIdentityUsers(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.requireIdentity(w, r)
	if ip == nil {
		return
	}

	search := r.URL.Query().Get("search")
	limit := int32(50)
	offset := int32(0)

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = int32(n)
		}
	}
	if limit > 200 {
		limit = 200
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}

	resp, err := ip.ListIdentityUsers(r.Context(), search, limit, offset)
	if err != nil {
		internalError(w, "failed to list identity users", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) HandleGetIdentityUser(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.requireIdentity(w, r)
	if ip == nil {
		return
	}

	userID := chi.URLParam(r, "userID")
	if userID == "" {
		errorJSON(w, "user ID is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	resp, err := ip.GetIdentityUser(r.Context(), userID)
	if err != nil {
		internalError(w, "failed to get identity user", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) HandleSearchIdentityUsers(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.requireIdentity(w, r)
	if ip == nil {
		return
	}

	query := r.URL.Query().Get("q")
	limit := int32(10)

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = int32(n)
		}
	}
	if limit > 50 {
		limit = 50
	}

	resp, err := ip.SearchIdentityUsers(r.Context(), query, limit)
	if err != nil {
		internalError(w, "failed to search identity users", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ── Groups ─────────────────────────────────────────────────────────────────

func (s *Server) HandleListIdentityGroups(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.requireIdentity(w, r)
	if ip == nil {
		return
	}

	resp, err := ip.ListIdentityGroups(r.Context())
	if err != nil {
		internalError(w, "failed to list identity groups", err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
