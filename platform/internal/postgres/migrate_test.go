package postgres_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPoolForMigration creates a pool without running migrations first,
// so we can test the Migrate function itself.
func testPoolForMigration(t *testing.T) *pgxpool.Pool {
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

	return pool
}

func TestMigrate_AcquiresAdvisoryLock(t *testing.T) {
	pool := testPoolForMigration(t)
	ctx := context.Background()

	// Run migrations — should succeed and acquire/release the lock
	err := postgres.Migrate(ctx, pool)
	require.NoError(t, err)

	// Verify the advisory lock is NOT held after Migrate returns.
	// pg_try_advisory_lock returns true if the lock was successfully acquired
	// (meaning nobody else holds it).
	var acquired bool
	err = pool.QueryRow(ctx, "SELECT pg_try_advisory_lock(779415198)").Scan(&acquired)
	require.NoError(t, err)
	assert.True(t, acquired, "advisory lock should be released after Migrate completes")

	// Clean up: release the lock we just acquired for the check
	_, err = pool.Exec(ctx, "SELECT pg_advisory_unlock(779415198)")
	require.NoError(t, err)
}

func TestMigrate_ConcurrentCallsAreSerialized(t *testing.T) {
	pool := testPoolForMigration(t)
	ctx := context.Background()

	// Run initial migration to ensure tables exist
	err := postgres.Migrate(ctx, pool)
	require.NoError(t, err)

	// Run two concurrent migrations — both should succeed because
	// the advisory lock serializes them (second waits for first).
	const concurrency = 3
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = postgres.Migrate(ctx, pool)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "concurrent migration %d should succeed", i)
	}
}

func TestMigrate_IdempotentOnRepeatedCalls(t *testing.T) {
	pool := testPoolForMigration(t)
	ctx := context.Background()

	// First run applies migrations
	err := postgres.Migrate(ctx, pool)
	require.NoError(t, err)

	// Second run should be a no-op (all migrations already applied)
	err = postgres.Migrate(ctx, pool)
	require.NoError(t, err)

	// Verify schema_migrations table has entries
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "should have at least one recorded migration")
}

func TestMigrate_LockBlocksSecondCaller(t *testing.T) {
	pool := testPoolForMigration(t)
	ctx := context.Background()

	// Acquire the migration advisory lock manually on a separate connection,
	// simulating another ratd instance running migrations.
	lockConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer lockConn.Release()

	_, err = lockConn.Exec(ctx, "SELECT pg_advisory_lock(779415198)")
	require.NoError(t, err)

	// Now try to Migrate with a short context timeout.
	// It should fail because the lock is held by our manual connection.
	shortCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err = postgres.Migrate(shortCtx, pool)
	assert.Error(t, err, "Migrate should fail when the lock is already held and context times out")

	// Release the manually-held lock
	_, err = lockConn.Exec(ctx, "SELECT pg_advisory_unlock(779415198)")
	require.NoError(t, err)
}
