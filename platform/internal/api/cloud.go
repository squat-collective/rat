package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/plugins"
)

// MountCloudRoutes registers cloud credential vending endpoints on the router.
//
// Routes:
//   - GET /api/v1/cloud/credentials?namespace=X
//
// The route is always mounted. The handler responds 501 NOT_IMPLEMENTED when
// no plugin with capability "cloud" is registered, so the SDK can distinguish
// "endpoint unknown" (404) from "no provider plugin" (501).
func MountCloudRoutes(r chi.Router, srv *Server) {
	r.Get("/cloud/credentials", srv.HandleGetCloudCredentials)
}

// HandleGetCloudCredentials returns scoped, short-lived cloud credentials for
// the authenticated user, scoped to a namespace's storage prefix.
//
// Semantics:
//   - 401 if the request has no authenticated user.
//   - 400 if the required "namespace" query parameter is missing or invalid.
//   - 501 if no cloud provider plugin is registered/enabled.
//   - 502 if the upstream plugin call fails (the plugin is loaded but
//     unhealthy or returned an error — runs depending on cloud creds are
//     unrunnable, but the rest of the platform keeps working).
//   - 200 with a JSON CloudCredentials body on success.
//
// The handler is read-only and idempotent. Callers are expected to refetch
// when credentials approach Expiry — ratd does not cache.
func (s *Server) HandleGetCloudCredentials(w http.ResponseWriter, r *http.Request) {
	user := plugins.UserFromContext(r.Context())
	if user == nil {
		errorJSON(w, "authentication required", "UNAUTHENTICATED", http.StatusUnauthorized)
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		errorJSON(w, "namespace query parameter is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(namespace) {
		errorJSON(w, "namespace must be a lowercase slug (a-z, 0-9, hyphens, underscores; must start with a letter)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if s.Cloud == nil || !s.Cloud.CloudEnabled() {
		errorJSON(w, "no cloud provider plugin registered", "NOT_IMPLEMENTED", http.StatusNotImplemented)
		return
	}

	creds, err := s.Cloud.GetCredentials(r.Context(), user.UserID, namespace)
	if err != nil {
		// The plugin is loaded but the upstream call failed — surface as a
		// gateway error so the SDK can retry / surface a meaningful message.
		errorJSON(w, "cloud provider plugin returned an error", "UPSTREAM_ERROR", http.StatusBadGateway)
		return
	}
	if creds == nil {
		errorJSON(w, "cloud provider returned no credentials", "UPSTREAM_ERROR", http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, creds)
}
