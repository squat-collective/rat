package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/rat-data/rat/platform/internal/domain"
)

// contextKey is an unexported type for context value keys in this package.
type contextKey string

// webhookPlaintextTokenKey is the context key for the one-time plaintext token
// shown to the user when a webhook trigger is first created.
const webhookPlaintextTokenKey contextKey = "webhookPlaintextToken"

// PipelineTriggerStore defines the persistence interface for pipeline triggers.
type PipelineTriggerStore interface {
	ListTriggers(ctx context.Context, pipelineID uuid.UUID) ([]domain.PipelineTrigger, error)
	GetTrigger(ctx context.Context, triggerID string) (*domain.PipelineTrigger, error)
	CreateTrigger(ctx context.Context, trigger *domain.PipelineTrigger) error
	UpdateTrigger(ctx context.Context, triggerID string, update UpdateTriggerRequest) (*domain.PipelineTrigger, error)
	DeleteTrigger(ctx context.Context, triggerID string) error
	FindTriggersByLandingZone(ctx context.Context, namespace, zoneName string) ([]domain.PipelineTrigger, error)
	FindTriggersByType(ctx context.Context, triggerType string) ([]domain.PipelineTrigger, error)
	// FindTriggerByWebhookToken looks up a webhook trigger by token hash
	// (hex-encoded SHA-256). Callers must hash the plaintext via HashWebhookToken.
	FindTriggerByWebhookToken(ctx context.Context, tokenHash string) (*domain.PipelineTrigger, error)
	FindTriggersByPipelineSuccess(ctx context.Context, namespace, layer, pipeline string) ([]domain.PipelineTrigger, error)
	FindTriggersByFilePattern(ctx context.Context, namespace, zoneName string) ([]domain.PipelineTrigger, error)
	UpdateTriggerFired(ctx context.Context, triggerID string, runID uuid.UUID) error
}

// CreateTriggerRequest is the JSON body for POST /api/v1/pipelines/{namespace}/{layer}/{name}/triggers.
type CreateTriggerRequest struct {
	Type            string          `json:"type"`
	Config          json.RawMessage `json:"config"`
	Enabled         *bool           `json:"enabled"`
	CooldownSeconds *int            `json:"cooldown_seconds"`
}

// UpdateTriggerRequest is the JSON body for PUT /api/v1/pipelines/{namespace}/{layer}/{name}/triggers/{triggerID}.
type UpdateTriggerRequest struct {
	Config          *json.RawMessage `json:"config"`
	Enabled         *bool            `json:"enabled"`
	CooldownSeconds *int             `json:"cooldown_seconds"`
}

// landingZoneUploadConfig is the expected shape for landing_zone_upload trigger config.
type landingZoneUploadConfig struct {
	Namespace string `json:"namespace"`
	ZoneName  string `json:"zone_name"`
}

type cronConfig struct {
	CronExpr string `json:"cron_expr"`
}

type pipelineSuccessConfig struct {
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Pipeline  string `json:"pipeline"`
}

type webhookConfig struct {
	// TokenHash is the SHA-256 hex digest of the plaintext webhook token.
	// The plaintext token is never stored — only shown once on creation.
	TokenHash string `json:"token_hash"`
}

type filePatternConfig struct {
	Namespace string `json:"namespace"`
	ZoneName  string `json:"zone_name"`
	Pattern   string `json:"pattern"`
}

type cronDependencyConfig struct {
	CronExpr     string   `json:"cron_expr"`
	Dependencies []string `json:"dependencies"`
}

// cronParser is a standard 5-field cron parser (minute, hour, day-of-month, month, day-of-week).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// MountTriggerRoutes registers trigger endpoints nested under pipelines.
func MountTriggerRoutes(r chi.Router, srv *Server) {
	r.Get("/pipelines/{namespace}/{layer}/{name}/triggers", srv.HandleListTriggers)
	r.Post("/pipelines/{namespace}/{layer}/{name}/triggers", srv.HandleCreateTrigger)
	r.Get("/pipelines/{namespace}/{layer}/{name}/triggers/{triggerID}", srv.HandleGetTrigger)
	r.Put("/pipelines/{namespace}/{layer}/{name}/triggers/{triggerID}", srv.HandleUpdateTrigger)
	r.Delete("/pipelines/{namespace}/{layer}/{name}/triggers/{triggerID}", srv.HandleDeleteTrigger)
}

// HandleListTriggers returns all triggers for a pipeline.
func (s *Server) HandleListTriggers(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	pipeline, err := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	triggers, err := s.Triggers.ListTriggers(r.Context(), pipeline.ID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Enrich webhook triggers with computed webhook_url
	enriched := make([]map[string]interface{}, len(triggers))
	for i, t := range triggers {
		enriched[i] = triggerToResponse(t, r)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"triggers": enriched,
		"total":    len(triggers),
	})
}

// HandleGetTrigger returns a single trigger by ID.
func (s *Server) HandleGetTrigger(w http.ResponseWriter, r *http.Request) {
	triggerID := chi.URLParam(r, "triggerID")

	trigger, err := s.Triggers.GetTrigger(r.Context(), triggerID)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if trigger == nil {
		errorJSON(w, "trigger not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, triggerToResponse(*trigger, r))
}

// HandleCreateTrigger creates a new trigger for a pipeline.
func (s *Server) HandleCreateTrigger(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	pipeline, err := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	var req CreateTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.Type == "" {
		errorJSON(w, "type is required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Validate trigger type
	triggerType := domain.TriggerType(req.Type)
	if !domain.ValidTriggerType(req.Type) {
		errorJSON(w, "unknown trigger type", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	switch triggerType {
	case domain.TriggerTypeLandingZoneUpload:
		var cfg landingZoneUploadConfig
		if err := json.Unmarshal(req.Config, &cfg); err != nil || cfg.Namespace == "" || cfg.ZoneName == "" {
			errorJSON(w, "config must include namespace and zone_name", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		if s.LandingZones != nil {
			zone, err := s.LandingZones.GetZone(r.Context(), cfg.Namespace, cfg.ZoneName)
			if err != nil {
				internalError(w, "internal error", err)
				return
			}
			if zone == nil {
				errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
				return
			}
		}

	case domain.TriggerTypeCron:
		var cfg cronConfig
		if err := json.Unmarshal(req.Config, &cfg); err != nil || cfg.CronExpr == "" {
			errorJSON(w, "config must include cron_expr", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		if _, err := cronParser.Parse(cfg.CronExpr); err != nil {
			errorJSON(w, "invalid cron expression: "+err.Error(), "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}

	case domain.TriggerTypePipelineSuccess:
		var cfg pipelineSuccessConfig
		if err := json.Unmarshal(req.Config, &cfg); err != nil || cfg.Namespace == "" || cfg.Layer == "" || cfg.Pipeline == "" {
			errorJSON(w, "config must include namespace, layer, and pipeline", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		// Verify upstream pipeline exists
		upstream, err := s.Pipelines.GetPipeline(r.Context(), cfg.Namespace, cfg.Layer, cfg.Pipeline)
		if err != nil {
			internalError(w, "internal error", err)
			return
		}
		if upstream == nil {
			errorJSON(w, "upstream pipeline not found", "NOT_FOUND", http.StatusNotFound)
			return
		}

	case domain.TriggerTypeWebhook:
		// Auto-generate token — 32 random bytes → 64-char hex string.
		// Only the SHA-256 hash is stored; the plaintext is returned once.
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			internalError(w, "internal error", err)
			return
		}
		plaintextToken := hex.EncodeToString(tokenBytes)
		cfg := webhookConfig{TokenHash: HashWebhookToken(plaintextToken)}
		configJSON, _ := json.Marshal(cfg)
		req.Config = configJSON

		// Stash the plaintext token so we can return it once in the response.
		// We use the request context to pass it down without changing signatures.
		r = r.WithContext(context.WithValue(r.Context(), webhookPlaintextTokenKey, plaintextToken))

	case domain.TriggerTypeFilePattern:
		var cfg filePatternConfig
		if err := json.Unmarshal(req.Config, &cfg); err != nil || cfg.Namespace == "" || cfg.ZoneName == "" || cfg.Pattern == "" {
			errorJSON(w, "config must include namespace, zone_name, and pattern", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		// Verify glob pattern compiles
		if _, err := filepath.Match(cfg.Pattern, "test"); err != nil {
			errorJSON(w, "invalid glob pattern: "+err.Error(), "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		if s.LandingZones != nil {
			zone, err := s.LandingZones.GetZone(r.Context(), cfg.Namespace, cfg.ZoneName)
			if err != nil {
				internalError(w, "internal error", err)
				return
			}
			if zone == nil {
				errorJSON(w, "landing zone not found", "NOT_FOUND", http.StatusNotFound)
				return
			}
		}

	case domain.TriggerTypeCronDependency:
		var cfg cronDependencyConfig
		if err := json.Unmarshal(req.Config, &cfg); err != nil || cfg.CronExpr == "" || len(cfg.Dependencies) == 0 {
			errorJSON(w, "config must include cron_expr and at least one dependency", "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		if _, err := cronParser.Parse(cfg.CronExpr); err != nil {
			errorJSON(w, "invalid cron expression: "+err.Error(), "INVALID_ARGUMENT", http.StatusBadRequest)
			return
		}
		// Validate each dependency pipeline exists (format: "ns.layer.pipeline")
		for _, dep := range cfg.Dependencies {
			parts := strings.SplitN(dep, ".", 3)
			if len(parts) != 3 {
				errorJSON(w, "dependency must be in format namespace.layer.pipeline: "+dep, "INVALID_ARGUMENT", http.StatusBadRequest)
				return
			}
			p, err := s.Pipelines.GetPipeline(r.Context(), parts[0], parts[1], parts[2])
			if err != nil {
				internalError(w, "internal error", err)
				return
			}
			if p == nil {
				errorJSON(w, "dependency pipeline not found: "+dep, "NOT_FOUND", http.StatusNotFound)
				return
			}
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	cooldown := 0
	if req.CooldownSeconds != nil {
		cooldown = *req.CooldownSeconds
	}

	trigger := &domain.PipelineTrigger{
		PipelineID:      pipeline.ID,
		Type:            triggerType,
		Config:          req.Config,
		Enabled:         enabled,
		CooldownSeconds: cooldown,
	}

	if err := s.Triggers.CreateTrigger(r.Context(), trigger); err != nil {
		internalError(w, "internal error", err)
		return
	}

	writeJSON(w, http.StatusCreated, triggerToResponse(*trigger, r))
}

// HandleUpdateTrigger updates a trigger's config, enabled state, or cooldown.
func (s *Server) HandleUpdateTrigger(w http.ResponseWriter, r *http.Request) {
	triggerID := chi.URLParam(r, "triggerID")

	var req UpdateTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	trigger, err := s.Triggers.UpdateTrigger(r.Context(), triggerID, req)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if trigger == nil {
		errorJSON(w, "trigger not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, trigger)
}

// HandleDeleteTrigger deletes a trigger.
func (s *Server) HandleDeleteTrigger(w http.ResponseWriter, r *http.Request) {
	triggerID := chi.URLParam(r, "triggerID")

	if err := s.Triggers.DeleteTrigger(r.Context(), triggerID); err != nil {
		internalError(w, "internal error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// triggerToResponse converts a domain trigger to a JSON-serializable map,
// enriching webhook triggers with a computed webhook_url.
//
// For webhook triggers the plaintext token is ONLY included when the request
// context carries the one-time value (i.e. at creation time). Subsequent
// reads never expose the token or its hash.
func triggerToResponse(t domain.PipelineTrigger, r *http.Request) map[string]interface{} {
	resp := map[string]interface{}{
		"id":               t.ID,
		"pipeline_id":      t.PipelineID,
		"type":             t.Type,
		"config":           json.RawMessage(t.Config),
		"enabled":          t.Enabled,
		"cooldown_seconds": t.CooldownSeconds,
		"last_triggered_at": t.LastTriggeredAt,
		"last_run_id":      t.LastRunID,
		"created_at":       t.CreatedAt,
		"updated_at":       t.UpdatedAt,
	}
	if t.Type == domain.TriggerTypeWebhook {
		scheme := "http"
		if r != nil && r.TLS != nil {
			scheme = "https"
		}
		if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		}
		host := r.Host
		// Token is no longer in the URL path — callers must pass it via
		// X-Webhook-Token header or Authorization: Bearer <token>.
		resp["webhook_url"] = scheme + "://" + host + "/api/v1/webhooks"

		// Only include the plaintext token on creation (one-time display).
		if plaintext, ok := r.Context().Value(webhookPlaintextTokenKey).(string); ok && plaintext != "" {
			resp["webhook_token"] = plaintext
		}
	}
	return resp
}

// HandleEvaluateLandingZoneTriggers is the exported entry point for evaluating
// landing zone triggers. Used by the upload handler and tests.
func (s *Server) HandleEvaluateLandingZoneTriggers(ctx context.Context, namespace, zoneName, filename string) {
	s.evaluateLandingZoneTriggers(ctx, namespace, zoneName, filename)
}

// evaluateLandingZoneTriggers checks for triggers matching a landing zone upload
// and fires pipeline runs for each one that passes its cooldown.
// Also evaluates file_pattern triggers for the same zone.
func (s *Server) evaluateLandingZoneTriggers(ctx context.Context, namespace, zoneName, filename string) {
	triggers, err := s.Triggers.FindTriggersByLandingZone(ctx, namespace, zoneName)
	if err != nil {
		slog.Error("failed to find landing zone triggers", "namespace", namespace, "zone", zoneName, "error", err)
		return
	}

	now := time.Now()
	for _, trigger := range triggers {
		s.fireTriggerIfReady(ctx, trigger, now, "trigger:landing_zone_upload:"+namespace+"/"+zoneName)
	}

	// Evaluate file_pattern triggers for this zone
	if filename != "" {
		fpTriggers, err := s.Triggers.FindTriggersByFilePattern(ctx, namespace, zoneName)
		if err != nil {
			slog.Error("failed to find file pattern triggers", "namespace", namespace, "zone", zoneName, "error", err)
			return
		}
		for _, trigger := range fpTriggers {
			var cfg filePatternConfig
			if err := json.Unmarshal(trigger.Config, &cfg); err != nil {
				slog.Warn("invalid file_pattern trigger config", "trigger_id", trigger.ID, "error", err)
				continue
			}
			matched, err := filepath.Match(cfg.Pattern, filename)
			if err != nil || !matched {
				slog.Debug("file does not match pattern", "trigger_id", trigger.ID, "pattern", cfg.Pattern, "filename", filename)
				continue
			}
			s.fireTriggerIfReady(ctx, trigger, now, "trigger:file_pattern:"+namespace+"/"+zoneName+":"+cfg.Pattern)
		}
	}
}

// EvaluatePipelineSuccessTriggers finds and fires triggers that depend on a
// pipeline completing successfully. Called by the executor callback.
func (s *Server) EvaluatePipelineSuccessTriggers(ctx context.Context, run *domain.Run) {
	pipeline, err := s.Pipelines.GetPipelineByID(ctx, run.PipelineID.String())
	if err != nil || pipeline == nil {
		return
	}

	triggers, err := s.Triggers.FindTriggersByPipelineSuccess(ctx, pipeline.Namespace, string(pipeline.Layer), pipeline.Name)
	if err != nil {
		slog.Error("failed to find pipeline_success triggers", "pipeline", pipeline.Namespace+"/"+string(pipeline.Layer)+"/"+pipeline.Name, "error", err)
		return
	}

	now := time.Now()
	for _, trigger := range triggers {
		triggerLabel := "trigger:pipeline_success:" + pipeline.Namespace + "/" + string(pipeline.Layer) + "/" + pipeline.Name
		s.fireTriggerIfReady(ctx, trigger, now, triggerLabel)
	}
}

// fireTriggerIfReady checks cooldown, creates a run, submits to executor, and updates trigger state.
func (s *Server) fireTriggerIfReady(ctx context.Context, trigger domain.PipelineTrigger, now time.Time, triggerLabel string) {
	// Check cooldown
	if trigger.CooldownSeconds > 0 && trigger.LastTriggeredAt != nil {
		cooldownEnd := trigger.LastTriggeredAt.Add(time.Duration(trigger.CooldownSeconds) * time.Second)
		if now.Before(cooldownEnd) {
			slog.Debug("trigger cooldown active, skipping",
				"trigger_id", trigger.ID, "cooldown_until", cooldownEnd)
			return
		}
	}

	// Look up pipeline
	pipeline, err := s.Pipelines.GetPipelineByID(ctx, trigger.PipelineID.String())
	if err != nil {
		slog.Error("failed to get pipeline for trigger", "trigger_id", trigger.ID, "error", err)
		return
	}
	if pipeline == nil {
		slog.Warn("trigger references missing pipeline", "trigger_id", trigger.ID, "pipeline_id", trigger.PipelineID)
		return
	}

	// Create run
	run := &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    triggerLabel,
	}

	if err := s.Runs.CreateRun(ctx, run); err != nil {
		slog.Error("failed to create triggered run", "trigger_id", trigger.ID, "error", err)
		return
	}

	// Submit to executor
	if s.Executor != nil {
		if err := s.Executor.Submit(ctx, run, pipeline); err != nil {
			slog.Error("executor submit failed for triggered run", "run_id", run.ID, "error", err)
		}
	}

	// Update trigger fired state
	if err := s.Triggers.UpdateTriggerFired(ctx, trigger.ID.String(), run.ID); err != nil {
		slog.Error("failed to update trigger fired state", "trigger_id", trigger.ID, "error", err)
	}

	slog.Info("trigger fired", "trigger_id", trigger.ID, "trigger_type", trigger.Type, "run_id", run.ID)
}
