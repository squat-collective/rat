package api

import (
	"context"
	"net/http"

	"github.com/rat-data/rat/platform/internal/plugins"
)

// Authorizer checks whether a user can perform an action on a resource.
// NoopAuthorizer (community) allows everything. PluginAuthorizer (Pro)
// checks ownership first (local), then delegates to EnforcementService.
type Authorizer interface {
	CanAccess(ctx context.Context, userID, resourceType, resourceID, action string) (bool, error)
}

// NoopAuthorizer allows all access (community single-user mode).
type NoopAuthorizer struct{}

func (NoopAuthorizer) CanAccess(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

// requireAccess checks authorization and writes 403 if denied.
// Returns true if access is allowed, false if denied (response already written).
func (s *Server) requireAccess(w http.ResponseWriter, r *http.Request, resourceType, resourceID, action string) bool {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		return true // community mode, no auth = allow all
	}

	authorizer := s.Authorizer
	if authorizer == nil {
		return true // no authorizer configured = allow all
	}

	allowed, err := authorizer.CanAccess(r.Context(), user.UserId, resourceType, resourceID, action)
	if err != nil {
		errorJSON(w, "authorization check failed", "INTERNAL", http.StatusInternalServerError)
		return false
	}
	if !allowed {
		errorJSON(w, "forbidden", "FORBIDDEN", http.StatusForbidden)
		return false
	}
	return true
}
