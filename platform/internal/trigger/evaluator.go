// Package trigger provides the trigger evaluator for cron-based pipeline triggers.
// It runs as a background goroutine inside ratd, evaluating cron and cron_dependency
// triggers at a configurable interval (default 30s).
//
// When an EventBus is wired, the evaluator also reacts instantly to run_completed
// events via Postgres LISTEN/NOTIFY, enabling sub-second trigger evaluation for
// cron_dependency triggers without reducing the poll interval.
package trigger

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres"
)

// cronConfig mirrors the config shape for cron triggers.
type cronConfig struct {
	CronExpr string `json:"cron_expr"`
}

// cronDependencyConfig mirrors the config shape for cron_dependency triggers.
type cronDependencyConfig struct {
	CronExpr     string   `json:"cron_expr"`
	Dependencies []string `json:"dependencies"` // "ns.layer.pipeline"
}

// Evaluator checks cron and cron_dependency triggers and fires runs when they're due.
type Evaluator struct {
	triggers  api.PipelineTriggerStore
	pipelines api.PipelineStore
	runs      api.RunStore
	executor  api.Executor
	interval  time.Duration
	parser    cron.Parser

	// EventCh receives run_completed events from the event bus.
	// When set, the evaluator re-evaluates cron_dependency triggers
	// instantly on run completion instead of waiting for the next tick.
	EventCh       <-chan postgres.Event
	eventCancel   func() // cancel function for unsubscribing from event bus

	cancel    context.CancelFunc
	done      chan struct{}
}

// SetEventCancel sets the cancel function for unsubscribing from the event bus.
// Called by main.go after subscribing to run_completed events.
func (e *Evaluator) SetEventCancel(cancel func()) {
	e.eventCancel = cancel
}

// NewEvaluator creates an Evaluator with the given stores and check interval.
func NewEvaluator(
	triggers api.PipelineTriggerStore,
	pipelines api.PipelineStore,
	runs api.RunStore,
	executor api.Executor,
	interval time.Duration,
) *Evaluator {
	return &Evaluator{
		triggers:  triggers,
		pipelines: pipelines,
		runs:      runs,
		executor:  executor,
		interval:  interval,
		parser:    cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Start begins the background evaluator goroutine.
// If EventCh is set, it also listens for run_completed events for instant
// cron_dependency evaluation.
func (e *Evaluator) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	e.done = make(chan struct{})

	go func() {
		defer close(e.done)
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.tick(ctx)
			case event, ok := <-e.eventCh():
				if !ok {
					continue
				}
				e.handleRunCompleted(ctx, event)
			}
		}
	}()
}

// Stop cancels the background goroutine and waits for it to finish.
func (e *Evaluator) Stop() {
	if e.eventCancel != nil {
		e.eventCancel()
	}
	if e.cancel != nil {
		e.cancel()
	}
	if e.done != nil {
		<-e.done
	}
}

// eventCh returns the event channel or a nil channel (blocks forever) if not set.
func (e *Evaluator) eventCh() <-chan postgres.Event {
	if e.EventCh != nil {
		return e.EventCh
	}
	return nil
}

// handleRunCompleted processes a run_completed event by re-evaluating
// cron_dependency triggers that might depend on the completed pipeline.
func (e *Evaluator) handleRunCompleted(ctx context.Context, event postgres.Event) {
	var payload postgres.RunCompletedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		slog.Warn("trigger evaluator: invalid run_completed payload", "error", err)
		return
	}

	// Only react to successful runs — cron_dependency triggers care about new data.
	if payload.Status != string(domain.RunStatusSuccess) {
		return
	}

	slog.Debug("trigger evaluator: run_completed event received",
		"run_id", payload.RunID, "pipeline_id", payload.PipelineID)

	// Re-evaluate cron_dependency triggers on event — they might
	// now see new upstream data that satisfies their dependencies.
	now := time.Now()
	cdTriggers, err := e.triggers.FindTriggersByType(ctx, string(domain.TriggerTypeCronDependency))
	if err != nil {
		slog.Error("trigger evaluator: failed to list cron_dependency triggers on event", "error", err)
		return
	}
	for _, t := range cdTriggers {
		e.evaluateCronDependency(ctx, t, now)
	}
}

// tick evaluates all enabled cron and cron_dependency triggers.
func (e *Evaluator) tick(ctx context.Context) {
	now := time.Now()

	// Evaluate cron triggers
	cronTriggers, err := e.triggers.FindTriggersByType(ctx, string(domain.TriggerTypeCron))
	if err != nil {
		slog.Error("trigger evaluator: failed to list cron triggers", "error", err)
	} else {
		for _, t := range cronTriggers {
			e.evaluateCron(ctx, t, now)
		}
	}

	// Evaluate cron_dependency triggers
	cdTriggers, err := e.triggers.FindTriggersByType(ctx, string(domain.TriggerTypeCronDependency))
	if err != nil {
		slog.Error("trigger evaluator: failed to list cron_dependency triggers", "error", err)
	} else {
		for _, t := range cdTriggers {
			e.evaluateCronDependency(ctx, t, now)
		}
	}
}

// evaluateCron fires a cron trigger if its schedule is due.
func (e *Evaluator) evaluateCron(ctx context.Context, t domain.PipelineTrigger, now time.Time) {
	var cfg cronConfig
	if err := json.Unmarshal(t.Config, &cfg); err != nil {
		slog.Warn("trigger evaluator: invalid cron trigger config", "trigger_id", t.ID, "error", err)
		return
	}

	cronSched, err := e.parser.Parse(cfg.CronExpr)
	if err != nil {
		slog.Warn("trigger evaluator: invalid cron expression", "trigger_id", t.ID, "cron", cfg.CronExpr, "error", err)
		return
	}

	if !e.isDue(t, cronSched, now) {
		return
	}

	e.fireAndUpdate(ctx, t, "trigger:cron:"+cfg.CronExpr)
}

// evaluateCronDependency fires a cron_dependency trigger if its schedule is due
// AND at least one upstream dependency has new successful data since last trigger.
func (e *Evaluator) evaluateCronDependency(ctx context.Context, t domain.PipelineTrigger, now time.Time) {
	var cfg cronDependencyConfig
	if err := json.Unmarshal(t.Config, &cfg); err != nil {
		slog.Warn("trigger evaluator: invalid cron_dependency trigger config", "trigger_id", t.ID, "error", err)
		return
	}

	cronSched, err := e.parser.Parse(cfg.CronExpr)
	if err != nil {
		slog.Warn("trigger evaluator: invalid cron expression", "trigger_id", t.ID, "cron", cfg.CronExpr, "error", err)
		return
	}

	if !e.isDue(t, cronSched, now) {
		return
	}

	// Check if any dependency has a successful run after last_triggered_at
	hasNewData := false
	for _, dep := range cfg.Dependencies {
		parts := strings.SplitN(dep, ".", 3)
		if len(parts) != 3 {
			continue
		}
		ns, layer, name := parts[0], parts[1], parts[2]

		runs, err := e.runs.ListRuns(ctx, api.RunFilter{
			Namespace: ns,
			Layer:     layer,
			Pipeline:  name,
			Status:    string(domain.RunStatusSuccess),
		})
		if err != nil {
			slog.Warn("trigger evaluator: failed to list runs for dependency",
				"trigger_id", t.ID, "dependency", dep, "error", err)
			continue
		}

		// Check if the latest successful run finished after last_triggered_at
		for _, run := range runs {
			if run.FinishedAt != nil {
				if t.LastTriggeredAt == nil || run.FinishedAt.After(*t.LastTriggeredAt) {
					hasNewData = true
					break
				}
			}
		}
		if hasNewData {
			break
		}
	}

	if !hasNewData {
		slog.Debug("trigger evaluator: cron_dependency skipped (no new data)",
			"trigger_id", t.ID)
		return
	}

	e.fireAndUpdate(ctx, t, "trigger:cron_dependency:"+cfg.CronExpr)
}

// isDue checks whether a trigger's cron schedule is due based on last_triggered_at.
// Uses catch-up-once policy: if last_triggered_at is nil, initialize and don't fire.
func (e *Evaluator) isDue(t domain.PipelineTrigger, cronSched cron.Schedule, now time.Time) bool {
	if t.LastTriggeredAt == nil {
		// First time — don't fire, just record when the next run should be.
		// We'll fire on the next tick after the computed time.
		return false
	}

	// Fire if the next scheduled time after last trigger is in the past or now
	nextRun := cronSched.Next(*t.LastTriggeredAt)
	return !nextRun.After(now)
}

// fireAndUpdate looks up the pipeline, creates a run, submits it, and updates trigger state.
func (e *Evaluator) fireAndUpdate(ctx context.Context, t domain.PipelineTrigger, triggerLabel string) {
	pipeline, err := e.pipelines.GetPipelineByID(ctx, t.PipelineID.String())
	if err != nil {
		slog.Error("trigger evaluator: failed to get pipeline", "trigger_id", t.ID, "pipeline_id", t.PipelineID, "error", err)
		return
	}
	if pipeline == nil {
		slog.Warn("trigger evaluator: pipeline not found for trigger", "trigger_id", t.ID, "pipeline_id", t.PipelineID)
		return
	}

	run := &domain.Run{
		PipelineID: pipeline.ID,
		Status:     domain.RunStatusPending,
		Trigger:    triggerLabel,
	}

	if err := e.runs.CreateRun(ctx, run); err != nil {
		slog.Error("trigger evaluator: failed to create run", "trigger_id", t.ID, "error", err)
		return
	}

	if err := e.executor.Submit(ctx, run, pipeline); err != nil {
		slog.Error("trigger evaluator: executor submit failed", "run_id", run.ID, "error", err)
	}

	if err := e.triggers.UpdateTriggerFired(ctx, t.ID.String(), run.ID); err != nil {
		slog.Error("trigger evaluator: failed to update trigger fired state", "trigger_id", t.ID, "error", err)
	}

	slog.Info("trigger evaluator: fired run", "trigger_id", t.ID, "trigger_type", t.Type, "run_id", run.ID)
}
