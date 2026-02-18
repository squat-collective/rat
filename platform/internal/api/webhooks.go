package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rat-data/rat/platform/internal/domain"
)

// MountWebhookRoutes registers the webhook trigger endpoint.
// This is mounted outside the auth middleware — the token IS the auth.
//
// CHANGED (v2): Token moved from URL path to header for security.
// Tokens in URL paths get logged by proxies, load balancers, and access logs,
// leaking secrets. The token is now read from the X-Webhook-Token header,
// or from Authorization: Bearer <token>.
// Old route: POST /api/v1/webhooks/{token}
// New route: POST /api/v1/webhooks
func MountWebhookRoutes(r chi.Router, srv *Server) {
	r.Post("/api/v1/webhooks", srv.HandleWebhookTrigger)
}

// HandleWebhookTrigger handles incoming webhook requests.
// Token-authenticated via header: reads X-Webhook-Token or Authorization: Bearer <token>.
//
// Security: The plaintext token is hashed (SHA-256) before the database lookup.
// After retrieval the stored hash is verified again via constant-time comparison
// to guard against timing side-channels.
func (s *Server) HandleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	token := extractWebhookToken(r)
	if token == "" {
		errorJSON(w, "missing token: set X-Webhook-Token header or Authorization: Bearer <token>", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	// Hash the incoming token — we never query by plaintext.
	tokenHash := HashWebhookToken(token)

	trigger, err := s.Triggers.FindTriggerByWebhookToken(r.Context(), tokenHash)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if trigger == nil {
		errorJSON(w, "not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Constant-time comparison as a second verification against the stored hash.
	var cfg webhookConfig
	if err := json.Unmarshal(trigger.Config, &cfg); err != nil || !webhookTokenHashesEqual(tokenHash, cfg.TokenHash) {
		errorJSON(w, "not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Check cooldown
	now := time.Now()
	if trigger.CooldownSeconds > 0 && trigger.LastTriggeredAt != nil {
		cooldownEnd := trigger.LastTriggeredAt.Add(time.Duration(trigger.CooldownSeconds) * time.Second)
		if now.Before(cooldownEnd) {
			errorJSON(w, "cooldown active", "RESOURCE_EXHAUSTED", http.StatusTooManyRequests)
			return
		}
	}

	// Look up pipeline
	pipeline, err := s.Pipelines.GetPipelineByID(r.Context(), trigger.PipelineID.String())
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if pipeline == nil {
		errorJSON(w, "pipeline not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Create run — use a prefix of the *hash* (not the plaintext) in the label.
	hashPrefix := tokenHash
	if len(hashPrefix) > 8 {
		hashPrefix = hashPrefix[:8]
	}
	triggerLabel := "trigger:webhook:" + hashPrefix
	run := &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    triggerLabel,
	}

	if err := s.Runs.CreateRun(r.Context(), run); err != nil {
		internalError(w, "internal error", err)
		return
	}

	// Submit to executor.
	// Use a dedicated context with timeout rather than the request context
	// (which will be cancelled when the HTTP response is sent) or a bare
	// context.Background() (which has no timeout and could hang indefinitely).
	if s.Executor != nil {
		submitCtx, submitCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer submitCancel()
		if err := s.Executor.Submit(submitCtx, run, pipeline); err != nil {
			slog.Error("executor submit failed for webhook trigger", "run_id", run.ID, "error", err)
		}
	}

	// Update trigger fired state
	if err := s.Triggers.UpdateTriggerFired(r.Context(), trigger.ID.String(), run.ID); err != nil {
		slog.Error("failed to update trigger fired state", "trigger_id", trigger.ID, "error", err)
	}

	slog.Info("webhook trigger fired", "trigger_id", trigger.ID, "run_id", run.ID)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"run_id": run.ID,
	})
}

// extractWebhookToken reads the webhook token from request headers.
// It checks X-Webhook-Token first, then falls back to Authorization: Bearer <token>.
// Returns empty string if no token is found.
func extractWebhookToken(r *http.Request) string {
	// Prefer dedicated webhook header
	if token := r.Header.Get("X-Webhook-Token"); token != "" {
		return strings.TrimSpace(token)
	}

	// Fall back to Authorization: Bearer <token>
	if auth := r.Header.Get("Authorization"); auth != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
	}

	return ""
}
