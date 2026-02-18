package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/domain"
)

// PipelinePublisher provides transactional publish and rollback operations.
// All steps within a single call share a database transaction so they either
// succeed together or roll back atomically.
type PipelinePublisher struct {
	pool *pgxpool.Pool
}

// NewPipelinePublisher creates a PipelinePublisher backed by the given pool.
func NewPipelinePublisher(pool *pgxpool.Pool) *PipelinePublisher {
	return &PipelinePublisher{pool: pool}
}

// PublishPipelineTx atomically: updates the pipeline's published state,
// creates a version history record, and prunes old versions.
func (p *PipelinePublisher) PublishPipelineTx(ctx context.Context, ns, layer, name string, versions map[string]string, pv *domain.PipelineVersion, keepCount int) error {
	versionsJSON, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("marshal published versions: %w", err)
	}
	pvJSON, err := json.Marshal(pv.PublishedVersions)
	if err != nil {
		return fmt.Errorf("marshal version published versions: %w", err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin publish tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Step 1: Update pipeline published state
	_, err = tx.Exec(ctx,
		`UPDATE pipelines SET published_at = NOW(), published_versions = $4, draft_dirty = false, updated_at = NOW()
		 WHERE namespace = $1 AND layer = $2 AND name = $3 AND deleted_at IS NULL`,
		ns, layer, name, versionsJSON)
	if err != nil {
		return fmt.Errorf("update pipeline published state: %w", err)
	}

	// Step 2: Create version record
	err = tx.QueryRow(ctx,
		`INSERT INTO pipeline_versions (pipeline_id, version_number, message, published_versions)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		pv.PipelineID, pv.VersionNumber, pv.Message, pvJSON).Scan(&pv.ID, &pv.CreatedAt)
	if err != nil {
		return fmt.Errorf("create version record: %w", err)
	}

	// Step 3: Prune old versions
	_, err = tx.Exec(ctx,
		`DELETE FROM pipeline_versions
		 WHERE pipeline_id = $1 AND version_number NOT IN (
			SELECT version_number FROM pipeline_versions
			WHERE pipeline_id = $1
			ORDER BY version_number DESC LIMIT $2
		 )`, pv.PipelineID, keepCount)
	if err != nil {
		return fmt.Errorf("prune versions: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit publish tx: %w", err)
	}
	return nil
}

// RollbackPipelineTx atomically: creates a new version record with the old
// snapshot, applies that snapshot as the pipeline's published state, and
// prunes old versions.
func (p *PipelinePublisher) RollbackPipelineTx(ctx context.Context, ns, layer, name string, versions map[string]string, pv *domain.PipelineVersion, keepCount int) error {
	versionsJSON, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("marshal rollback versions: %w", err)
	}
	pvJSON, err := json.Marshal(pv.PublishedVersions)
	if err != nil {
		return fmt.Errorf("marshal version published versions: %w", err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin rollback tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Step 1: Create new version record with old snapshot
	err = tx.QueryRow(ctx,
		`INSERT INTO pipeline_versions (pipeline_id, version_number, message, published_versions)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		pv.PipelineID, pv.VersionNumber, pv.Message, pvJSON).Scan(&pv.ID, &pv.CreatedAt)
	if err != nil {
		return fmt.Errorf("create rollback version record: %w", err)
	}

	// Step 2: Apply the old snapshot as the pipeline's published versions
	_, err = tx.Exec(ctx,
		`UPDATE pipelines SET published_at = NOW(), published_versions = $4, draft_dirty = false, updated_at = NOW()
		 WHERE namespace = $1 AND layer = $2 AND name = $3 AND deleted_at IS NULL`,
		ns, layer, name, versionsJSON)
	if err != nil {
		return fmt.Errorf("apply rollback versions: %w", err)
	}

	// Step 3: Prune old versions
	_, err = tx.Exec(ctx,
		`DELETE FROM pipeline_versions
		 WHERE pipeline_id = $1 AND version_number NOT IN (
			SELECT version_number FROM pipeline_versions
			WHERE pipeline_id = $1
			ORDER BY version_number DESC LIMIT $2
		 )`, pv.PipelineID, keepCount)
	if err != nil {
		return fmt.Errorf("prune versions: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rollback tx: %w", err)
	}
	return nil
}
