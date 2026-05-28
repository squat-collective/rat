package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres/gen"
)

// InTx runs fn inside a pgx transaction. Commits on a clean return from fn,
// rolls back on any error or panic.
//
// Contract: fn MUST only perform database work against the supplied tx. Do
// NOT make HTTP calls, gRPC calls, file IO, or anything else that blocks on
// a remote system inside fn — a pooled DB connection is held for the entire
// duration and starving the pool will deadlock the platform under load.
// The pattern for handlers that mix DB writes and network IO is: do the
// IO first (or last), then open a tx for the DB mutations only.
//
// This is a low-level helper. Most callers should use TxRunner.InTx, which
// exposes tx-bound stores so handlers do not have to import pgx.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) (err error) {
	tx, beginErr := pool.Begin(ctx)
	if beginErr != nil {
		return fmt.Errorf("begin tx: %w", beginErr)
	}

	// Recover from a panic inside fn so we always release the connection.
	// Re-panic after rolling back so the original stack still reaches the
	// process panic handler — atomicity guarantees, not panic-swallowing.
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx) //nolint:errcheck // best-effort during panic
			panic(p)
		}
		if err != nil {
			// Best-effort rollback. If commit already ran, Rollback returns
			// ErrTxClosed which we deliberately ignore.
			_ = tx.Rollback(ctx) //nolint:errcheck
		}
	}()

	if err = fn(tx); err != nil { //nolint:gocritic // must assign to the named return so the deferred rollback sees fn's error
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// TxRunner wires together the tx-bound stores used by multi-step handlers.
// Construct via NewTxRunner(pool). Pass through api.TxRunner to handlers.
type TxRunner struct {
	pool *pgxpool.Pool
}

// NewTxRunner creates a TxRunner backed by the given pool.
func NewTxRunner(pool *pgxpool.Pool) *TxRunner {
	return &TxRunner{pool: pool}
}

// InTx runs fn inside a pgx transaction with tx-scoped stores.
// Same contract as the free InTx: fn must perform DB work only.
func (t *TxRunner) InTx(ctx context.Context, fn func(api.TxStores) error) error {
	return InTx(ctx, t.pool, func(tx pgx.Tx) error {
		txQ := gen.New(tx)
		return fn(api.TxStores{
			Runs:      &RunStore{pool: t.pool, q: txQ},
			Triggers:  &TriggerStore{q: txQ},
			Schedules: &ScheduleStore{q: txQ},
		})
	})
}

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
