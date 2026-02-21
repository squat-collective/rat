package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// MountRunnerPluginRoutes registers the runner plugin listing endpoint.
func MountRunnerPluginRoutes(r chi.Router, srv *Server) {
	r.Get("/runner/plugins", srv.HandleRunnerPlugins)
}

// HandleRunnerPlugins returns the list of Python entry points discovered by the runner.
// GET /api/v1/runner/plugins
func (s *Server) HandleRunnerPlugins(w http.ResponseWriter, r *http.Request) {
	if s.RunnerPlugins == nil {
		writeJSON(w, http.StatusOK, []domain.RunnerPlugin{})
		return
	}

	plugins, err := s.RunnerPlugins.ListRunnerPlugins(r.Context())
	if err != nil {
		internalError(w, "failed to list runner plugins", err)
		return
	}

	if plugins == nil {
		plugins = []domain.RunnerPlugin{}
	}

	writeJSON(w, http.StatusOK, plugins)
}
