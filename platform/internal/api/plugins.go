package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// PluginManager defines the interface the API layer uses for plugin lifecycle operations.
// Implemented by plugins.Manager.
type PluginManager interface {
	Register(ctx context.Context, name, addr string) error
	Enable(ctx context.Context, name string) error
	Disable(ctx context.Context, name string) error
	Remove(ctx context.Context, name string) error
	UpdateConfig(ctx context.Context, name string, config json.RawMessage) (*domain.PluginEntry, error)
}

// PluginLister lists plugins from the catalog (read-only queries).
type PluginLister interface {
	ListPlugins(ctx context.Context, filter domain.PluginFilter) ([]domain.PluginEntry, error)
	GetPlugin(ctx context.Context, name string) (*domain.PluginEntry, error)
}

// MountPluginInternalRoutes mounts the phone-home endpoint outside the auth middleware.
// Called by internal services and plugins to self-register.
func MountPluginInternalRoutes(r chi.Router, srv *Server) {
	r.Post("/internal/plugins/register", srv.HandlePluginRegister)
}

// MountPluginRoutes mounts the authenticated plugin management endpoints.
func MountPluginRoutes(r chi.Router, srv *Server) {
	r.Get("/plugins", srv.HandleListPlugins)
	r.Get("/plugins/{name}", srv.HandleGetPlugin)
	r.Put("/plugins/{name}/enable", srv.HandleEnablePlugin)
	r.Put("/plugins/{name}/disable", srv.HandleDisablePlugin)
	r.Put("/plugins/{name}/config", srv.HandleUpdatePluginConfig)
	r.Delete("/plugins/{name}", srv.HandleDeletePlugin)
}

// HandlePluginRegister handles POST /internal/plugins/register (phone-home).
// Body: {"name": "...", "addr": "..."}
func (srv *Server) HandlePluginRegister(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Name string `json:"name"`
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.Addr == "" {
		errorJSON(w, "name and addr are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(body.Name) {
		errorJSON(w, "name must be a lowercase slug", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if err := srv.PluginManager.Register(r.Context(), body.Name, body.Addr); err != nil {
		internalError(w, "plugin registration failed", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "registered",
		"name":   body.Name,
	})
}

// HandleListPlugins handles GET /api/v1/plugins.
func (srv *Server) HandleListPlugins(w http.ResponseWriter, r *http.Request) {
	if srv.PluginCatalog == nil {
		writeJSON(w, http.StatusOK, []domain.PluginEntry{})
		return
	}

	filter := domain.PluginFilter{
		Status: r.URL.Query().Get("status"),
		Kind:   r.URL.Query().Get("kind"),
	}

	plugins, err := srv.PluginCatalog.ListPlugins(r.Context(), filter)
	if err != nil {
		internalError(w, "failed to list plugins", err)
		return
	}

	writeJSON(w, http.StatusOK, plugins)
}

// HandleGetPlugin handles GET /api/v1/plugins/{name}.
func (srv *Server) HandleGetPlugin(w http.ResponseWriter, r *http.Request) {
	if srv.PluginCatalog == nil {
		errorJSON(w, "plugin catalog not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")
	plugin, err := srv.PluginCatalog.GetPlugin(r.Context(), name)
	if err != nil {
		internalError(w, "failed to get plugin", err)
		return
	}
	if plugin == nil {
		errorJSON(w, "plugin not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, plugin)
}

// HandleEnablePlugin handles PUT /api/v1/plugins/{name}/enable.
func (srv *Server) HandleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")
	if err := srv.PluginManager.Enable(r.Context(), name); err != nil {
		internalError(w, "failed to enable plugin", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "name": name})
}

// HandleDisablePlugin handles PUT /api/v1/plugins/{name}/disable.
func (srv *Server) HandleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")
	if err := srv.PluginManager.Disable(r.Context(), name); err != nil {
		internalError(w, "failed to disable plugin", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled", "name": name})
}

// HandleUpdatePluginConfig handles PUT /api/v1/plugins/{name}/config.
func (srv *Server) HandleUpdatePluginConfig(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")

	var config json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	entry, err := srv.PluginManager.UpdateConfig(r.Context(), name, config)
	if err != nil {
		internalError(w, "failed to update plugin config", err)
		return
	}
	if entry == nil {
		errorJSON(w, "plugin not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, entry)
}

// HandleDeletePlugin handles DELETE /api/v1/plugins/{name}.
func (srv *Server) HandleDeletePlugin(w http.ResponseWriter, r *http.Request) {
	if srv.PluginManager == nil {
		errorJSON(w, "plugin manager not available", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	name := chi.URLParam(r, "name")
	if err := srv.PluginManager.Remove(r.Context(), name); err != nil {
		internalError(w, "failed to delete plugin", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
