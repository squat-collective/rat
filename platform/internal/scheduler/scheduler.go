// Package scheduler evaluates cron schedules and fires pipeline runs.
// It runs as a background goroutine inside ratd, checking enabled schedules
// at a configurable interval (default 30s).
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/executor"
	"golang.org/x/sync/errgroup"
)

// EventPublisher publishes events to the event bus.
type EventPublisher interface {
	Publish(ctx context.Context, channel string, payload interface{}) error
}

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
	EventBus  EventPublisher // Optional: publishes schedule_fired events when set.
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
		// SecondOptional lets the parser accept BOTH 5-field cron (minute
		// granularity, e.g. "0 * * * *") and 6-field cron with leading
		// seconds (e.g. "*/30 * * * * *" for every 30s). Required for
		// the demo's live-ingestion pipeline; legacy schedules keep
		// working unchanged.
		parser:    cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
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

// maxConcurrentScheduleDispatches caps the number of in-flight submit
// RPCs the scheduler will fan out per tick. Matches the runner's default
// concurrent-run capacity (RUNNER_MAX_CONCURRENT=10) — exceeding it just
// produces RESOURCE_EXHAUSTED (ErrRunnerBusy) replies the next tick will
// retry. Replaces an older per-submission 3 s sleep that was a workaround
// for a DuckDB crash *inside the runner's execution path* (many parallel
// DuckDB connections "terminate called without an active exception" — see
// examples/rat-plugin-demo-loader/install.go for the user-facing mirror).
// The scheduler talks to the runner over gRPC, and the runner already
// serializes submissions under a lock and rejects beyond its concurrency
// cap, so per-tick fan-out at this limit is safe and far faster than the
// old serial-with-sleep behaviour (which made 100 same-tick schedules
// take 5+ minutes — by which point the next tick was already overdue).
const maxConcurrentScheduleDispatches = 10

// dueDispatch carries the data needed to fire a single schedule. We
// build the list synchronously (preserving the existing sequential reads
// of stores) and then fan out the actual executor.Submit calls.
type dueDispatch struct {
	schedule domain.Schedule
	pipeline *domain.Pipeline
	run      *domain.Run
	nextRun  time.Time
}

// tick evaluates all enabled schedules and fires runs that are due.
//
// Two phases:
//  1. Sequential planning: walk every schedule, parse cron, skip
//     not-due/disabled/duplicate, look up the pipeline, create the run
//     row. The planning phase is intentionally serial — store calls
//     touch shared Postgres state and are cheap.
//  2. Concurrent dispatch: fan out executor.Submit calls through an
//     errgroup capped at maxConcurrentScheduleDispatches so 100 same-tick
//     schedules dispatch in <2 s instead of >5 min.
func (s *Scheduler) tick(ctx context.Context) {
	schedules, err := s.schedules.ListSchedules(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list schedules", "error", err)
		return
	}

	now := time.Now()
	dispatches := make([]dueDispatch, 0, len(schedules))

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

		// Create the run row up-front (in pending state) so it's visible
		// in the UI even if the downstream dispatch is rejected/delayed.
		run := &domain.Run{
			PipelineID: pipeline.ID,
			Status:     domain.RunStatusPending,
			Trigger:    "schedule:" + sched.CronExpr,
		}
		if err := s.runs.CreateRun(ctx, run); err != nil {
			slog.Error("scheduler: failed to create run", "schedule_id", sched.ID, "error", err)
			continue
		}

		// Compute next-fire time from NOW (catch up once, then advance to future).
		dispatches = append(dispatches, dueDispatch{
			schedule: sched,
			pipeline: pipeline,
			run:      run,
			nextRun:  cronSched.Next(now),
		})
	}

	if len(dispatches) == 0 {
		return
	}

	s.dispatchDue(ctx, now, dispatches)
}

// dispatchDue fans out the actual executor.Submit calls for the planned
// dispatches, capped at maxConcurrentScheduleDispatches in flight. Each
// successful dispatch advances its schedule's next_run_at; ErrRunnerBusy
// leaves the schedule alone so the next tick retries. Other submit
// errors are logged but the schedule still advances (the run row was
// already created in the planning phase).
func (s *Scheduler) dispatchDue(ctx context.Context, now time.Time, dispatches []dueDispatch) {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentScheduleDispatches)

	var mu sync.Mutex // serialises slog calls only — not required for correctness, just neater output.

	for _, d := range dispatches {
		d := d // capture for closure
		g.Go(func() error {
			return s.dispatchOne(gctx, now, d, &mu)
		})
	}

	// We never return an error from dispatchOne (each path logs its own
	// failure), so g.Wait should always be nil — but keep the defensive
	// check in case future contributors propagate one.
	if err := g.Wait(); err != nil {
		slog.Warn("scheduler: at least one dispatch failed", "error", err)
	}
}

// dispatchOne submits a single planned dispatch and advances (or not)
// the schedule's next_run_at accordingly. Always returns nil — the
// errgroup is only used for concurrency control, not error propagation.
func (s *Scheduler) dispatchOne(ctx context.Context, now time.Time, d dueDispatch, mu *sync.Mutex) error {
	if err := s.executor.Submit(ctx, d.run, d.pipeline); err != nil {
		// If the runner is at capacity, don't advance the schedule —
		// the next tick will retry. The run stays in pending state.
		if errors.Is(err, executor.ErrRunnerBusy) {
			mu.Lock()
			slog.Warn("scheduler: runner busy, will retry next tick",
				"schedule_id", d.schedule.ID, "run_id", d.run.ID)
			mu.Unlock()
			return nil
		}
		mu.Lock()
		slog.Error("scheduler: executor submit failed", "run_id", d.run.ID, "error", err)
		mu.Unlock()
		// Fall through — run was created, just not dispatched. The
		// schedule still advances so we don't fire the same slot again
		// next tick.
	}

	// Publish schedule_fired event (best-effort).
	if s.EventBus != nil {
		_ = s.EventBus.Publish(ctx, "schedule_fired", map[string]interface{}{
			"schedule_id": d.schedule.ID.String(),
			"pipeline_id": d.schedule.PipelineID.String(),
			"run_id":      d.run.ID.String(),
			"cron_expr":   d.schedule.CronExpr,
		})
	}

	if err := s.schedules.UpdateScheduleRun(ctx, d.schedule.ID.String(), d.run.ID.String(), now, d.nextRun); err != nil {
		mu.Lock()
		slog.Error("scheduler: failed to update schedule run", "schedule_id", d.schedule.ID, "error", err)
		mu.Unlock()
		return nil
	}

	mu.Lock()
	slog.Info("scheduler: fired run", "schedule_id", d.schedule.ID, "run_id", d.run.ID, "next_run_at", d.nextRun)
	mu.Unlock()
	return nil
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
