package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/postgres"
)

// testPool returns a pgxpool.Pool connected to the test database.
// It skips the test if DATABASE_URL is not set (so `make test-go` stays fast).
// It runs migrations and cleans all tables before returning.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cleanTables(t, pool)

	return pool
}

// cleanTables truncates all tables and re-seeds the default namespace.
func cleanTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	// Order matters — FK constraints
	tables := []string{
		"landing_files", "landing_zones",
		"quality_results", "quality_tests",
		"schedules", "runs", "pipelines", "namespaces",
		"plugins",
	}
	for _, table := range tables {
		if _, err := pool.Exec(ctx, "TRUNCATE "+table+" CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}

	// Re-seed default namespace
	if _, err := pool.Exec(ctx, "INSERT INTO namespaces (name) VALUES ('default')"); err != nil {
		t.Fatalf("seed default namespace: %v", err)
	}
}
