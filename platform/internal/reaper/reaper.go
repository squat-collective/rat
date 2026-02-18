package reaper

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
)

// Reaper is a background daemon that enforces data retention policies.
// It periodically cleans up old runs, logs, quality results, orphan branches,
// soft-deleted pipelines, and processed landing zone files.
type Reaper struct {
	settings  api.SettingsStore
	runs      api.RunStore
	pipelines api.PipelineStore
	zones     api.LandingZoneStore
	storage   api.StorageStore
	audit     api.AuditStore
	nessie    NessieClient
	cancel    context.CancelFunc
	done      chan struct{}
}

// New creates a Reaper with the given store dependencies.
func New(
	settings api.SettingsStore,
	runs api.RunStore,
	pipelines api.PipelineStore,
	zones api.LandingZoneStore,
	storage api.StorageStore,
	audit api.AuditStore,
	nessie NessieClient,
) *Reaper {
	return &Reaper{
		settings:  settings,
		runs:      runs,
		pipelines: pipelines,
		zones:     zones,
		storage:   storage,
		audit:     audit,
		nessie:    nessie,
	}
}

// Start begins the background reaper goroutine.
// The ticker interval is re-read from the retention config after each tick,
// so changes to ReaperIntervalMinutes take effect without a restart.
func (r *Reaper) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	r.done = make(chan struct{})

	go func() {
		defer close(r.done)

		// Load initial interval
		cfg := r.loadConfig(ctx)
		interval := reaperInterval(cfg)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.tick(ctx)

				// Re-read interval from config and reset the ticker if it changed.
				newCfg := r.loadConfig(ctx)
				newInterval := reaperInterval(newCfg)
				if newInterval != interval {
					interval = newInterval
					ticker.Reset(interval)
					slog.Info("reaper: interval updated", "interval", interval)
				}
			}
		}
	}()
}

// reaperInterval returns the ticker duration from the retention config,
// clamping to a minimum of 1 minute with a default of 1 hour.
func reaperInterval(cfg domain.RetentionConfig) time.Duration {
	interval := time.Duration(cfg.ReaperIntervalMinutes) * time.Minute
	if interval < time.Minute {
		interval = time.Hour
	}
	return interval
}

// Stop cancels the background goroutine and waits for it to finish.
func (r *Reaper) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.done != nil {
		<-r.done
	}
}

// RunNow triggers a manual reaper run and returns the resulting stats.
func (r *Reaper) RunNow(ctx context.Context) (*domain.ReaperStatus, error) {
	return r.tick(ctx), nil
}

// tick executes all retention tasks. Each task is isolated — a failure in one
// does not prevent the others from running.
func (r *Reaper) tick(ctx context.Context) *domain.ReaperStatus {
	cfg := r.loadConfig(ctx)
	now := time.Now()
	status := &domain.ReaperStatus{}

	// Task 1: Prune old runs per pipeline
	r.safeRun("pruneRuns", func() {
		count := r.pruneRuns(ctx, cfg, now)
		status.RunsPruned = count
	})

	// Task 2: Fail stuck runs
	r.safeRun("failStuckRuns", func() {
		count := r.failStuckRuns(ctx, cfg, now)
		status.RunsFailed = count
	})

	// Task 3: Purge soft-deleted pipelines
	r.safeRun("purgeSoftDeleted", func() {
		count := r.purgeSoftDeletedPipelines(ctx, cfg, now)
		status.PipelinesPurged = count
	})

	// Task 4: Clean orphan Nessie branches
	r.safeRun("cleanOrphanBranches", func() {
		count := r.cleanOrphanBranches(ctx, cfg, now)
		status.BranchesCleaned = count
	})

	// Task 5: Purge processed landing zone files
	r.safeRun("purgeProcessedLZ", func() {
		count := r.purgeProcessedLZFiles(ctx, now)
		status.LZFilesCleaned = count
	})

	// Task 6: Prune audit log
	r.safeRun("pruneAuditLog", func() {
		count := r.pruneAuditLog(ctx, cfg, now)
		status.AuditPruned = count
	})

	// Save status
	if r.settings != nil {
		if err := r.settings.UpdateReaperStatus(ctx, status); err != nil {
			slog.Error("reaper: failed to update status", "error", err)
		}
	}

	slog.Info("reaper: tick complete",
		"runs_pruned", status.RunsPruned,
		"runs_failed", status.RunsFailed,
		"pipelines_purged", status.PipelinesPurged,
		"branches_cleaned", status.BranchesCleaned,
		"lz_files_cleaned", status.LZFilesCleaned,
		"audit_pruned", status.AuditPruned,
	)

	return status
}

// pruneRuns deletes runs beyond the per-pipeline limit and past the max age.
func (r *Reaper) pruneRuns(ctx context.Context, cfg domain.RetentionConfig, now time.Time) int {
	if r.runs == nil || r.pipelines == nil {
		return 0
	}

	total := 0

	// Per-pipeline count-based pruning
	pipelines, err := r.pipelines.ListPipelines(ctx, api.PipelineFilter{})
	if err != nil {
		slog.Error("reaper: failed to list pipelines for run pruning", "error", err)
		return 0
	}

	for _, p := range pipelines {
		count, err := r.runs.DeleteRunsBeyondLimit(ctx, p.ID, cfg.RunsMaxPerPipeline)
		if err != nil {
			slog.Warn("reaper: failed to prune runs for pipeline", "pipeline_id", p.ID, "error", err)
			continue
		}
		total += count
	}

	// Age-based pruning
	if cfg.RunsMaxAgeDays > 0 {
		cutoff := now.Add(-time.Duration(cfg.RunsMaxAgeDays) * 24 * time.Hour)
		count, err := r.runs.DeleteRunsOlderThan(ctx, cutoff)
		if err != nil {
			slog.Error("reaper: failed to delete old runs", "error", err)
		} else {
			total += count
		}
	}

	return total
}

// failStuckRuns marks pending/running runs as failed if they exceed the timeout.
func (r *Reaper) failStuckRuns(ctx context.Context, cfg domain.RetentionConfig, now time.Time) int {
	if r.runs == nil {
		return 0
	}

	cutoff := now.Add(-time.Duration(cfg.StuckRunTimeoutMinutes) * time.Minute)
	stuckRuns, err := r.runs.ListStuckRuns(ctx, cutoff)
	if err != nil {
		slog.Error("reaper: failed to list stuck runs", "error", err)
		return 0
	}

	count := 0
	for _, run := range stuckRuns {
		errMsg := "run timed out (stuck for too long)"
		if err := r.runs.UpdateRunStatus(ctx, run.ID.String(), domain.RunStatusFailed, &errMsg, nil, nil); err != nil {
			slog.Warn("reaper: failed to fail stuck run", "run_id", run.ID, "error", err)
			continue
		}
		count++
	}
	return count
}

// purgeSoftDeletedPipelines hard-deletes pipelines that were soft-deleted beyond the purge period.
func (r *Reaper) purgeSoftDeletedPipelines(ctx context.Context, cfg domain.RetentionConfig, now time.Time) int {
	if r.pipelines == nil {
		return 0
	}

	cutoff := now.Add(-time.Duration(cfg.SoftDeletePurgeDays) * 24 * time.Hour)
	pipelines, err := r.pipelines.ListSoftDeletedPipelines(ctx, cutoff)
	if err != nil {
		slog.Error("reaper: failed to list soft-deleted pipelines", "error", err)
		return 0
	}

	count := 0
	for _, p := range pipelines {
		// Delete S3 files first (best-effort)
		if r.storage != nil && p.S3Path != "" {
			files, err := r.storage.ListFiles(ctx, p.S3Path)
			if err == nil {
				for _, f := range files {
					_ = r.storage.DeleteFile(ctx, f.Path)
				}
			}
		}

		if err := r.pipelines.HardDeletePipeline(ctx, p.ID); err != nil {
			slog.Warn("reaper: failed to hard-delete pipeline", "pipeline_id", p.ID, "error", err)
			continue
		}
		count++
	}
	return count
}

// cleanOrphanBranches deletes Nessie branches named "run-*" that have no active run.
func (r *Reaper) cleanOrphanBranches(ctx context.Context, _ domain.RetentionConfig, _ time.Time) int {
	if r.nessie == nil || r.runs == nil {
		return 0
	}

	branches, err := r.nessie.ListBranches(ctx)
	if err != nil {
		slog.Error("reaper: failed to list Nessie branches", "error", err)
		return 0
	}

	count := 0
	for _, b := range branches {
		if !strings.HasPrefix(b.Name, "run-") {
			continue
		}

		// Extract run ID from branch name
		runID := strings.TrimPrefix(b.Name, "run-")
		run, err := r.runs.GetRun(ctx, runID)
		if err != nil {
			slog.Warn("reaper: failed to check run for branch", "branch", b.Name, "error", err)
			continue
		}

		// Delete branch if run doesn't exist or is in a terminal state
		if run == nil || run.Status == domain.RunStatusSuccess ||
			run.Status == domain.RunStatusFailed || run.Status == domain.RunStatusCancelled {
			if err := r.nessie.DeleteBranch(ctx, b.Name, b.Hash); err != nil {
				slog.Warn("reaper: failed to delete orphan branch", "branch", b.Name, "error", err)
				continue
			}
			count++
		}
	}
	return count
}

// purgeProcessedLZFiles deletes _processed/ files from landing zones with auto_purge enabled.
func (r *Reaper) purgeProcessedLZFiles(ctx context.Context, now time.Time) int {
	if r.zones == nil || r.storage == nil {
		return 0
	}

	zones, err := r.zones.ListZonesWithAutoPurge(ctx)
	if err != nil {
		slog.Error("reaper: failed to list auto-purge zones", "error", err)
		return 0
	}

	count := 0
	for _, z := range zones {
		maxAge := 30 // default 30 days
		if z.ProcessedMaxAgeDays != nil && *z.ProcessedMaxAgeDays > 0 {
			maxAge = *z.ProcessedMaxAgeDays
		}
		cutoff := now.Add(-time.Duration(maxAge) * 24 * time.Hour)

		prefix := z.Namespace + "/landing/" + z.Name + "/_processed/"
		files, err := r.storage.ListFiles(ctx, prefix)
		if err != nil {
			slog.Warn("reaper: failed to list processed files", "zone", z.Name, "error", err)
			continue
		}

		for _, f := range files {
			if f.Modified.Before(cutoff) {
				if err := r.storage.DeleteFile(ctx, f.Path); err != nil {
					slog.Warn("reaper: failed to delete processed file", "path", f.Path, "error", err)
					continue
				}
				count++
			}
		}
	}
	return count
}

// pruneAuditLog deletes audit entries older than the configured max age.
func (r *Reaper) pruneAuditLog(ctx context.Context, cfg domain.RetentionConfig, now time.Time) int {
	if r.audit == nil {
		return 0
	}

	cutoff := now.Add(-time.Duration(cfg.AuditLogMaxAgeDays) * 24 * time.Hour)
	count, err := r.audit.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		slog.Error("reaper: failed to prune audit log", "error", err)
		return 0
	}
	return count
}

// loadConfig loads the retention config from settings, falling back to defaults.
// Errors are logged so operators can diagnose misconfigured settings.
func (r *Reaper) loadConfig(ctx context.Context) domain.RetentionConfig {
	if r.settings == nil {
		return domain.DefaultRetentionConfig()
	}

	data, err := r.settings.GetSetting(ctx, "retention")
	if err != nil {
		slog.Warn("reaper: failed to load retention config, using defaults", "error", err)
		return domain.DefaultRetentionConfig()
	}

	var cfg domain.RetentionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Warn("reaper: failed to unmarshal retention config, using defaults", "error", err)
		return domain.DefaultRetentionConfig()
	}
	return cfg
}

// safeRun executes fn with panic recovery to isolate task failures.
func (r *Reaper) safeRun(name string, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("reaper: task panicked", "task", name, "panic", rec)
		}
	}()
	fn()
}
