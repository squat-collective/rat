// ratd is the RAT platform server.
// It serves the REST API, manages plugins, and orchestrates pipeline execution.
//
// P10-33 TODO: Refactor main() into a builder/options pattern:
//   - Extract server construction into a ServerBuilder with functional options.
//   - Move store wiring, executor setup, and plugin loading into builder methods.
//   - main() should only parse config, build the server, and start it.
//
// P10-39 TODO: Replace manual shutdown hooks with golang.org/x/sync/errgroup:
//   - Create an errgroup.Group for all background goroutines (HTTP server,
//     scheduler, trigger evaluator, reaper, event bus).
//   - Use g.Go() for each goroutine and g.Wait() for coordinated shutdown.
//   - This eliminates the manual stopLeader/stopScheduler/stopReaper/etc. closures
//     and provides automatic error propagation + coordinated cancellation.
//
// Example (P10-39):
//
//	g, ctx := errgroup.WithContext(ctx)
//	g.Go(func() error { return httpServer.ListenAndServe() })
//	g.Go(func() error { scheduler.Run(ctx); return nil })
//	g.Go(func() error { reaper.Run(ctx); return nil })
//	<-sigCh; cancel(); return g.Wait()
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/auth"
	"github.com/rat-data/rat/platform/internal/cache"
	"github.com/rat-data/rat/platform/internal/config"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/executor"
	"github.com/rat-data/rat/platform/internal/leader"
	"github.com/rat-data/rat/platform/internal/license"
	"github.com/rat-data/rat/platform/internal/plugins"
	"github.com/rat-data/rat/platform/internal/postgres"
	"github.com/rat-data/rat/platform/internal/query"
	"github.com/rat-data/rat/platform/internal/reaper"
	"github.com/rat-data/rat/platform/internal/scheduler"
	"github.com/rat-data/rat/platform/internal/storage"
	"github.com/rat-data/rat/platform/internal/transport"
	"github.com/rat-data/rat/platform/internal/trigger"
)

// validateEnv checks that critical environment variables have valid values.
// Returns a slice of validation errors (empty if all valid).
func validateEnv() []string {
	var errs []string

	// Validate listen address format (host:port).
	if addr := os.Getenv("RAT_LISTEN_ADDR"); addr != "" {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			errs = append(errs, fmt.Sprintf("RAT_LISTEN_ADDR=%q: must be host:port (%v)", addr, err))
		}
	}

	// Validate PORT is numeric.
	if port := os.Getenv("PORT"); port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			errs = append(errs, fmt.Sprintf("PORT=%q: must be a valid port number", port))
		}
	}

	// Validate DATABASE_URL is a parseable postgres URL.
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		if _, err := url.Parse(dbURL); err != nil {
			errs = append(errs, fmt.Sprintf("DATABASE_URL: invalid URL (%v)", err))
		}
	}

	// Validate duration-typed env vars.
	for _, name := range []string{"S3_METADATA_TIMEOUT", "S3_DATA_TIMEOUT"} {
		if v := os.Getenv(name); v != "" {
			if _, err := time.ParseDuration(v); err != nil {
				errs = append(errs, fmt.Sprintf("%s=%q: must be a valid Go duration (e.g. 10s, 2m) (%v)", name, v, err))
			}
		}
	}

	// Validate URL-typed env vars.
	for _, name := range []string{"S3_ENDPOINT", "NESSIE_URL"} {
		if v := os.Getenv(name); v != "" {
			// S3_ENDPOINT may be host:port without scheme; allow that.
			if name == "S3_ENDPOINT" {
				if _, _, err := net.SplitHostPort(v); err != nil {
					if _, err := url.Parse("http://" + v); err != nil {
						errs = append(errs, fmt.Sprintf("%s=%q: must be a valid endpoint", name, v))
					}
				}
			} else {
				if _, err := url.ParseRequestURI(v); err != nil {
					errs = append(errs, fmt.Sprintf("%s=%q: must be a valid URL (%v)", name, v, err))
				}
			}
		}
	}

	// Validate gRPC address env vars (URL or host:port).
	for _, name := range []string{"RUNNER_ADDR", "RATQ_ADDR"} {
		if v := os.Getenv(name); v != "" {
			// RUNNER_ADDR may be comma-separated for round-robin.
			for _, addr := range strings.Split(v, ",") {
				addr = strings.TrimSpace(addr)
				if addr == "" {
					continue
				}
				// Accept full URLs (http://host:port) or raw host:port.
				if _, err := url.ParseRequestURI(addr); err != nil {
					if _, _, err2 := net.SplitHostPort(addr); err2 != nil {
						errs = append(errs, fmt.Sprintf("%s=%q: must be a URL or host:port (%v)", name, addr, err2))
					}
				}
			}
		}
	}

	return errs
}

// warnDefaultCredentials logs security warnings when S3 or Postgres credentials
// appear to be well-known defaults (e.g., minioadmin/minioadmin, rat/rat).
// These are safe for local development but dangerous in production deployments.
func warnDefaultCredentials() {
	// S3/MinIO default credentials
	s3Access := os.Getenv("S3_ACCESS_KEY")
	s3Secret := os.Getenv("S3_SECRET_KEY")
	if s3Access == "minioadmin" || s3Secret == "minioadmin" {
		slog.Warn("S3 credentials are set to default values (minioadmin) — change these for production deployments")
	}

	// Postgres default credentials embedded in DATABASE_URL
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		if u, err := url.Parse(dbURL); err == nil && u.User != nil {
			user := u.User.Username()
			pass, _ := u.User.Password()
			if (user == "rat" && pass == "rat") || (user == "postgres" && pass == "postgres") {
				slog.Warn("database credentials appear to be defaults — change these for production deployments",
					"user", user)
			}
		}
	}
}

func main() {
	// Built-in healthcheck for scratch containers (no wget/curl available).
	// Usage: /ratd healthcheck
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		resp, err := http.Get("http://localhost:8080/health")
		if err != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// P10-40: Use context-aware slog handler to automatically include request_id
	// in all log records when a request context is available.
	baseHandler := slog.NewJSONHandler(os.Stdout, nil)
	logger := slog.New(api.NewContextHandler(baseHandler))
	slog.SetDefault(logger)

	// Validate critical environment variables before wiring anything.
	if errs := validateEnv(); len(errs) > 0 {
		for _, e := range errs {
			slog.Error("invalid environment variable", "error", e)
		}
		os.Exit(1)
	}

	srv := &api.Server{}

	// Initialize in-memory caches for slow-changing data.
	// These reduce Postgres load for namespace lists and pipeline metadata
	// that are fetched on almost every portal page load but rarely change.
	srv.NamespaceCache = cache.New[string, []domain.Namespace](cache.Options{
		TTL:        30 * time.Second,
		MaxEntries: 10, // namespace list is a single "all" entry
	})
	srv.PipelineCache = cache.New[string, *domain.Pipeline](cache.Options{
		TTL:        30 * time.Second,
		MaxEntries: 500, // reasonable upper bound for pipeline count
	})
	slog.Info("in-memory caches initialized", "namespace_ttl", "30s", "pipeline_ttl", "30s")

	// Load plugin config: RAT_CONFIG env > ./rat.yaml > community defaults.
	configPath := config.ResolvePath()
	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "path", configPath, "error", err)
		os.Exit(1)
	}
	if configPath != "" {
		slog.Info("config loaded", "path", configPath, "edition", cfg.Edition)
	}

	// Build gRPC HTTP client (TLS or h2c depending on GRPC_TLS_CA env var).
	tlsCfg := transport.TLSConfigFromEnv()
	grpcClient, err := transport.NewGRPCClient(tlsCfg)
	if err != nil {
		slog.Error("failed to create gRPC client", "error", err)
		os.Exit(1)
	}
	if tlsCfg.CACertFile != "" {
		slog.Info("gRPC TLS enabled", "ca", tlsCfg.CACertFile)
	}

	// Load plugins (connects to plugin containers, health-checks).
	ctx := context.Background()
	registry, err := plugins.Load(ctx, cfg, grpcClient)
	if err != nil {
		slog.Error("failed to load plugins", "error", err)
		os.Exit(1)
	}
	srv.Plugins = registry

	// Auth middleware: plugin auth (Pro) takes priority over API key (Community).
	// If an auth plugin is loaded, use it. Otherwise, check RAT_API_KEY.
	if registry.AuthEnabled() {
		srv.Auth = registry.AuthMiddleware()
	} else if apiKey := os.Getenv("RAT_API_KEY"); apiKey != "" {
		srv.Auth = auth.APIKey(apiKey)
		slog.Info("API key authentication enabled")
	} else {
		srv.Auth = auth.Noop()
	}

	// Decode license key for display (no validation — enforcement is in plugins).
	if licenseKey := os.Getenv("RAT_LICENSE_KEY"); licenseKey != "" {
		info, err := license.Decode(licenseKey)
		if err != nil {
			slog.Warn("failed to decode license key", "error", err)
		} else {
			li := &domain.LicenseInfo{
				Valid:     info.Valid,
				Tier:      info.Tier,
				OrgID:     info.OrgID,
				Plugins:   info.Plugins,
				SeatLimit: info.SeatLimit,
				Error:     info.Error,
			}
			if info.ExpiresAt != nil {
				exp := info.ExpiresAt.Format(time.RFC3339)
				li.ExpiresAt = &exp
			}
			srv.LicenseInfo = li
			slog.Info("license key decoded", "tier", info.Tier, "org_id", info.OrgID, "valid", info.Valid)
		}
	}

	// Shutdown hooks — populated below, called in order during graceful shutdown.
	var (
		stopLeader    func()
		stopScheduler func()
		stopEvaluator func()
		stopReaper    func()
		stopExecutor  func()
		stopEventBus  func()
		closePool     func()
	)

	// Event bus — populated below when DATABASE_URL is set.
	// Enables instant event-driven reactions via Postgres LISTEN/NOTIFY.
	var eventBus *postgres.PgEventBus

	// SCHEDULER_ENABLED controls whether this replica can run background workers
	// (scheduler, trigger evaluator, reaper). Default: true.
	// Set to "false" to run a pure API-only replica.
	schedulerEnabled := os.Getenv("SCHEDULER_ENABLED") != "false"

	// Wire Postgres stores when DATABASE_URL is set.
	// If not set, stores are nil (useful for development/testing without a DB).
	var pool *pgxpool.Pool
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		ctx := context.Background()

		var poolErr error
		pool, poolErr = postgres.NewPool(ctx, dbURL)
		if poolErr != nil {
			slog.Error("failed to connect to database", "error", poolErr)
			os.Exit(1)
		}
		closePool = func() { pool.Close() }

		if err := postgres.Migrate(ctx, pool); err != nil {
			slog.Error("failed to run migrations", "error", err)
			os.Exit(1)
		}

		// Start event bus (Postgres LISTEN/NOTIFY) for instant event delivery.
		eventBus = postgres.NewPgEventBus(pool)
		if err := eventBus.Start(ctx); err != nil {
			slog.Warn("event bus failed to start, continuing without instant events", "error", err)
			eventBus = nil
		} else {
			stopEventBus = func() { eventBus.Stop() }
		}

		pipelineStore := postgres.NewPipelineStore(pool)
		runStore := postgres.NewRunStore(pool)

		// Wire event bus into stores for automatic NOTIFY on state changes.
		if eventBus != nil {
			pipelineStore.EventBus = eventBus
			runStore.EventBus = eventBus
		}

		srv.Pipelines = pipelineStore
		srv.Versions = postgres.NewVersionStore(pool)
		srv.Publisher = postgres.NewPipelinePublisher(pool)
		srv.Runs = runStore
		srv.Namespaces = postgres.NewNamespaceStore(pool)
		srv.Schedules = postgres.NewScheduleStore(pool)
		srv.LandingZones = postgres.NewLandingZoneStore(pool)
		srv.TableMetadata = postgres.NewTableMetadataStore(pool)
		srv.Triggers = postgres.NewTriggerStore(pool)
		srv.Audit = postgres.NewAuditStore(pool)
		srv.Settings = postgres.NewSettingsStore(pool)

		srv.DBHealth = postgres.NewHealthChecker(pool)
		slog.Info("postgres stores initialized")

		// Wire enforcement authorizer after Postgres stores are available.
		if registry.EnforcementEnabled() {
			srv.Authorizer = plugins.NewPluginAuthorizer(registry, srv.Pipelines)
			slog.Info("enforcement authorizer initialized (plugin)")
		}
	} else {
		slog.Warn("DATABASE_URL not set, running without persistence")
	}

	// Wire S3 storage when S3_ENDPOINT is set.
	if s3Endpoint := os.Getenv("S3_ENDPOINT"); s3Endpoint != "" {
		s3Bucket := os.Getenv("S3_BUCKET")
		if s3Bucket == "" {
			s3Bucket = "rat"
		}

		s3Cfg := storage.S3Config{
			Endpoint:  s3Endpoint,
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
			Bucket:    s3Bucket,
			UseSSL:    os.Getenv("S3_USE_SSL") == "true",
		}

		// Optional timeout overrides (e.g. S3_METADATA_TIMEOUT=15s, S3_DATA_TIMEOUT=120s).
		if v := os.Getenv("S3_METADATA_TIMEOUT"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				slog.Error("invalid S3_METADATA_TIMEOUT", "value", v, "error", err)
				os.Exit(1)
			}
			s3Cfg.MetadataTimeout = d
		}
		if v := os.Getenv("S3_DATA_TIMEOUT"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				slog.Error("invalid S3_DATA_TIMEOUT", "value", v, "error", err)
				os.Exit(1)
			}
			s3Cfg.DataTimeout = d
		}

		ctx := context.Background()
		s3Store, err := storage.NewS3StoreFromConfig(ctx, s3Cfg)
		if err != nil {
			slog.Error("failed to connect to S3", "error", err)
			os.Exit(1)
		}
		srv.Storage = s3Store
		srv.S3Health = storage.NewHealthChecker(s3Store)
		srv.Quality = storage.NewS3QualityStore(s3Store)

		// Log effective timeouts (defaults if not explicitly configured).
		metaTimeout := s3Cfg.MetadataTimeout
		if metaTimeout == 0 {
			metaTimeout = storage.DefaultMetadataTimeout
		}
		dataTimeout := s3Cfg.DataTimeout
		if dataTimeout == 0 {
			dataTimeout = storage.DefaultDataTimeout
		}
		slog.Info("s3 storage initialized",
			"endpoint", s3Endpoint,
			"bucket", s3Bucket,
			"metadata_timeout", metaTimeout,
			"data_timeout", dataTimeout,
		)
	} else {
		slog.Warn("S3_ENDPOINT not set, running without storage")
	}

	// Wire cloud provider if cloud plugin is available.
	if registry.CloudEnabled() {
		srv.Cloud = registry
		slog.Info("cloud provider initialized (plugin)")
	}

	// Wire executor: plugin executor (Pro) takes priority over WarmPoolExecutor (Community).
	if registry.ExecutorEnabled() {
		addr := registry.GetExecutorAddr()
		exec := executor.NewPluginExecutor(addr, srv.Runs, grpcClient)
		exec.OnRunComplete = func(ctx context.Context, run *domain.Run, status domain.RunStatus) {
			if status != domain.RunStatusSuccess || srv.Triggers == nil {
				return
			}
			srv.EvaluatePipelineSuccessTriggers(ctx, run)
		}
		srv.Executor = exec
		srv.RunnerHealth = transport.NewTCPHealthChecker(addr, "runner")
		exec.Start(ctx)
		stopExecutor = func() { exec.Stop() }
		slog.Info("executor initialized (plugin)", "addr", addr)
	} else if runnerAddr := os.Getenv("RUNNER_ADDR"); runnerAddr != "" {
		addrs := executor.ParseRunnerAddrs(runnerAddr)
		srv.RunnerHealth = transport.NewTCPHealthChecker(addrs[0], "runner")
		onComplete := func(ctx context.Context, run *domain.Run, status domain.RunStatus) {
			if status != domain.RunStatusSuccess || srv.Triggers == nil {
				return
			}
			srv.EvaluatePipelineSuccessTriggers(ctx, run)
		}

		if len(addrs) > 1 {
			// Multiple runner replicas — use round-robin dispatch with
			// RESOURCE_EXHAUSTED failover across all runners.
			rr := executor.NewRoundRobinExecutor(addrs, srv.Runs, grpcClient)
			rr.SetLandingZones(srv.LandingZones)
			rr.SetOnRunComplete(onComplete)
			srv.Executor = rr
			rr.Start(ctx)
			stopExecutor = func() { rr.Stop() }
			slog.Info("executor initialized (round-robin)", "runners", len(addrs), "runner_addrs", strings.Join(addrs, ","))
		} else {
			// Single runner — use direct WarmPoolExecutor (original behavior).
			exec := executor.NewWarmPoolExecutor(addrs[0], srv.Runs, grpcClient)
			exec.LandingZones = srv.LandingZones
			exec.OnRunComplete = onComplete
			srv.Executor = exec
			exec.Start(ctx)
			stopExecutor = func() { exec.Stop() }
			slog.Info("executor initialized (warmpool)", "runner_addr", addrs[0])
		}
	}

	// Wire query service when RATQ_ADDR is set.
	if ratqAddr := os.Getenv("RATQ_ADDR"); ratqAddr != "" {
		srv.Query = query.NewClient(ratqAddr, grpcClient)
		srv.QueryHealth = transport.NewTCPHealthChecker(ratqAddr, "query")
		slog.Info("query service initialized", "ratq_addr", ratqAddr)
	}

	// startBackgroundWorkers launches scheduler, trigger evaluator, and reaper.
	// Called directly when no leader election is needed, or by the leader
	// elector when this replica wins the advisory lock.
	startBackgroundWorkers := func(ctx context.Context) func() {
		// Wire scheduler when executor is available.
		if srv.Executor != nil {
			sched := scheduler.New(srv.Schedules, srv.Pipelines, srv.Runs, srv.Executor, 30*time.Second)
			sched.Start(ctx)
			stopScheduler = func() { sched.Stop() }
			slog.Info("scheduler started")
		}

		// Wire trigger evaluator for cron + cron_dependency triggers.
		if srv.Executor != nil && srv.Triggers != nil {
			eval := trigger.NewEvaluator(srv.Triggers, srv.Pipelines, srv.Runs, srv.Executor, 30*time.Second)

			// Subscribe to run_completed events for instant cron_dependency evaluation.
			if eventBus != nil {
				ch, cancelSub := eventBus.Subscribe(postgres.ChannelRunCompleted)
				eval.EventCh = ch
				eval.SetEventCancel(cancelSub)
				slog.Info("trigger evaluator subscribed to run_completed events")
			}

			eval.Start(ctx)
			stopEvaluator = func() { eval.Stop() }
			slog.Info("trigger evaluator started")
		}

		// Wire reaper for data retention cleanup.
		if srv.Settings != nil {
			var nessieClient reaper.NessieClient
			if nessieURL := os.Getenv("NESSIE_URL"); nessieURL != "" {
				nessieClient = reaper.NewHTTPNessieClient(nessieURL)
			}
			reap := reaper.New(srv.Settings, srv.Runs, srv.Pipelines, srv.LandingZones, srv.Storage, srv.Audit, nessieClient)
			reap.Start(ctx)
			srv.Reaper = reap
			stopReaper = func() { reap.Stop() }
			slog.Info("reaper started")
		}

		return func() {
			if stopScheduler != nil {
				stopScheduler()
				stopScheduler = nil
				slog.Info("scheduler stopped")
			}
			if stopEvaluator != nil {
				stopEvaluator()
				stopEvaluator = nil
				slog.Info("trigger evaluator stopped")
			}
			if stopReaper != nil {
				stopReaper()
				stopReaper = nil
				slog.Info("reaper stopped")
			}
		}
	}

	// Background workers: scheduler, trigger evaluator, reaper.
	// These should only run on ONE replica to avoid duplicate pipeline runs.
	switch {
	case !schedulerEnabled:
		slog.Info("background workers disabled (SCHEDULER_ENABLED=false)")
	case pool != nil:
		// Leader election via Postgres advisory lock. Only the replica that
		// acquires the lock starts background workers. If the leader dies,
		// Postgres releases the lock and another replica takes over.
		tryLock := func(ctx context.Context) (bool, error) {
			var acquired bool
			err := pool.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", leader.AdvisoryLockID).Scan(&acquired)
			return acquired, err
		}
		elector := leader.New(tryLock, leader.RetryInterval, startBackgroundWorkers)
		elector.Start(ctx)
		stopLeader = func() { elector.Stop() }
		slog.Info("leader election started (advisory lock)")
	default:
		// No database — start workers directly (single-instance mode).
		stopFn := startBackgroundWorkers(ctx)
		stopLeader = stopFn
	}

	// Warn if S3 or Postgres credentials are still set to well-known defaults.
	// These are fine for local development but dangerous in production.
	warnDefaultCredentials()

	// Configurable CORS origins (comma-separated).
	if corsEnv := os.Getenv("CORS_ORIGINS"); corsEnv != "" {
		srv.CORSOrigins = strings.Split(corsEnv, ",")
	}

	// Per-IP rate limiting (disable with RATE_LIMIT=0).
	if rl := os.Getenv("RATE_LIMIT"); rl != "0" {
		cfg := api.DefaultRateLimitConfig()
		srv.RateLimit = &cfg
		slog.Info("rate limiting enabled", "rps", cfg.RequestsPerSecond, "burst", cfg.Burst)
	}

	router := api.NewRouter(srv)

	// Listen address: RAT_LISTEN_ADDR > PORT (legacy) > default 127.0.0.1:8080.
	// Default binds to localhost only — users must explicitly set 0.0.0.0:8080
	// for network access, which triggers a security warning if no API key is set.
	addr := "127.0.0.1:8080"
	if listenAddr := os.Getenv("RAT_LISTEN_ADDR"); listenAddr != "" {
		addr = listenAddr
	} else if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	// Warn if listening on all interfaces without authentication.
	if strings.HasPrefix(addr, "0.0.0.0") && os.Getenv("RAT_API_KEY") == "" && !registry.AuthEnabled() {
		slog.Warn("listening on 0.0.0.0 without RAT_API_KEY — API is unauthenticated and accessible from the network")
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadTimeout:       60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	}

	// Start HTTP(S) server in a goroutine.
	tlsCertFile := os.Getenv("TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("TLS_KEY_FILE")

	errCh := make(chan error, 1)
	if tlsCertFile != "" && tlsKeyFile != "" {
		go func() {
			errCh <- httpServer.ListenAndServeTLS(tlsCertFile, tlsKeyFile)
		}()
		slog.Info("starting ratd (HTTPS)", "addr", addr, "version", "2.0.0-dev")
	} else {
		go func() {
			errCh <- httpServer.ListenAndServe()
		}()
		slog.Info("starting ratd", "addr", addr, "version", "2.0.0-dev")
	}

	// Wait for shutdown signal or server error.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}

	// Graceful shutdown: drain HTTP connections (15s timeout).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown error", "error", err)
	}

	// Ordered cleanup: leader (stops scheduler/evaluator/reaper) → executor → event bus → database pool.
	if stopLeader != nil {
		stopLeader()
		slog.Info("leader elector stopped")
	}
	if stopExecutor != nil {
		stopExecutor()
		slog.Info("executor stopped")
	}
	if stopEventBus != nil {
		stopEventBus()
		slog.Info("event bus stopped")
	}
	if srv.RateLimiterStop != nil {
		srv.RateLimiterStop()
		slog.Info("rate limiter stopped")
	}
	if srv.WebhookRateLimiterStop != nil {
		srv.WebhookRateLimiterStop()
		slog.Info("webhook rate limiter stopped")
	}
	if closePool != nil {
		closePool()
		slog.Info("database pool closed")
	}

	slog.Info("ratd shutdown complete")
}
