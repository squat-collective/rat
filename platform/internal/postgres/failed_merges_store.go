package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/domain"
)

// FailedMergesStore persists Phase 5 branch-merge audit records.
//
// When the runner exhausts its retry budget merging an ephemeral branch
// into main, the branch is RETAINED (not auto-deleted) so a human can
// recover the data. This store records what failed and why; the reaper
// consults it to avoid sweeping retained branches.
type FailedMergesStore struct {
	pool *pgxpool.Pool
}

// NewFailedMergesStore creates a FailedMergesStore backed by the given pool.
func NewFailedMergesStore(pool *pgxpool.Pool) *FailedMergesStore {
	return &FailedMergesStore{pool: pool}
}

// Create inserts a single failed-merge audit row. RunID and BranchName are
// required; SourceHash/TargetHash may be empty if Nessie was unreachable.
func (s *FailedMergesStore) Create(ctx context.Context, fm domain.FailedMerge) error {
	runID, err := uuid.Parse(fm.RunID)
	if err != nil {
		return fmt.Errorf("invalid run_id %q: %w", fm.RunID, err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO failed_merges
			(run_id, branch_name, source_hash, target_hash, error_kind, error_message)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		runID,
		fm.BranchName,
		textOrNull(fm.SourceHash),
		textOrNull(fm.TargetHash),
		fm.ErrorKind,
		fm.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert failed_merge: %w", err)
	}
	return nil
}

// RecentBranchNames returns the distinct branch names of failed-merge audit
// rows newer than `since`. The reaper uses this to skip branches that a
// human may still need to recover.
func (s *FailedMergesStore) RecentBranchNames(ctx context.Context, since time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT branch_name
		FROM failed_merges
		WHERE created_at >= $1
	`, since)
	if err != nil {
		return nil, fmt.Errorf("query recent failed_merges: %w", err)
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan failed_merge branch_name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate failed_merges: %w", err)
	}
	return names, nil
}
