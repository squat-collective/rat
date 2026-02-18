package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres/gen"
)

// TableMetadataStore implements api.TableMetadataStore backed by Postgres.
type TableMetadataStore struct {
	q *gen.Queries
}

// NewTableMetadataStore creates a TableMetadataStore backed by the given pool.
func NewTableMetadataStore(pool *pgxpool.Pool) *TableMetadataStore {
	return &TableMetadataStore{q: gen.New(pool)}
}

func tableMetadatumToDomain(r gen.TableMetadatum) domain.TableMetadata {
	m := domain.TableMetadata{
		ID:                 r.ID,
		Namespace:          r.Namespace,
		Layer:              r.Layer,
		Name:               r.Name,
		Description:        r.Description,
		Owner:              nullableTextToPtr(r.Owner),
		ColumnDescriptions: map[string]string{},
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
	if len(r.ColumnDescriptions) > 0 {
		_ = json.Unmarshal(r.ColumnDescriptions, &m.ColumnDescriptions)
	}
	return m
}

func (s *TableMetadataStore) Get(ctx context.Context, namespace, layer, name string) (*domain.TableMetadata, error) {
	row, err := s.q.GetTableMetadata(ctx, gen.GetTableMetadataParams{
		Namespace: namespace,
		Layer:     layer,
		Name:      name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get table metadata: %w", err)
	}
	m := tableMetadatumToDomain(row)
	return &m, nil
}

func (s *TableMetadataStore) Upsert(ctx context.Context, m *domain.TableMetadata) error {
	colDesc, err := json.Marshal(m.ColumnDescriptions)
	if err != nil {
		return fmt.Errorf("marshal column_descriptions: %w", err)
	}
	row, err := s.q.UpsertTableMetadata(ctx, gen.UpsertTableMetadataParams{
		Namespace:          m.Namespace,
		Layer:              m.Layer,
		Name:               m.Name,
		Description:        m.Description,
		Owner:              textPtrToNullable(m.Owner),
		ColumnDescriptions: colDesc,
	})
	if err != nil {
		return fmt.Errorf("upsert table metadata: %w", err)
	}
	m.ID = row.ID
	m.CreatedAt = row.CreatedAt
	m.UpdatedAt = row.UpdatedAt
	return nil
}

func (s *TableMetadataStore) ListAll(ctx context.Context) ([]domain.TableMetadata, error) {
	rows, err := s.q.ListAllTableMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("list table metadata: %w", err)
	}
	result := make([]domain.TableMetadata, len(rows))
	for i, r := range rows {
		result[i] = tableMetadatumToDomain(r)
	}
	return result, nil
}
