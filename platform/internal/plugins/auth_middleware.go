package plugins

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rat-data/rat/platform/internal/auth"
)

// AuthMiddleware returns HTTP middleware that delegates authentication to the auth plugin.
// If no auth plugin is loaded, returns auth.Noop() (community edition pass-through).
func (r *Registry) AuthMiddleware() func(http.Handler) http.Handler {
	if r.auth == nil {
		return auth.Noop()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			token := extractBearerToken(req)
			if token == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "missing or invalid Authorization header",
				})
				return
			}

			resp, err := r.Authenticate(req.Context(), token)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "authentication failed",
				})
				return
			}

			if !resp.Authenticated {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": resp.ErrorMessage,
				})
				return
			}

			// Store user identity in context for downstream handlers.
			ctx := ContextWithUser(req.Context(), resp.User)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

// extractBearerToken extracts the token from "Authorization: Bearer <token>".
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Error is intentionally ignored: headers and status code are already
	// written, so there is nothing useful we can do if encoding fails.
	_ = json.NewEncoder(w).Encode(v)
}
