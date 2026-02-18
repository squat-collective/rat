// Package scheduler evaluates cron schedules and fires pipeline runs.
// It runs as a background goroutine inside ratd, checking enabled schedules
// at a configurable interval (default 30s).
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/executor"
)

// Scheduler checks enabled schedules and fires runs when they're due.
type Scheduler struct {
	schedules api.ScheduleStore
	pipelines api.PipelineStore
	runs      api.RunStore
	executor  api.Executor
	interval  time.Duration
	parser    cron.Parser
	cancel    context.CancelFunc
	done      chan struct{}
}

// New creates a Scheduler with the given stores and check interval.
func New(
	schedules api.ScheduleStore,
	pipelines api.PipelineStore,
	runs api.RunStore,
	executor api.Executor,
	interval time.Duration,
) *Scheduler {
	return &Scheduler{
		schedules: schedules,
		pipelines: pipelines,
		runs:      runs,
		executor:  executor,
		interval:  interval,
		parser:    cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Start begins the background scheduler goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})

	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	}()
}

// Stop cancels the background goroutine and waits for it to finish.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.done != nil {
		<-s.done
	}
}

// tick evaluates all enabled schedules and fires runs that are due.
func (s *Scheduler) tick(ctx context.Context) {
	schedules, err := s.schedules.ListSchedules(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list schedules", "error", err)
		return
	}

	now := time.Now()

	for _, sched := range schedules {
		if !sched.Enabled {
			continue
		}

		// Parse the cron expression
		cronSched, err := s.parser.Parse(sched.CronExpr)
		if err != nil {
			slog.Warn("scheduler: invalid cron expression", "schedule_id", sched.ID, "cron", sched.CronExpr, "error", err)
			continue
		}

		// If next_run_at is nil, compute it and set it (don't fire)
		if sched.NextRunAt == nil {
			nextRun := cronSched.Next(now)
			if err := s.schedules.UpdateScheduleRun(ctx, sched.ID.String(), "", now, nextRun); err != nil {
				slog.Error("scheduler: failed to set initial next_run_at", "schedule_id", sched.ID, "error", err)
			}
			continue
		}

		// Not due yet
		if sched.NextRunAt.After(now) {
			continue
		}

		// Due — look up the pipeline
		pipeline, err := s.pipelines.GetPipelineByID(ctx, sched.PipelineID.String())
		if err != nil {
			slog.Error("scheduler: failed to get pipeline", "schedule_id", sched.ID, "pipeline_id", sched.PipelineID, "error", err)
			continue
		}
		if pipeline == nil {
			slog.Warn("scheduler: pipeline not found for schedule", "schedule_id", sched.ID, "pipeline_id", sched.PipelineID)
			continue
		}

		// Skip if pipeline already has a pending or running run — avoids
		// piling up duplicate runs when the runner is slow or at capacity.
		if s.hasActiveRun(ctx, sched.PipelineID.String()) {
			slog.Debug("scheduler: skipping — pipeline already has an active run",
				"schedule_id", sched.ID, "pipeline_id", sched.PipelineID)
			continue
		}

		// Create the run
		run := &domain.Run{
			PipelineID: pipeline.ID,
			Status:     domain.RunStatusPending,
			Trigger:    "schedule:" + sched.CronExpr,
		}
		if err := s.runs.CreateRun(ctx, run); err != nil {
			slog.Error("scheduler: failed to create run", "schedule_id", sched.ID, "error", err)
			continue
		}

		// Submit to executor
		if err := s.executor.Submit(ctx, run, pipeline); err != nil {
			// If the runner is at capacity, don't advance the schedule —
			// the next tick will retry. The run stays in pending state.
			if errors.Is(err, executor.ErrRunnerBusy) {
				slog.Warn("scheduler: runner busy, will retry next tick",
					"schedule_id", sched.ID, "run_id", run.ID)
				continue
			}
			slog.Error("scheduler: executor submit failed", "run_id", run.ID, "error", err)
			// Continue — run was created, just not dispatched
		}

		// Compute the next run time from NOW (catch up once, then advance to future)
		nextRun := cronSched.Next(now)
		if err := s.schedules.UpdateScheduleRun(ctx, sched.ID.String(), run.ID.String(), now, nextRun); err != nil {
			slog.Error("scheduler: failed to update schedule run", "schedule_id", sched.ID, "error", err)
		}

		slog.Info("scheduler: fired run", "schedule_id", sched.ID, "run_id", run.ID, "next_run_at", nextRun)
	}
}

// hasActiveRun checks whether the given pipeline already has a pending or
// running run. Used to avoid scheduling duplicate runs when the runner is
// slow or at capacity.
func (s *Scheduler) hasActiveRun(ctx context.Context, pipelineID string) bool {
	// Check pending runs
	pendingRuns, err := s.runs.ListRuns(ctx, api.RunFilter{
		PipelineID: pipelineID,
		Status:     string(domain.RunStatusPending),
		Limit:      1,
	})
	if err != nil {
		slog.Warn("scheduler: failed to check pending runs", "pipeline_id", pipelineID, "error", err)
		return false // on error, allow the run (don't block scheduling)
	}
	if len(pendingRuns) > 0 {
		return true
	}

	// Check running runs
	runningRuns, err := s.runs.ListRuns(ctx, api.RunFilter{
		PipelineID: pipelineID,
		Status:     string(domain.RunStatusRunning),
		Limit:      1,
	})
	if err != nil {
		slog.Warn("scheduler: failed to check running runs", "pipeline_id", pipelineID, "error", err)
		return false
	}
	return len(runningRuns) > 0
}
