package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/api"
	"github.com/rat-data/rat/platform/internal/domain"
)

// pipelineColumns is the full column list for pipeline queries.
const pipelineColumns = `id, namespace, layer, name, type, s3_path, description, owner,
	published_at, published_versions, draft_dirty, max_versions, created_at, updated_at`

// PipelineStore implements api.PipelineStore backed by Postgres.
type PipelineStore struct {
	pool     *pgxpool.Pool
	EventBus EventBus // optional — publishes pipeline_created/updated events when set
}

// NewPipelineStore creates a PipelineStore backed by the given pool.
func NewPipelineStore(pool *pgxpool.Pool) *PipelineStore {
	return &PipelineStore{pool: pool}
}

// scanPipeline scans a single pipeline row into domain.Pipeline.
func scanPipeline(row pgx.Row) (*domain.Pipeline, error) {
	var (
		id                uuid.UUID
		namespace, layer  string
		name, typ, s3Path string
		description       pgtype.Text
		owner             pgtype.Text
		publishedAt       *time.Time
		publishedVersions []byte
		draftDirty        bool
		maxVersions       int
		createdAt         time.Time
		updatedAt         time.Time
	)

	err := row.Scan(&id, &namespace, &layer, &name, &typ, &s3Path,
		&description, &owner, &publishedAt, &publishedVersions,
		&draftDirty, &maxVersions, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	p := pipelineRowToDomain(id, namespace, layer, name, typ, s3Path,
		description, owner, publishedAt, publishedVersions, draftDirty,
		maxVersions, createdAt, updatedAt)
	return &p, nil
}

// pipelineWhereClause builds the shared WHERE clause and args for pipeline list/count queries.
func pipelineWhereClause(filter api.PipelineFilter) (string, []interface{}, int) {
	where := ` WHERE deleted_at IS NULL`
	args := []interface{}{}
	argN := 1

	if filter.Namespace != "" {
		where += fmt.Sprintf(" AND namespace = $%d", argN)
		args = append(args, filter.Namespace)
		argN++
	}
	if filter.Layer != "" {
		where += fmt.Sprintf(" AND layer = $%d", argN)
		args = append(args, filter.Layer)
		argN++
	}
	return where, args, argN
}

func (s *PipelineStore) ListPipelines(ctx context.Context, filter api.PipelineFilter) ([]domain.Pipeline, error) {
	where, args, argN := pipelineWhereClause(filter)
	query := `SELECT ` + pipelineColumns + ` FROM pipelines` + where + ` ORDER BY created_at DESC`

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argN, argN+1)
		args = append(args, filter.Limit, filter.Offset)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	defer rows.Close()

	var result []domain.Pipeline
	for rows.Next() {
		var (
			id                uuid.UUID
			namespace, layer  string
			name, typ, s3Path string
			description       pgtype.Text
			owner             pgtype.Text
			publishedAt       *time.Time
			publishedVersions []byte
			draftDirty        bool
			maxVersions       int
			createdAt         time.Time
			updatedAt         time.Time
		)

		if err := rows.Scan(&id, &namespace, &layer, &name, &typ, &s3Path,
			&description, &owner, &publishedAt, &publishedVersions,
			&draftDirty, &maxVersions, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline: %w", err)
		}

		result = append(result, pipelineRowToDomain(id, namespace, layer, name, typ, s3Path,
			description, owner, publishedAt, publishedVersions, draftDirty,
			maxVersions, createdAt, updatedAt))
	}
	return result, rows.Err()
}

// CountPipelines returns the total count of pipelines matching the filter (ignoring Limit/Offset).
func (s *PipelineStore) CountPipelines(ctx context.Context, filter api.PipelineFilter) (int, error) {
	where, args, _ := pipelineWhereClause(filter)
	query := `SELECT COUNT(*) FROM pipelines` + where

	var count int
	err := s.pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pipelines: %w", err)
	}
	return count, nil
}

func (s *PipelineStore) GetPipeline(ctx context.Context, namespace, layer, name string) (*domain.Pipeline, error) {
	query := `SELECT ` + pipelineColumns + ` FROM pipelines
		WHERE namespace = $1 AND layer = $2 AND name = $3 AND deleted_at IS NULL`

	p, err := scanPipeline(s.pool.QueryRow(ctx, query, namespace, layer, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get pipeline: %w", err)
	}
	return p, nil
}

func (s *PipelineStore) GetPipelineByID(ctx context.Context, id string) (*domain.Pipeline, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, nil
	}

	query := `SELECT ` + pipelineColumns + ` FROM pipelines
		WHERE id = $1 AND deleted_at IS NULL`

	p, err := scanPipeline(s.pool.QueryRow(ctx, query, uid))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get pipeline by id: %w", err)
	}
	return p, nil
}

func (s *PipelineStore) CreatePipeline(ctx context.Context, p *domain.Pipeline) error {
	query := `INSERT INTO pipelines (namespace, layer, name, type, s3_path, description, owner)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + pipelineColumns

	row := s.pool.QueryRow(ctx, query,
		p.Namespace, string(p.Layer), p.Name, p.Type, p.S3Path,
		pgtype.Text{String: p.Description, Valid: true},
		textPtrToNullable(p.Owner))

	created, err := scanPipeline(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("pipeline %s/%s/%s: %w", p.Namespace, p.Layer, p.Name, domain.ErrAlreadyExists)
		}
		return fmt.Errorf("create pipeline: %w", err)
	}

	p.ID = created.ID
	p.CreatedAt = created.CreatedAt
	p.UpdatedAt = created.UpdatedAt
	p.MaxVersions = created.MaxVersions

	// Best-effort event publishing — does not fail the create.
	if s.EventBus != nil {
		_ = s.EventBus.Publish(ctx, ChannelPipelineCreated, PipelineEventPayload{
			PipelineID: p.ID.String(),
			Namespace:  p.Namespace,
			Layer:      string(p.Layer),
			Name:       p.Name,
		})
	}

	return nil
}

func (s *PipelineStore) UpdatePipeline(ctx context.Context, namespace, layer, name string, update api.UpdatePipelineRequest) (*domain.Pipeline, error) {
	query := `UPDATE pipelines SET
		description = COALESCE($4, description),
		type = COALESCE($5, type),
		owner = COALESCE($6, owner),
		updated_at = NOW()
		WHERE namespace = $1 AND layer = $2 AND name = $3 AND deleted_at IS NULL
		RETURNING ` + pipelineColumns

	p, err := scanPipeline(s.pool.QueryRow(ctx, query,
		namespace, layer, name,
		textPtrToNullable(update.Description),
		textPtrToNullable(update.Type),
		textPtrToNullable(update.Owner)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update pipeline: %w", err)
	}

	// Best-effort event publishing — does not fail the update.
	if s.EventBus != nil && p != nil {
		_ = s.EventBus.Publish(ctx, ChannelPipelineUpdated, PipelineEventPayload{
			PipelineID: p.ID.String(),
			Namespace:  p.Namespace,
			Layer:      string(p.Layer),
			Name:       p.Name,
		})
	}

	return p, nil
}

func (s *PipelineStore) DeletePipeline(ctx context.Context, namespace, layer, name string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE pipelines SET deleted_at = NOW() WHERE namespace = $1 AND layer = $2 AND name = $3 AND deleted_at IS NULL`,
		namespace, layer, name)
	return err
}

func (s *PipelineStore) SetDraftDirty(ctx context.Context, namespace, layer, name string, dirty bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE pipelines SET draft_dirty = $4, updated_at = NOW()
		 WHERE namespace = $1 AND layer = $2 AND name = $3 AND deleted_at IS NULL`,
		namespace, layer, name, dirty)
	return err
}

func (s *PipelineStore) PublishPipeline(ctx context.Context, namespace, layer, name string, versions map[string]string) error {
	versionsJSON, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("marshal published versions: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE pipelines SET published_at = NOW(), published_versions = $4, draft_dirty = false, updated_at = NOW()
		 WHERE namespace = $1 AND layer = $2 AND name = $3 AND deleted_at IS NULL`,
		namespace, layer, name, versionsJSON)
	return err
}

// UpdatePipelineRetention sets per-pipeline retention overrides (JSONB).
func (s *PipelineStore) UpdatePipelineRetention(ctx context.Context, pipelineID uuid.UUID, config json.RawMessage) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE pipelines SET retention_config = $2, updated_at = NOW() WHERE id = $1`,
		pipelineID, config,
	)
	if err != nil {
		return fmt.Errorf("update pipeline retention: %w", err)
	}
	return nil
}

// ListSoftDeletedPipelines returns pipelines that were soft-deleted before the given time.
func (s *PipelineStore) ListSoftDeletedPipelines(ctx context.Context, olderThan time.Time) ([]domain.Pipeline, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+pipelineColumns+`, deleted_at FROM pipelines WHERE deleted_at IS NOT NULL AND deleted_at < $1`,
		olderThan,
	)
	if err != nil {
		return nil, fmt.Errorf("list soft-deleted pipelines: %w", err)
	}
	defer rows.Close()

	var result []domain.Pipeline
	for rows.Next() {
		var (
			id                uuid.UUID
			namespace, layer  string
			name, typ, s3Path string
			description       pgtype.Text
			owner             pgtype.Text
			publishedAt       *time.Time
			publishedVersions []byte
			draftDirty        bool
			maxVersions       int
			createdAt         time.Time
			updatedAt         time.Time
			deletedAt         *time.Time
		)
		if err := rows.Scan(&id, &namespace, &layer, &name, &typ, &s3Path,
			&description, &owner, &publishedAt, &publishedVersions,
			&draftDirty, &maxVersions, &createdAt, &updatedAt, &deletedAt); err != nil {
			return nil, fmt.Errorf("scan soft-deleted pipeline: %w", err)
		}
		p := pipelineRowToDomain(id, namespace, layer, name, typ, s3Path,
			description, owner, publishedAt, publishedVersions, draftDirty,
			maxVersions, createdAt, updatedAt)
		p.DeletedAt = deletedAt
		result = append(result, p)
	}
	return result, rows.Err()
}

// HardDeletePipeline permanently removes a pipeline row (after soft-delete purge period).
func (s *PipelineStore) HardDeletePipeline(ctx context.Context, pipelineID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM pipelines WHERE id = $1`, pipelineID)
	if err != nil {
		return fmt.Errorf("hard delete pipeline: %w", err)
	}
	return nil
}
