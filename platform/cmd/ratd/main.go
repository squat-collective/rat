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

	// Validate internal listen address format (host:port).
	if addr := os.Getenv("INTERNAL_LISTEN_ADDR"); addr != "" {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			errs = append(errs, fmt.Sprintf("INTERNAL_LISTEN_ADDR=%q: must be host:port (%v)", addr, err))
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

	// Load plugins via the new open Manager.
	// 1. Create Manager with an empty registry.
	// 2. If Postgres is available later, create PluginStore and load from catalog.
	// 3. For backward compat, register any plugins declared in rat.yaml config.
	ctx := context.Background()
	mgr := plugins.NewManager(nil, cfg.Edition, grpcClient) // catalog set after Postgres init
	registry := mgr.Registry()
	srv.Plugins = registry
	srv.PluginRegistry = registry

	// Register any plugins declared in rat.yaml config (backward compat).
	// These are registered immediately via health-check + describe.
	for name, pluginCfg := range cfg.Plugins {
		if err := mgr.Register(ctx, name, pluginCfg.Addr); err != nil {
			slog.Warn("config plugin registration failed, disabled", "name", name, "addr", pluginCfg.Addr, "error", err)
		}
	}

	// Auth middleware: plugin auth (Pro) takes priority over API key (Community).
	if registry.AuthEnabled() {
		srv.Auth = registry.AuthMiddleware()
	} else if apiKey := os.Getenv("RAT_API_KEY"); apiKey != "" {
		srv.Auth = auth.APIKey(apiKey)
		slog.Info("API key authentication enabled")
	} else {
		srv.Auth = auth.Noop()
	}

	// Runtime re-wiring callbacks — fired when plugins register/unregister.
	mgr.OnAuthChanged = func(reg *plugins.Registry) {
		if reg.AuthEnabled() {
			srv.Auth = reg.AuthMiddleware()
			slog.Info("auth middleware re-wired (plugin change)")
		} else if apiKey := os.Getenv("RAT_API_KEY"); apiKey != "" {
			srv.Auth = auth.APIKey(apiKey)
		} else {
			srv.Auth = auth.Noop()
		}
	}
	mgr.OnEnforcementChanged = func(reg *plugins.Registry) {
		if reg.EnforcementEnabled() && srv.Pipelines != nil {
			srv.Authorizer = plugins.NewPluginAuthorizer(reg, srv.Pipelines)
			slog.Info("enforcement authorizer re-wired (plugin change)")
		}
	}
	mgr.OnCloudChanged = func(reg *plugins.Registry) {
		if reg.CloudEnabled() {
			srv.Cloud = reg
			slog.Info("cloud provider re-wired (plugin change)")
		} else {
			srv.Cloud = nil
			slog.Info("cloud provider unregistered (no plugin with capability \"cloud\")")
		}
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
		stopLeader         func()
		stopScheduler      func()
		stopEvaluator      func()
		stopReaper         func()
		stopExecutor       func()
		stopEventBus       func()
		stopHealthLoop     func()
		stopDispatcher     func()
		closePool          func()
		closeHeartbeatPool func()
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
	var heartbeatPool *pgxpool.Pool
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		ctx := context.Background()

		var poolErr error
		pool, poolErr = postgres.NewPool(ctx, dbURL)
		if poolErr != nil {
			slog.Error("failed to connect to database", "error", poolErr)
			os.Exit(1)
		}
		closePool = func() { pool.Close() }

		// Dedicated single-connection pool for the leader heartbeat ping.
		// If RAT_HEARTBEAT_POOL_ENABLED=false, fall back to the shared pool
		// (saves one Postgres connection in tiny single-instance setups).
		//
		// Resilience: retry with exponential backoff (1s, 2s, 4s, 8s) before
		// giving up. On terminal failure, log a WARN and leave heartbeatPool
		// nil — the downstream leader wiring (see pingPool fallback below)
		// already handles nil by reusing the shared pool. We trade the
		// pool-starvation guard for boot survivability so a transient
		// Postgres slowness (Kubernetes parallel start, slow host) doesn't
		// crash-loop ratd.
		if os.Getenv("RAT_HEARTBEAT_POOL_ENABLED") != "false" {
			var hbPool *pgxpool.Pool
			var hbErr error
			backoff := time.Second
			for attempt := 1; attempt <= 5; attempt++ {
				hbPool, hbErr = postgres.NewHeartbeatPool(ctx, dbURL)
				if hbErr == nil {
					break
				}
				slog.Warn("heartbeat pool connect attempt failed",
					"attempt", attempt, "error", hbErr, "next_retry_in", backoff)
				if attempt < 5 {
					time.Sleep(backoff)
					backoff *= 2
				}
			}
			if hbErr != nil {
				slog.Warn("heartbeat pool unavailable after retries — falling back to shared pool; "+
					"pool-starvation guard disabled", "error", hbErr)
				// heartbeatPool stays nil; downstream code already handles this.
			} else {
				heartbeatPool = hbPool
				closeHeartbeatPool = func() { heartbeatPool.Close() }
			}
		} else {
			slog.Info("heartbeat pool disabled — falling back to shared pool",
				"env", "RAT_HEARTBEAT_POOL_ENABLED=false")
		}

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

		// Wire event bus into stores and server for automatic NOTIFY on state changes.
		if eventBus != nil {
			pipelineStore.EventBus = eventBus
			runStore.EventBus = eventBus
			srv.EventBus = eventBus
		}

		srv.Pipelines = pipelineStore
		srv.Versions = postgres.NewVersionStore(pool)
		srv.Publisher = postgres.NewPipelinePublisher(pool)
		srv.TxRunner = postgres.NewTxRunner(pool)
		srv.Runs = runStore
		srv.Namespaces = postgres.NewNamespaceStore(pool)
		srv.Schedules = postgres.NewScheduleStore(pool)
		srv.LandingZones = postgres.NewLandingZoneStore(pool)
		srv.TableMetadata = postgres.NewTableMetadataStore(pool)
		srv.Triggers = postgres.NewTriggerStore(pool)
		srv.Audit = postgres.NewAuditStore(pool)
		srv.FailedMerges = postgres.NewFailedMergesStore(pool)
		srv.Settings = postgres.NewSettingsStore(pool)

		srv.DBHealth = postgres.NewHealthChecker(pool)
		slog.Info("postgres stores initialized")

		// Wire plugin catalog persistence now that Postgres is available.
		pluginStore := postgres.NewPluginStore(pool)
		srv.PluginCatalog = pluginStore
		srv.PluginSources = pluginStore
		srv.PluginPolicies = pluginStore
		srv.PluginManager = mgr

		// Reconnect Manager to persistent catalog and load saved plugins.
		mgr.SetPolicies(pluginStore)
		mgr.SetSources(pluginStore)
		mgr.SetCatalog(pluginStore)
		if err := mgr.LoadFromCatalog(ctx); err != nil {
			slog.Warn("failed to load plugins from catalog", "error", err)
		}

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

	// Cloud provider: the registry implements api.CloudProvider directly via
	// the cloudv1 proto. Wire it now if a cloud plugin was registered at boot;
	// runtime registration/unregistration is handled by OnCloudChanged above.
	// Runner integration (passing creds into pipeline execution) is deferred —
	// see ADR-018.
	if registry.CloudEnabled() {
		srv.Cloud = registry
		slog.Info("cloud provider wired (capability \"cloud\")")
	}

	// Wire executor: AtomicExecutor provides thread-safe dynamic swapping.
	// Plugin executor (Pro) takes priority; community executor is the fallback.
	// When an executor plugin registers/unregisters at runtime, the OnExecutorChanged
	// callback swaps the active executor without downtime.
	atomicExec := executor.NewAtomicExecutor()
	srv.Executor = atomicExec

	onComplete := func(ctx context.Context, run *domain.Run, status domain.RunStatus) {
		if status != domain.RunStatusSuccess || srv.Triggers == nil {
			return
		}
		srv.EvaluatePipelineSuccessTriggers(ctx, run)
	}

	// Build the community executor from RUNNER_ADDR (if set).
	// This is kept running as a persistent fallback — never stopped.
	type stoppable interface{ Stop() }
	var communityExec api.Executor
	var stopCommunityExec func()
	if runnerAddr := os.Getenv("RUNNER_ADDR"); runnerAddr != "" {
		addrs := executor.ParseRunnerAddrs(runnerAddr)
		srv.RunnerHealth = transport.NewTCPHealthChecker(addrs[0], "runner")

		if len(addrs) > 1 {
			rr := executor.NewRoundRobinExecutor(addrs, srv.Runs, grpcClient)
			rr.SetLandingZones(srv.LandingZones)
			rr.SetOnRunComplete(onComplete)
			rr.Start(ctx)
			communityExec = rr
			stopCommunityExec = func() { rr.Stop() }
			slog.Info("community executor ready (round-robin)", "runners", len(addrs), "runner_addrs", strings.Join(addrs, ","))
		} else {
			exec := executor.NewWarmPoolExecutor(addrs[0], srv.Runs, grpcClient)
			exec.LandingZones = srv.LandingZones
			exec.OnRunComplete = onComplete
			exec.Start(ctx)
			communityExec = exec
			stopCommunityExec = func() { exec.Stop() }
			slog.Info("community executor ready (warmpool)", "runner_addr", addrs[0])
		}
	}

	// Track the currently active plugin executor so we can stop it on swap.
	var activePluginExec stoppable

	// activatePluginExecutor creates and starts a new PluginExecutor, swaps it
	// into the AtomicExecutor, and stops the previous plugin executor (if any).
	activatePluginExecutor := func(addr string) {
		pluginExec := executor.NewPluginExecutor(addr, srv.Runs, grpcClient)
		pluginExec.OnRunComplete = onComplete
		pluginExec.Start(ctx)

		old := atomicExec.Swap(pluginExec)
		srv.RunnerHealth = transport.NewTCPHealthChecker(addr, "runner")

		// Stop the previous plugin executor (not the community one — it keeps running).
		if activePluginExec != nil {
			activePluginExec.Stop()
		}
		activePluginExec = pluginExec

		_ = old // community exec keeps running for in-flight fallback
		slog.Info("executor activated (plugin)", "addr", addr)
	}

	// activateCommunityExecutor swaps in the community executor and stops the
	// previous plugin executor.
	activateCommunityExecutor := func() {
		if communityExec == nil {
			slog.Warn("no community executor available for fallback")
			return
		}
		atomicExec.Swap(communityExec)

		if activePluginExec != nil {
			activePluginExec.Stop()
			activePluginExec = nil
		}
		slog.Info("executor activated (community fallback)")
	}

	// Initial activation: plugin executor if already registered, else community.
	if registry.ExecutorEnabled() {
		addr := registry.GetExecutorAddr()
		activatePluginExecutor(addr)
	} else if communityExec != nil {
		atomicExec.Swap(communityExec)
		slog.Info("executor initialized (community)")
	}

	// Wire runner plugin lister for GET /api/v1/runner/plugins.
	// Uses the community executor's gRPC connection to call ListPlugins on the runner.
	if communityExec != nil {
		if lister, ok := communityExec.(api.RunnerPluginLister); ok {
			srv.RunnerPlugins = lister
		}
	}

	// Dynamic re-wiring: fired when an executor plugin registers or unregisters.
	mgr.OnExecutorChanged = func(reg *plugins.Registry) {
		if reg.ExecutorEnabled() {
			addr := reg.GetExecutorAddr()
			activatePluginExecutor(addr)
		} else {
			activateCommunityExecutor()
		}
	}

	// Shutdown hook: stop both plugin and community executors.
	stopExecutor = func() {
		if activePluginExec != nil {
			activePluginExec.Stop()
		}
		if stopCommunityExec != nil {
			stopCommunityExec()
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
			if eventBus != nil {
				sched.EventBus = eventBus
			}
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
			reap := reaper.New(srv.Settings, srv.Runs, srv.Pipelines, srv.LandingZones, srv.Storage, srv.Audit, srv.FailedMerges, nessieClient)
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
		//
		// A heartbeat goroutine pings Postgres every 5s while leader; two
		// consecutive failures force a voluntary unlock so a partitioned
		// replica cannot indefinitely hold the lock without running workers.
		tryLock := func(ctx context.Context) (bool, error) {
			var acquired bool
			err := pool.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", leader.AdvisoryLockID).Scan(&acquired)
			return acquired, err
		}
		// Heartbeat ping uses its own pool so a saturated main pool can't
		// starve liveness checks. Falls back to the shared pool when the
		// dedicated one is disabled via RAT_HEARTBEAT_POOL_ENABLED=false.
		pingPool := pool
		heartbeatSource := "shared-pool"
		if heartbeatPool != nil {
			pingPool = heartbeatPool
			heartbeatSource = "dedicated-pool"
		}
		ping := func(ctx context.Context) error { return pingPool.Ping(ctx) }
		unlock := func(ctx context.Context) error {
			_, err := pool.Exec(ctx, "SELECT pg_advisory_unlock($1)", leader.AdvisoryLockID)
			return err
		}
		elector := leader.New(
			tryLock,
			leader.RetryInterval,
			startBackgroundWorkers,
			leader.WithPing(ping),
			leader.WithUnlock(unlock),
		)
		elector.Start(ctx)
		stopLeader = func() { elector.Stop() }
		slog.Info("leader election started (advisory lock)",
			"heartbeat_interval", leader.DefaultHeartbeatInterval,
			"heartbeat_source", heartbeatSource)
	default:
		// No database — start workers directly (single-instance mode).
		stopFn := startBackgroundWorkers(ctx)
		stopLeader = stopFn
	}

	// Start plugin health loop (30s interval, checks all registered plugins).
	// Catalog may be nil if Postgres is not available — health loop handles that.
	healthLoop := plugins.NewHealthLoop(registry, mgr.Catalog())
	healthLoop.OnTransition = func(p *plugins.Plugin, _, _ domain.PluginStatus) {
		mgr.NotifyHealthTransition(p.Name)
	}
	healthLoop.Start(ctx)
	stopHealthLoop = func() { healthLoop.Stop() }
	slog.Info("plugin health loop started")

	// Start plugin reconciler. Periodically WARNs (but does not auto-fix) on
	// any divergence between the in-memory registry and the catalog. The
	// goroutine self-terminates when ctx is cancelled, so no explicit stop is
	// needed — graceful shutdown takes care of it.
	mgr.StartReconciler(ctx, plugins.DefaultReconcilerInterval)
	slog.Info("plugin reconciler started", "interval", plugins.DefaultReconcilerInterval)

	// Start event dispatcher if event bus is available.
	if eventBus != nil {
		adapter := &eventBusAdapter{bus: eventBus}
		dispatcher := plugins.NewEventDispatcher(registry, adapter)
		dispatcher.Start(ctx)
		stopDispatcher = func() { dispatcher.Stop() }
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

	publicRouter := api.NewRouter(srv)
	internalRouter := api.NewInternalRouter(srv)

	// Public listen address: RAT_LISTEN_ADDR > PORT (legacy) > default 127.0.0.1:8080.
	// Default binds to localhost only — users must explicitly set 0.0.0.0:8080
	// for network access, which triggers a security warning if no API key is set.
	addr := "127.0.0.1:8080"
	if listenAddr := os.Getenv("RAT_LISTEN_ADDR"); listenAddr != "" {
		addr = listenAddr
	} else if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	// Internal listen address: INTERNAL_LISTEN_ADDR > default 127.0.0.1:8090.
	//
	// This second listener hosts service-to-service callbacks (runner run-status
	// callback, plugin phone-home) with NO authentication. Its trust model is
	// "the network is the perimeter" — the operator MUST keep this port off the
	// public internet and ideally off the host network too.
	//
	// In a Docker compose deployment the default is overridden to 0.0.0.0:8090
	// so other containers on the bridge can reach it, while the port stays
	// unpublished to the host (no `ports:` mapping for 8090).
	internalAddr := "127.0.0.1:8090"
	if v := os.Getenv("INTERNAL_LISTEN_ADDR"); v != "" {
		internalAddr = v
	}

	// Warn if listening on all interfaces without authentication.
	if strings.HasPrefix(addr, "0.0.0.0") && os.Getenv("RAT_API_KEY") == "" && !registry.AuthEnabled() {
		slog.Warn("listening on 0.0.0.0 without RAT_API_KEY — API is unauthenticated and accessible from the network")
	}

	// Refuse to share a port between the public and internal listeners — that
	// would defeat the whole point of separating them.
	if internalAddr == addr {
		slog.Error("INTERNAL_LISTEN_ADDR must not equal RAT_LISTEN_ADDR",
			"public", addr, "internal", internalAddr)
		os.Exit(1)
	}

	publicServer := &http.Server{
		Addr:              addr,
		Handler:           publicRouter,
		ReadTimeout:       60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	}

	// The internal listener intentionally does NOT inherit TLS config — it is
	// expected to live on a trusted internal network where plaintext is fine.
	// Operators who want TLS on the internal listener can put a sidecar in
	// front of it; the simpler default keeps service-to-service callbacks
	// cheap and avoids cert rotation pain inside the container.
	internalServer := &http.Server{
		Addr:              internalAddr,
		Handler:           internalRouter,
		ReadTimeout:       60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start HTTP(S) server in a goroutine.
	tlsCertFile := os.Getenv("TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("TLS_KEY_FILE")

	errCh := make(chan error, 2)
	if tlsCertFile != "" && tlsKeyFile != "" {
		go func() {
			errCh <- publicServer.ListenAndServeTLS(tlsCertFile, tlsKeyFile)
		}()
		slog.Info("starting ratd public listener (HTTPS)", "addr", addr, "version", "2.0.0-dev")
	} else {
		go func() {
			errCh <- publicServer.ListenAndServe()
		}()
		slog.Info("starting ratd public listener", "addr", addr, "version", "2.0.0-dev")
	}

	// The internal listener is always plaintext HTTP — see comment on
	// internalServer above for rationale.
	go func() {
		errCh <- internalServer.ListenAndServe()
	}()
	slog.Info("starting ratd internal listener (service-to-service callbacks)",
		"addr", internalAddr,
		"warning", "do NOT expose this port to the public network")

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

	// Graceful shutdown: drain HTTP connections on both listeners (15s timeout).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := publicServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("public http shutdown error", "error", err)
	}
	if err := internalServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("internal http shutdown error", "error", err)
	}

	// Ordered cleanup: health loop → dispatcher → leader → executor → event bus → heartbeat pool → database pool.
	// Heartbeat pool closes before the main pool so any final leader.Stop()
	// unlock attempt (which uses the main pool) still has a connection.
	if stopHealthLoop != nil {
		stopHealthLoop()
		slog.Info("plugin health loop stopped")
	}
	if stopDispatcher != nil {
		stopDispatcher()
		slog.Info("event dispatcher stopped")
	}
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
	if closeHeartbeatPool != nil {
		closeHeartbeatPool()
		slog.Info("heartbeat pool closed")
	}
	if closePool != nil {
		closePool()
		slog.Info("database pool closed")
	}

	slog.Info("ratd shutdown complete")
}

// eventBusAdapter bridges postgres.EventBus (returns <-chan postgres.Event) to
// plugins.DispatchEventBus (returns <-chan plugins.DispatchEvent).
type eventBusAdapter struct {
	bus *postgres.PgEventBus
}

func (a *eventBusAdapter) Subscribe(channel string) (<-chan plugins.DispatchEvent, func()) {
	pgCh, cancel := a.bus.Subscribe(channel)
	out := make(chan plugins.DispatchEvent, cap(pgCh))
	go func() {
		defer close(out)
		for ev := range pgCh {
			out <- plugins.DispatchEvent{
				Channel: ev.Channel,
				Payload: ev.Payload,
			}
		}
	}()
	return out, cancel
}
