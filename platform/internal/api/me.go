package api

import (
	"net/http"

	"github.com/rat-data/rat/platform/internal/plugins"
)

// MeResponse is the JSON shape returned by GET /api/v1/me.
type MeResponse struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
}

// HandleMe returns the authenticated user's identity from the request context.
// Returns 401 when no user is present (community edition or missing token).
func (s *Server) HandleMe(w http.ResponseWriter, r *http.Request) {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		errorJSON(w, "authentication required", "UNAUTHENTICATED", http.StatusUnauthorized)
		return
	}

	roles := user.Roles
	if roles == nil {
		roles = []string{}
	}

	writeJSON(w, http.StatusOK, MeResponse{
		UserID:      user.UserID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Roles:       roles,
	})
}
