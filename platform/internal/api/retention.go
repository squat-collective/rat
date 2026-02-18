package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// SettingsStore defines the persistence interface for platform settings.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (json.RawMessage, error)
	PutSetting(ctx context.Context, key string, value json.RawMessage) error
	GetReaperStatus(ctx context.Context) (*domain.ReaperStatus, error)
	UpdateReaperStatus(ctx context.Context, status *domain.ReaperStatus) error
}

// ReaperRunner allows the API to trigger a manual reaper run.
type ReaperRunner interface {
	RunNow(ctx context.Context) (*domain.ReaperStatus, error)
}

// RetentionConfigResponse wraps the retention config for API responses.
type RetentionConfigResponse struct {
	Config domain.RetentionConfig `json:"config"`
}

// PipelineRetentionResponse shows system defaults, overrides, and effective config.
type PipelineRetentionResponse struct {
	System    domain.RetentionConfig  `json:"system"`
	Overrides json.RawMessage         `json:"overrides"` // null if no overrides
	Effective domain.RetentionConfig  `json:"effective"`
}

// ZoneLifecycleResponse holds landing zone lifecycle settings.
type ZoneLifecycleResponse struct {
	ProcessedMaxAgeDays *int `json:"processed_max_age_days"`
	AutoPurge           bool `json:"auto_purge"`
}

// ZoneLifecycleRequest is the JSON body for PUT /api/v1/landing-zones/{namespace}/{name}/lifecycle.
type ZoneLifecycleRequest struct {
	ProcessedMaxAgeDays *int  `json:"processed_max_age_days"`
	AutoPurge           *bool `json:"auto_purge"`
}

// MountRetentionRoutes registers retention management endpoints.
func MountRetentionRoutes(r chi.Router, srv *Server) {
	// Admin retention config
	r.Get("/admin/retention/config", srv.HandleGetRetentionConfig)
	r.Put("/admin/retention/config", srv.HandlePutRetentionConfig)
	r.Get("/admin/retention/status", srv.HandleGetReaperStatus)
	r.Post("/admin/retention/run", srv.HandleTriggerReaper)

	// Per-pipeline retention
	r.Get("/pipelines/{namespace}/{layer}/{name}/retention", srv.HandleGetPipelineRetention)
	r.Put("/pipelines/{namespace}/{layer}/{name}/retention", srv.HandlePutPipelineRetention)

	// Landing zone lifecycle
	r.Get("/landing-zones/{namespace}/{name}/lifecycle", srv.HandleGetZoneLifecycle)
	r.Put("/landing-zones/{namespace}/{name}/lifecycle", srv.HandlePutZoneLifecycle)
}

// HandleGetRetentionConfig returns the system retention config.
func (s *Server) HandleGetRetentionConfig(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		errorJSON(w, "settings not configured", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	cfg, err := s.loadRetentionConfig(r.Context())
	if err != nil {
		internalError(w, "failed to load retention config", err)
		return
	}

	writeJSON(w, http.StatusOK, RetentionConfigResponse{Config: cfg})
}

// HandlePutRetentionConfig updates the system retention config.
func (s *Server) HandlePutRetentionConfig(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		errorJSON(w, "settings not configured", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	var cfg domain.RetentionConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Basic validation
	if cfg.RunsMaxPerPipeline < 1 {
		errorJSON(w, "runs_max_per_pipeline must be >= 1", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if cfg.ReaperIntervalMinutes < 1 {
		errorJSON(w, "reaper_interval_minutes must be >= 1", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		internalError(w, "failed to marshal config", err)
		return
	}

	if err := s.Settings.PutSetting(r.Context(), "retention", data); err != nil {
		internalError(w, "failed to save retention config", err)
		return
	}

	writeJSON(w, http.StatusOK, RetentionConfigResponse{Config: cfg})
}

// HandleGetReaperStatus returns the last reaper run stats.
func (s *Server) HandleGetReaperStatus(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		errorJSON(w, "settings not configured", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	status, err := s.Settings.GetReaperStatus(r.Context())
	if err != nil {
		internalError(w, "failed to get reaper status", err)
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// HandleTriggerReaper triggers a manual reaper run.
func (s *Server) HandleTriggerReaper(w http.ResponseWriter, r *http.Request) {
	if s.Reaper == nil {
		errorJSON(w, "reaper not configured", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	status, err := s.Reaper.RunNow(r.Context())
	if err != nil {
		internalError(w, "reaper run failed", err)
		return
	}

	writeJSON(w, http.StatusAccepted, status)
}

// HandleGetPipelineRetention returns the pipeline's retention config (system + overrides + effective).
func (s *Server) HandleGetPipelineRetention(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil || s.Pipelines == nil {
		errorJSON(w, "not configured", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	ns := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	pipeline, err := s.Pipelines.GetPipeline(r.Context(), ns, layer, name)
	if err != nil {
		internalError(w, "failed to get pipeline", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	systemCfg, err := s.loadRetentionConfig(r.Context())
	if err != nil {
		internalError(w, "failed to load retention config", err)
		return
	}

	effective := systemCfg
	if len(pipeline.RetentionConfig) > 0 {
		// Merge pipeline overrides onto system defaults
		if err := json.Unmarshal(pipeline.RetentionConfig, &effective); err != nil {
			slog.Warn("failed to unmarshal pipeline retention overrides, using system defaults",
				"pipeline", ns+"/"+layer+"/"+name, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, PipelineRetentionResponse{
		System:    systemCfg,
		Overrides: pipeline.RetentionConfig,
		Effective: effective,
	})
}

// HandlePutPipelineRetention updates per-pipeline retention overrides.
func (s *Server) HandlePutPipelineRetention(w http.ResponseWriter, r *http.Request) {
	if s.Pipelines == nil {
		errorJSON(w, "not configured", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	ns := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	pipeline, err := s.Pipelines.GetPipeline(r.Context(), ns, layer, name)
	if err != nil {
		internalError(w, "failed to get pipeline", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	var overrides json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&overrides); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if err := s.Pipelines.UpdatePipelineRetention(r.Context(), pipeline.ID, overrides); err != nil {
		internalError(w, "failed to update pipeline retention", err)
		return
	}

	// Invalidate pipeline cache after retention config change.
	if s.PipelineCache != nil {
		s.PipelineCache.Delete(pipelineCacheKey(ns, layer, name))
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleGetZoneLifecycle returns landing zone lifecycle settings.
func (s *Server) HandleGetZoneLifecycle(w http.ResponseWriter, r *http.Request) {
	if s.LandingZones == nil {
		errorJSON(w, "landing zones not configured", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	ns := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	zone, err := s.LandingZones.GetZone(r.Context(), ns, name)
	if err != nil {
		internalError(w, "failed to get zone", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, ZoneLifecycleResponse{
		ProcessedMaxAgeDays: zone.ProcessedMaxAgeDays,
		AutoPurge:           zone.AutoPurge,
	})
}

// HandlePutZoneLifecycle updates landing zone lifecycle settings.
func (s *Server) HandlePutZoneLifecycle(w http.ResponseWriter, r *http.Request) {
	if s.LandingZones == nil {
		errorJSON(w, "landing zones not configured", "UNAVAILABLE", http.StatusServiceUnavailable)
		return
	}

	ns := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	zone, err := s.LandingZones.GetZone(r.Context(), ns, name)
	if err != nil {
		internalError(w, "failed to get zone", err)
		return
	}
	if zone == nil {
		errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	var req ZoneLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid JSON body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if err := s.LandingZones.UpdateZoneLifecycle(r.Context(), zone.ID, req.ProcessedMaxAgeDays, req.AutoPurge); err != nil {
		internalError(w, "failed to update zone lifecycle", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// loadRetentionConfig loads the system retention config from settings, falling back to defaults.
// Errors from the settings store or unmarshalling are logged and result in defaults being used.
func (s *Server) loadRetentionConfig(ctx context.Context) (domain.RetentionConfig, error) {
	data, err := s.Settings.GetSetting(ctx, "retention")
	if err != nil {
		slog.Warn("loadRetentionConfig: failed to read setting, using defaults", "error", err)
		return domain.DefaultRetentionConfig(), nil
	}

	var cfg domain.RetentionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Warn("loadRetentionConfig: failed to unmarshal config, using defaults", "error", err)
		return domain.DefaultRetentionConfig(), nil
	}
	return cfg, nil
}

