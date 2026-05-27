package api

import (
	"context"
	"net/http"

	"github.com/rat-data/rat/platform/internal/plugins"
)

// Authorizer checks whether a user can perform an action on a resource.
// NoopAuthorizer (community) allows everything. PluginAuthorizer (Pro)
// checks ownership first (local), then delegates to EnforcementService.
//
// Filter is the batch variant used by list/read paths: returns the subset
// of resourceIDs the user is allowed to access for the given action. The
// returned slice's order is unspecified. Callers should build a set from
// the result and filter their item list in one pass.
type Authorizer interface {
	CanAccess(ctx context.Context, userID, resourceType, resourceID, action string) (bool, error)
	Filter(ctx context.Context, userID, resourceType, action string, resourceIDs []string) ([]string, error)
}

// NoopAuthorizer allows all access (community single-user mode).
type NoopAuthorizer struct{}

func (NoopAuthorizer) CanAccess(_ context.Context, _, _, _, _ string) (bool, error) {
	return true, nil
}

func (NoopAuthorizer) Filter(_ context.Context, _, _, _ string, ids []string) ([]string, error) {
	return ids, nil
}

// filterAccess returns the subset of resourceIDs the current request's user
// can access for the given action. When the request has no user (community
// mode) or no authorizer is configured, the full input slice is returned
// unchanged — same passthrough as requireAccess.
//
// On error, an empty slice is returned (deny-by-default for list paths).
// Callers should treat an error as "no results" rather than 500ing the
// whole list, mirroring the behaviour of an EnforcementService outage.
func (s *Server) filterAccess(ctx context.Context, resourceType, action string, resourceIDs []string) []string {
	if len(resourceIDs) == 0 {
		return resourceIDs
	}
	user := plugins.UserFromContext(ctx)
	if user == nil {
		return resourceIDs
	}
	authorizer := s.Authorizer
	if authorizer == nil {
		return resourceIDs
	}
	allowed, err := authorizer.Filter(ctx, user.UserID, resourceType, action, resourceIDs)
	if err != nil {
		return nil
	}
	return allowed
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

	allowed, err := authorizer.CanAccess(r.Context(), user.UserID, resourceType, resourceID, action)
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
