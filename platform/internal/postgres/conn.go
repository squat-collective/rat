// Package postgres implements Postgres-backed stores for the ratd platform.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Default pgxpool connection limits.
// These can be overridden via environment variables:
//   - DB_MAX_CONNS: maximum number of connections in the pool (default 25)
//   - DB_MIN_CONNS: minimum idle connections kept alive (default 5)
//   - DB_MAX_CONN_LIFETIME: maximum lifetime of a connection (default 1h)
//   - DB_MAX_CONN_IDLE_TIME: maximum idle time before closing (default 30m)
//   - DB_HEALTH_CHECK_PERIOD: how often idle connections are health-checked (default 1m)
const (
	defaultMaxConns          = 25
	defaultMinConns          = 5
	defaultMaxConnLifetime   = 1 * time.Hour
	defaultMaxConnIdleTime   = 30 * time.Minute
	defaultHealthCheckPeriod = 1 * time.Minute
)

// NewPool creates a pgxpool.Pool from a DATABASE_URL connection string.
// Connection pool limits are configurable via environment variables.
// Sensible defaults are applied when env vars are not set.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	// Apply connection pool limits. The DATABASE_URL may already contain some
	// of these (e.g. ?pool_max_conns=10), so env vars override URL params.
	config.MaxConns = int32(envInt("DB_MAX_CONNS", defaultMaxConns))
	config.MinConns = int32(envInt("DB_MIN_CONNS", defaultMinConns))
	config.MaxConnLifetime = envDuration("DB_MAX_CONN_LIFETIME", defaultMaxConnLifetime)
	config.MaxConnIdleTime = envDuration("DB_MAX_CONN_IDLE_TIME", defaultMaxConnIdleTime)
	config.HealthCheckPeriod = envDuration("DB_HEALTH_CHECK_PERIOD", defaultHealthCheckPeriod)

	slog.Info("pgxpool configured",
		"max_conns", config.MaxConns,
		"min_conns", config.MinConns,
		"max_conn_lifetime", config.MaxConnLifetime,
		"max_conn_idle_time", config.MaxConnIdleTime,
		"health_check_period", config.HealthCheckPeriod,
	)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// NewHeartbeatPool creates a dedicated single-connection pool for the leader
// elector's heartbeat ping. Using a separate pool guarantees the heartbeat
// never contends with handler queries on the main pool — a saturated main
// pool used to starve the heartbeat, causing the leader to voluntarily step
// down and replicas to ping-pong leadership every ~10s.
//
// The pool is pinned at one connection (MaxConns=MinConns=1) and tagged with
// application_name="ratd-heartbeat" so it's distinguishable in pg_stat_activity.
// Lifetime/idle/health-check defaults match NewPool — there's no reason these
// would diverge.
func NewHeartbeatPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	config.MaxConns = 1
	config.MinConns = 1
	config.MaxConnLifetime = envDuration("DB_MAX_CONN_LIFETIME", defaultMaxConnLifetime)
	config.MaxConnIdleTime = envDuration("DB_MAX_CONN_IDLE_TIME", defaultMaxConnIdleTime)
	config.HealthCheckPeriod = envDuration("DB_HEALTH_CHECK_PERIOD", defaultHealthCheckPeriod)

	// Tag the connection so it's distinguishable from the main pool in
	// pg_stat_activity. RuntimeParams flow through to the server as
	// startup options.
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = map[string]string{}
	}
	config.ConnConfig.RuntimeParams["application_name"] = "ratd-heartbeat"

	slog.Info("pgxpool (heartbeat) configured",
		"max_conns", config.MaxConns,
		"min_conns", config.MinConns,
		"application_name", "ratd-heartbeat",
	)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create heartbeat pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping heartbeat pool: %w", err)
	}

	return pool, nil
}

// envInt reads an integer from an environment variable, returning defaultVal if unset or invalid.
func envInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid integer env var, using default", "key", key, "value", v, "default", defaultVal)
		return defaultVal
	}
	return n
}

// envDuration reads a Go duration from an environment variable, returning defaultVal if unset or invalid.
func envDuration(key string, defaultVal time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid duration env var, using default", "key", key, "value", v, "default", defaultVal)
		return defaultVal
	}
	return d
}
