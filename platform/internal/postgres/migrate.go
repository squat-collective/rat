package postgres

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationLockID is a well-known advisory lock ID used to prevent concurrent
// migration execution. Derived from: SELECT hashtext('rat-migrations') → 779415198.
const migrationLockID int64 = 779415198

// migrationLockTimeoutSQL is the SET statement for advisory lock timeout.
// Prevents indefinite blocking if a lock holder crashes without releasing.
const migrationLockTimeoutSQL = "SET lock_timeout = '30s'"

// Migrate applies pending SQL migration files in order.
// It acquires a Postgres advisory lock to prevent concurrent instances from
// running migrations simultaneously. The lock is session-level and auto-releases
// if the connection drops.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// Acquire a dedicated connection for migration locking.
	// Advisory locks are session-level, so we need a single connection that
	// holds the lock for the entire migration run.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for migration: %w", err)
	}
	defer conn.Release()

	if err := acquireMigrationLock(ctx, conn.Conn()); err != nil {
		return err
	}
	defer releaseMigrationLock(ctx, conn.Conn())

	// Ensure migration tracking table exists
	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	// Load applied versions
	applied, err := loadAppliedMigrations(ctx, conn.Conn())
	if err != nil {
		return fmt.Errorf("load applied migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if applied[name] {
			slog.Debug("migration already applied, skipping", "file", name)
			continue
		}

		sql, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		slog.Info("applying migration", "file", name)
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		// Record applied migration
		if _, err := conn.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING",
			name,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}

	return nil
}

// acquireMigrationLock sets a lock_timeout and acquires a Postgres advisory lock
// to serialize migration execution across concurrent ratd instances.
func acquireMigrationLock(ctx context.Context, conn *pgx.Conn) error {
	// Set lock_timeout so we don't block forever if another instance holds the lock
	// and crashes. After this timeout, pg_advisory_lock will return an error.
	if _, err := conn.Exec(ctx, migrationLockTimeoutSQL); err != nil {
		return fmt.Errorf("set migration lock timeout: %w", err)
	}

	slog.Info("acquiring migration lock", "lock_id", migrationLockID)
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock (another instance may be migrating): %w", err)
	}
	slog.Info("migration lock acquired")

	return nil
}

// releaseMigrationLock explicitly releases the advisory lock. The lock would also
// auto-release when the connection is returned to the pool or closed.
func releaseMigrationLock(ctx context.Context, conn *pgx.Conn) {
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
		slog.Warn("failed to release migration lock", "error", err)
	}
	// Reset lock_timeout to default for the connection
	if _, err := conn.Exec(ctx, "SET lock_timeout = DEFAULT"); err != nil {
		slog.Warn("failed to reset lock_timeout", "error", err)
	}
}

// loadAppliedMigrations returns a set of already-applied migration filenames.
func loadAppliedMigrations(ctx context.Context, conn *pgx.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}
