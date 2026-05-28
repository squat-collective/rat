package api

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/rat-data/rat/platform/internal/domain"
)

// readinessTimeout is the per-dependency timeout for readiness checks.
const readinessTimeout = 2 * time.Second

// Build-time version information. These are injected via -ldflags by the
// release pipeline (.github/workflows/release.yml → platform/Dockerfile
// ARG VERSION), so the git tag is the single source of truth — never
// hardcode a real version here. Local / unstamped builds report "dev".
//
//	go build -ldflags "-X github.com/rat-data/rat/platform/internal/api.Version=0.2.0-beta.1 ..."
var (
	Version   = "dev"     // Semantic version — injected at release build
	GitCommit = "unknown" // Git commit SHA — injected at release build
	BuildTime = "unknown" // ISO 8601 build timestamp — injected at release build
)

// HealthChecker verifies that a dependency is reachable and healthy.
// Implementations should be lightweight (e.g. Ping, SELECT 1, BucketExists).
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// CheckResult holds the outcome of a single dependency health check.
type CheckResult struct {
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // human-readable error when status is "error"
}

// ReadinessResponse is the structured JSON returned by GET /health/ready.
type ReadinessResponse struct {
	Status string                 `json:"status"` // "ready" or "not_ready"
	Checks map[string]CheckResult `json:"checks"`
}

// HandleHealthLive is a lightweight liveness probe — confirms the process is alive.
// Always returns 200. Used by orchestrators (Docker, Kubernetes) for liveness checks.
// Includes version and build information for operational visibility.
func (s *Server) HandleHealthLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "ok",
		"version":    Version,
		"git_commit": GitCommit,
		"build_time": BuildTime,
		"go_version": runtime.Version(),
	})
}

// HandleHealthReady checks all registered dependencies and returns 200 if all are healthy,
// or 503 if any critical dependency is down. Each dependency check runs with a 2s timeout.
func (s *Server) HandleHealthReady(w http.ResponseWriter, r *http.Request) {
	checkers := s.healthCheckers()

	// No dependencies configured — still ready (e.g. dev mode with no DB/S3).
	if len(checkers) == 0 {
		writeJSON(w, http.StatusOK, ReadinessResponse{
			Status: "ready",
			Checks: map[string]CheckResult{},
		})
		return
	}

	// Run all checks concurrently, each with its own timeout.
	type result struct {
		name string
		res  CheckResult
	}
	results := make([]result, len(checkers))

	var wg sync.WaitGroup
	i := 0
	for name, checker := range checkers {
		wg.Add(1)
		go func(idx int, n string, c HealthChecker) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
			defer cancel()

			if err := c.HealthCheck(ctx); err != nil {
				results[idx] = result{name: n, res: CheckResult{Status: "error", Error: err.Error()}}
			} else {
				results[idx] = result{name: n, res: CheckResult{Status: "ok"}}
			}
		}(i, name, checker)
		i++
	}
	wg.Wait()

	// Build response.
	checks := make(map[string]CheckResult, len(results))
	allOK := true
	for _, r := range results {
		checks[r.name] = r.res
		if r.res.Status != "ok" {
			allOK = false
		}
	}

	resp := ReadinessResponse{Checks: checks}
	if allOK {
		resp.Status = "ready"
		writeJSON(w, http.StatusOK, resp)
	} else {
		resp.Status = "not_ready"
		writeJSON(w, http.StatusServiceUnavailable, resp)
	}
}

// HandleHealth is the backward-compatible health endpoint.
// Aliases to the liveness probe (always 200).
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	s.HandleHealthLive(w, r)
}

// healthCheckers returns the map of dependency name → checker based on
// which dependencies are configured on the Server. Only non-nil checkers
// are included, so dev/test servers with no dependencies return an empty map.
func (s *Server) healthCheckers() map[string]HealthChecker {
	checkers := make(map[string]HealthChecker)
	if s.DBHealth != nil {
		checkers["postgres"] = s.DBHealth
	}
	if s.S3Health != nil {
		checkers["s3"] = s.S3Health
	}
	if s.RunnerHealth != nil {
		checkers["runner"] = s.RunnerHealth
	}
	if s.QueryHealth != nil {
		checkers["query"] = s.QueryHealth
	}
	return checkers
}

// HandleMetrics returns basic application metrics in Prometheus text exposition format.
// This is a lightweight implementation suitable for scraping by Prometheus.
// For production use, consider integrating prometheus/client_golang for full histogram support.
func (s *Server) HandleMetrics(w http.ResponseWriter, _ *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Process metrics
	fmt.Fprintf(w, "# HELP ratd_info Build information about ratd.\n")
	fmt.Fprintf(w, "# TYPE ratd_info gauge\n")
	fmt.Fprintf(w, "ratd_info{version=%q,git_commit=%q,go_version=%q} 1\n", Version, GitCommit, runtime.Version())

	// Go runtime metrics
	fmt.Fprintf(w, "# HELP ratd_goroutines Number of goroutines.\n")
	fmt.Fprintf(w, "# TYPE ratd_goroutines gauge\n")
	fmt.Fprintf(w, "ratd_goroutines %d\n", runtime.NumGoroutine())

	fmt.Fprintf(w, "# HELP ratd_memory_alloc_bytes Current memory allocation in bytes.\n")
	fmt.Fprintf(w, "# TYPE ratd_memory_alloc_bytes gauge\n")
	fmt.Fprintf(w, "ratd_memory_alloc_bytes %d\n", memStats.Alloc)

	fmt.Fprintf(w, "# HELP ratd_memory_sys_bytes Total memory obtained from the OS in bytes.\n")
	fmt.Fprintf(w, "# TYPE ratd_memory_sys_bytes gauge\n")
	fmt.Fprintf(w, "ratd_memory_sys_bytes %d\n", memStats.Sys)

	fmt.Fprintf(w, "# HELP ratd_gc_completed_total Total number of completed GC cycles.\n")
	fmt.Fprintf(w, "# TYPE ratd_gc_completed_total counter\n")
	fmt.Fprintf(w, "ratd_gc_completed_total %d\n", memStats.NumGC)

	// SSE connection metrics
	if s.SSELimiter != nil {
		fmt.Fprintf(w, "# HELP ratd_sse_connections_active Current number of active SSE connections.\n")
		fmt.Fprintf(w, "# TYPE ratd_sse_connections_active gauge\n")
		fmt.Fprintf(w, "ratd_sse_connections_active %d\n", s.SSELimiter.GlobalCount())
	}

	// Postgres pool saturation — main pool.
	// total = pool size (max connections), acquired = currently in-use.
	// Saturation = acquired / total → 1.0 means every connection is busy;
	// next acquire blocks until one is freed. Use the difference between
	// the two gauges in PromQL to alert on sustained near-saturation.
	if s.DBPoolStats != nil {
		total, acquired := s.DBPoolStats()
		fmt.Fprintf(w, "# HELP ratd_postgres_pool_total Total connections configured for the main Postgres pool.\n")
		fmt.Fprintf(w, "# TYPE ratd_postgres_pool_total gauge\n")
		fmt.Fprintf(w, "ratd_postgres_pool_total %d\n", total)

		fmt.Fprintf(w, "# HELP ratd_postgres_pool_acquired Currently-acquired connections from the main Postgres pool.\n")
		fmt.Fprintf(w, "# TYPE ratd_postgres_pool_acquired gauge\n")
		fmt.Fprintf(w, "ratd_postgres_pool_acquired %d\n", acquired)
	}

	// Postgres pool saturation — dedicated heartbeat pool (when enabled).
	// This pool exists so a saturated main pool can't starve the leader
	// heartbeat ping (see main.go: RAT_HEARTBEAT_POOL_ENABLED). If this
	// dedicated pool itself looks saturated something is very wrong — the
	// heartbeat path should at most need one connection at a time.
	if s.HeartbeatPoolStats != nil {
		total, acquired := s.HeartbeatPoolStats()
		fmt.Fprintf(w, "# HELP ratd_postgres_heartbeat_pool_total Total connections configured for the dedicated heartbeat pool.\n")
		fmt.Fprintf(w, "# TYPE ratd_postgres_heartbeat_pool_total gauge\n")
		fmt.Fprintf(w, "ratd_postgres_heartbeat_pool_total %d\n", total)

		fmt.Fprintf(w, "# HELP ratd_postgres_heartbeat_pool_acquired Currently-acquired connections from the dedicated heartbeat pool.\n")
		fmt.Fprintf(w, "# TYPE ratd_postgres_heartbeat_pool_acquired gauge\n")
		fmt.Fprintf(w, "ratd_postgres_heartbeat_pool_acquired %d\n", acquired)
	}

	// Plugin fleet health.
	// total counts every registered plugin (any status); healthy counts only
	// those currently in PluginStatusEnabled. The delta is "registered but
	// not currently passing the 30s health-loop probe".
	if s.PluginHealthStats != nil {
		total, healthy := s.PluginHealthStats()
		fmt.Fprintf(w, "# HELP ratd_plugins_total Total number of registered plugins (any status).\n")
		fmt.Fprintf(w, "# TYPE ratd_plugins_total gauge\n")
		fmt.Fprintf(w, "ratd_plugins_total %d\n", total)

		fmt.Fprintf(w, "# HELP ratd_plugins_healthy Plugins currently in the enabled status (passing health-loop probes).\n")
		fmt.Fprintf(w, "# TYPE ratd_plugins_healthy gauge\n")
		fmt.Fprintf(w, "ratd_plugins_healthy %d\n", healthy)
	}

	// Scheduler tick observability.
	// last_tick_duration_seconds growth signals the planning phase (store
	// reads) is slowing down — usually a Postgres or schedule-count issue.
	// last_tick_dispatched_total spikes show bursty load on the runner.
	if s.SchedulerMetrics != nil {
		lastTickSeconds, dispatched := s.SchedulerMetrics()
		fmt.Fprintf(w, "# HELP ratd_scheduler_last_tick_duration_seconds Duration of the most recent scheduler tick.\n")
		fmt.Fprintf(w, "# TYPE ratd_scheduler_last_tick_duration_seconds gauge\n")
		fmt.Fprintf(w, "ratd_scheduler_last_tick_duration_seconds %g\n", lastTickSeconds)

		fmt.Fprintf(w, "# HELP ratd_scheduler_last_tick_dispatched_total Schedules dispatched in the most recent scheduler tick.\n")
		fmt.Fprintf(w, "# TYPE ratd_scheduler_last_tick_dispatched_total gauge\n")
		fmt.Fprintf(w, "ratd_scheduler_last_tick_dispatched_total %d\n", dispatched)
	}
}

// HandleFeatures returns the active platform capabilities.
// The portal uses this to show/hide UI elements based on active plugins.
// When a PluginRegistry is available, features are dynamic. Otherwise, hardcoded community defaults.
func (s *Server) HandleFeatures(w http.ResponseWriter, _ *http.Request) {
	var features domain.Features

	if s.Plugins != nil {
		features = s.Plugins.Features()
	} else {
		// Backward compatibility: no plugin registry → community defaults.
		features = domain.Features{
			Edition:    "community",
			Namespaces: false,
			MultiUser:  false,
			Plugins:    map[string]domain.PluginFeature{},
		}
	}

	features.LandingZones = s.LandingZones != nil

	if s.LicenseInfo != nil {
		features.License = s.LicenseInfo
	}

	writeJSON(w, http.StatusOK, features)
}
