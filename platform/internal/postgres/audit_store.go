package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/domain"
)

// AuditStore provides audit log persistence.
type AuditStore struct {
	pool *pgxpool.Pool
}

// NewAuditStore creates an AuditStore backed by the given pool.
func NewAuditStore(pool *pgxpool.Pool) *AuditStore {
	return &AuditStore{pool: pool}
}

// Log records an audit entry.
func (s *AuditStore) Log(ctx context.Context, userID, action, resource, detail, ip string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_log (user_id, action, resource, detail, ip) VALUES ($1, $2, $3, $4, $5)`,
		userID, action, resource, detail, ip,
	)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}

// List returns recent audit entries, most recent first.
func (s *AuditStore) List(ctx context.Context, limit, offset int) ([]domain.AuditEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, action, resource, detail, COALESCE(ip, ''), created_at
		 FROM audit_log ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.AuditEntry
	for rows.Next() {
		var e domain.AuditEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.Resource, &e.Detail, &e.IP, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit entries: %w", err)
	}
	if entries == nil {
		entries = []domain.AuditEntry{}
	}
	return entries, nil
}

// DeleteOlderThan removes audit entries older than the given time.
// Returns the number of entries deleted.
func (s *AuditStore) DeleteOlderThan(ctx context.Context, olderThan time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM audit_log WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("delete old audit entries: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
