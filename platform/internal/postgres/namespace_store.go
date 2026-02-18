package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/domain"
	"github.com/rat-data/rat/platform/internal/postgres/gen"
)

// NamespaceStore implements api.NamespaceStore backed by Postgres.
type NamespaceStore struct {
	q *gen.Queries
}

// NewNamespaceStore creates a NamespaceStore backed by the given pool.
func NewNamespaceStore(pool *pgxpool.Pool) *NamespaceStore {
	return &NamespaceStore{q: gen.New(pool)}
}

func (s *NamespaceStore) ListNamespaces(ctx context.Context) ([]domain.Namespace, error) {
	rows, err := s.q.ListNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	result := make([]domain.Namespace, len(rows))
	for i, r := range rows {
		result[i] = domain.Namespace{
			Name:        r.Name,
			Description: r.Description,
			CreatedBy:   nullableTextToPtr(r.CreatedBy),
			CreatedAt:   r.CreatedAt,
		}
	}
	return result, nil
}

func (s *NamespaceStore) CreateNamespace(ctx context.Context, name string, createdBy *string) error {
	if err := s.q.CreateNamespace(ctx, gen.CreateNamespaceParams{Name: name, CreatedBy: textPtrToNullable(createdBy)}); err != nil {
		return fmt.Errorf("namespace %q already exists", name)
	}
	return nil
}

func (s *NamespaceStore) DeleteNamespace(ctx context.Context, name string) error {
	return s.q.DeleteNamespace(ctx, name)
}

func (s *NamespaceStore) UpdateNamespace(ctx context.Context, name, description string) error {
	return s.q.UpdateNamespace(ctx, gen.UpdateNamespaceParams{Name: name, Description: description})
}
