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

// Build-time version information. These are set via -ldflags at build time:
//
//	go build -ldflags "-X api.Version=2.0.0 -X api.GitCommit=abc1234 -X api.BuildTime=2026-02-16T12:00:00Z"
//
// If not set, defaults are used.
var (
	Version   = "dev"     // Semantic version (e.g., "2.0.0")
	GitCommit = "unknown" // Git commit SHA
	BuildTime = "unknown" // ISO 8601 build timestamp
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
			Plugins: map[string]domain.PluginFeature{
				"auth":        {Enabled: false},
				"sharing":     {Enabled: false},
				"executor":    {Enabled: true, Type: "warmpool"},
				"audit":       {Enabled: false},
				"enforcement": {Enabled: false},
			},
		}
	}

	features.LandingZones = s.LandingZones != nil

	if s.LicenseInfo != nil {
		features.License = s.LicenseInfo
	}

	writeJSON(w, http.StatusOK, features)
}
