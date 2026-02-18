package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// ScheduleStore defines the persistence interface for schedules.
type ScheduleStore interface {
	ListSchedules(ctx context.Context) ([]domain.Schedule, error)
	GetSchedule(ctx context.Context, id string) (*domain.Schedule, error)
	CreateSchedule(ctx context.Context, schedule *domain.Schedule) error
	UpdateSchedule(ctx context.Context, id string, update UpdateScheduleRequest) (*domain.Schedule, error)
	UpdateScheduleRun(ctx context.Context, id string, lastRunID string, lastRunAt time.Time, nextRunAt time.Time) error
	DeleteSchedule(ctx context.Context, id string) error
}

// CreateScheduleRequest is the JSON body for POST /api/v1/schedules.
type CreateScheduleRequest struct {
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Pipeline  string `json:"pipeline"`
	Cron      string `json:"cron"`
	Enabled   *bool  `json:"enabled"`
}

// UpdateScheduleRequest is the JSON body for PUT /api/v1/schedules/:id.
type UpdateScheduleRequest struct {
	Cron    *string `json:"cron"`
	Enabled *bool   `json:"enabled"`
}

// MountScheduleRoutes registers schedule endpoints on the router.
func MountScheduleRoutes(r chi.Router, srv *Server) {
	r.Get("/schedules", srv.HandleListSchedules)
	r.Post("/schedules", srv.HandleCreateSchedule)
	r.Get("/schedules/{scheduleID}", srv.HandleGetSchedule)
	r.Put("/schedules/{scheduleID}", srv.HandleUpdateSchedule)
	r.Delete("/schedules/{scheduleID}", srv.HandleDeleteSchedule)
}

// HandleListSchedules returns all schedules.
func (s *Server) HandleListSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := s.Schedules.ListSchedules(r.Context())
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	total := len(schedules)
	limit, offset := parsePagination(r)
	schedules = paginate(schedules, limit, offset)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"schedules": schedules,
		"total":     total,
	})
}

// HandleGetSchedule returns a single schedule by ID.
func (s *Server) HandleGetSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "scheduleID")

	schedule, err := s.Schedules.GetSchedule(r.Context(), id)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if schedule == nil {
		errorJSON(w, "schedule not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, schedule)
}

// HandleCreateSchedule creates a schedule for a pipeline.
func (s *Server) HandleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req CreateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.Namespace == "" || req.Layer == "" || req.Pipeline == "" || req.Cron == "" {
		errorJSON(w, "namespace, layer, pipeline, and cron are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(req.Namespace) || !validName(req.Pipeline) {
		errorJSON(w, "namespace and pipeline must be a lowercase slug (a-z, 0-9, hyphens, underscores; must start with a letter)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !domain.ValidLayer(req.Layer) {
		errorJSON(w, "layer must be bronze, silver, or gold", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if _, err := cronParser.Parse(req.Cron); err != nil {
		errorJSON(w, "invalid cron expression: "+err.Error(), "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Verify pipeline exists
	pipeline, err := s.Pipelines.GetPipeline(r.Context(), req.Namespace, req.Layer, req.Pipeline)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	schedule := &domain.Schedule{
		PipelineID: pipeline.ID,
		CronExpr:   req.Cron,
		Enabled:    enabled,
	}

	if err := s.Schedules.CreateSchedule(r.Context(), schedule); err != nil {
		internalError(w, "internal error", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":      schedule.ID.String(),
		"cron":    schedule.CronExpr,
		"enabled": schedule.Enabled,
	})
}

// HandleUpdateSchedule updates a schedule's cron or enabled state.
func (s *Server) HandleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "scheduleID")

	var req UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	schedule, err := s.Schedules.UpdateSchedule(r.Context(), id, req)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if schedule == nil {
		errorJSON(w, "schedule not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, schedule)
}

// HandleDeleteSchedule deletes a schedule.
func (s *Server) HandleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "scheduleID")

	// Check existence first to return 404 instead of 500 for missing schedules.
	schedule, err := s.Schedules.GetSchedule(r.Context(), id)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if schedule == nil {
		errorJSON(w, "schedule not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	if err := s.Schedules.DeleteSchedule(r.Context(), id); err != nil {
		internalError(w, "internal error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
