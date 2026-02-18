package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/domain"
)

// VersionStore implements api.VersionStore backed by Postgres.
type VersionStore struct {
	pool *pgxpool.Pool
}

// NewVersionStore creates a VersionStore backed by the given pool.
func NewVersionStore(pool *pgxpool.Pool) *VersionStore {
	return &VersionStore{pool: pool}
}

func (s *VersionStore) ListVersions(ctx context.Context, pipelineID uuid.UUID) ([]domain.PipelineVersion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, pipeline_id, version_number, message, published_versions, created_at
		 FROM pipeline_versions WHERE pipeline_id = $1
		 ORDER BY version_number DESC`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()

	var result []domain.PipelineVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		result = append(result, *v)
	}
	return result, rows.Err()
}

func (s *VersionStore) GetVersion(ctx context.Context, pipelineID uuid.UUID, versionNumber int) (*domain.PipelineVersion, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, pipeline_id, version_number, message, published_versions, created_at
		 FROM pipeline_versions WHERE pipeline_id = $1 AND version_number = $2`,
		pipelineID, versionNumber)

	v, err := scanVersionRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get version: %w", err)
	}
	return v, nil
}

func (s *VersionStore) CreateVersion(ctx context.Context, v *domain.PipelineVersion) error {
	pvJSON, err := json.Marshal(v.PublishedVersions)
	if err != nil {
		return fmt.Errorf("marshal published versions: %w", err)
	}

	err = s.pool.QueryRow(ctx,
		`INSERT INTO pipeline_versions (pipeline_id, version_number, message, published_versions)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		v.PipelineID, v.VersionNumber, v.Message, pvJSON).Scan(&v.ID, &v.CreatedAt)
	if err != nil {
		return fmt.Errorf("create version: %w", err)
	}
	return nil
}

func (s *VersionStore) PruneVersions(ctx context.Context, pipelineID uuid.UUID, keepCount int) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM pipeline_versions
		 WHERE pipeline_id = $1 AND version_number NOT IN (
			SELECT version_number FROM pipeline_versions
			WHERE pipeline_id = $1
			ORDER BY version_number DESC LIMIT $2
		 )`, pipelineID, keepCount)
	if err != nil {
		return fmt.Errorf("prune versions: %w", err)
	}
	return nil
}

func (s *VersionStore) LatestVersionNumber(ctx context.Context, pipelineID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version_number), 0) FROM pipeline_versions WHERE pipeline_id = $1`,
		pipelineID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("latest version number: %w", err)
	}
	return n, nil
}

// scanVersion scans a version from pgx.Rows (multi-row result).
func scanVersion(rows pgx.Rows) (*domain.PipelineVersion, error) {
	var v domain.PipelineVersion
	var pvJSON []byte
	err := rows.Scan(&v.ID, &v.PipelineID, &v.VersionNumber, &v.Message, &pvJSON, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(pvJSON) > 0 {
		if err := json.Unmarshal(pvJSON, &v.PublishedVersions); err != nil {
			return nil, fmt.Errorf("unmarshal published_versions: %w", err)
		}
	}
	return &v, nil
}

// scanVersionRow scans a version from pgx.Row (single-row result).
func scanVersionRow(row pgx.Row) (*domain.PipelineVersion, error) {
	var v domain.PipelineVersion
	var pvJSON []byte
	err := row.Scan(&v.ID, &v.PipelineID, &v.VersionNumber, &v.Message, &pvJSON, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(pvJSON) > 0 {
		if err := json.Unmarshal(pvJSON, &v.PublishedVersions); err != nil {
			return nil, fmt.Errorf("unmarshal published_versions: %w", err)
		}
	}
	return &v, nil
}
