package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthChecker implements api.HealthChecker for Postgres.
// It pings the connection pool to verify connectivity.
type HealthChecker struct {
	pool *pgxpool.Pool
}

// NewHealthChecker creates a Postgres health checker backed by the given pool.
func NewHealthChecker(pool *pgxpool.Pool) *HealthChecker {
	return &HealthChecker{pool: pool}
}

// HealthCheck pings the Postgres pool. Returns nil if the database is reachable.
func (h *HealthChecker) HealthCheck(ctx context.Context) error {
	if err := h.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	return nil
}
