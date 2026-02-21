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
	"github.com/rat-data/rat/platform/internal/postgres/gen"
)

// PluginStore implements plugin catalog CRUD backed by Postgres.
type PluginStore struct {
	q *gen.Queries
}

// NewPluginStore creates a PluginStore backed by the given pool.
func NewPluginStore(pool *pgxpool.Pool) *PluginStore {
	return &PluginStore{q: gen.New(pool)}
}

func (s *PluginStore) ListPlugins(ctx context.Context, filter domain.PluginFilter) ([]domain.PluginEntry, error) {
	rows, err := s.q.ListPlugins(ctx, gen.ListPluginsParams{
		FilterStatus: textOrNull(filter.Status),
		FilterKind:   textOrNull(filter.Kind),
	})
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}

	result := make([]domain.PluginEntry, len(rows))
	for i, r := range rows {
		result[i] = pluginRowToDomain(r)
	}
	return result, nil
}

func (s *PluginStore) GetPlugin(ctx context.Context, name string) (*domain.PluginEntry, error) {
	row, err := s.q.GetPluginByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get plugin %s: %w", name, err)
	}
	entry := pluginRowToDomain(row)
	return &entry, nil
}

func (s *PluginStore) UpsertPlugin(ctx context.Context, entry domain.PluginEntry) (*domain.PluginEntry, error) {
	descriptor := entry.Descriptor
	if descriptor == nil {
		descriptor = json.RawMessage("{}")
	}
	config := entry.Config
	if config == nil {
		config = json.RawMessage("{}")
	}

	row, err := s.q.UpsertPlugin(ctx, gen.UpsertPluginParams{
		Name:       entry.Name,
		Kind:       string(entry.Kind),
		Version:    entry.Version,
		Status:     string(entry.Status),
		Error:      textOrNull(entry.Error),
		Descriptor: descriptor,
		Config:     config,
		Addr:       entry.Addr,
		Healthy:    entry.Healthy,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert plugin %s: %w", entry.Name, err)
	}
	result := pluginRowToDomain(row)
	return &result, nil
}

func (s *PluginStore) UpdatePluginStatus(ctx context.Context, name string, status domain.PluginStatus, errMsg string) error {
	err := s.q.UpdatePluginStatus(ctx, gen.UpdatePluginStatusParams{
		Name:   name,
		Status: string(status),
		Error:  textOrNull(errMsg),
	})
	if err != nil {
		return fmt.Errorf("update plugin status %s: %w", name, err)
	}
	return nil
}

func (s *PluginStore) UpdatePluginConfig(ctx context.Context, name string, config json.RawMessage) (*domain.PluginEntry, error) {
	row, err := s.q.UpdatePluginConfig(ctx, gen.UpdatePluginConfigParams{
		Name:   name,
		Config: config,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update plugin config %s: %w", name, err)
	}
	result := pluginRowToDomain(row)
	return &result, nil
}

func (s *PluginStore) UpdatePluginHealth(ctx context.Context, name string, healthy bool, errMsg string) error {
	err := s.q.UpdatePluginHealth(ctx, gen.UpdatePluginHealthParams{
		Name:    name,
		Healthy: healthy,
		Error:   textOrNull(errMsg),
	})
	if err != nil {
		return fmt.Errorf("update plugin health %s: %w", name, err)
	}
	return nil
}

func (s *PluginStore) DeletePlugin(ctx context.Context, name string) error {
	err := s.q.DeletePlugin(ctx, name)
	if err != nil {
		return fmt.Errorf("delete plugin %s: %w", name, err)
	}
	return nil
}

// ── Plugin Sources ─────────────────────────────────────────────

func (s *PluginStore) ListPluginSources(ctx context.Context) ([]domain.PluginSource, error) {
	rows, err := s.q.ListPluginSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list plugin sources: %w", err)
	}
	result := make([]domain.PluginSource, len(rows))
	for i, r := range rows {
		result[i] = domain.PluginSource{
			ID:        r.ID,
			Type:      r.Type,
			URL:       r.Url,
			Trusted:   r.Trusted,
			Enabled:   r.Enabled,
			CreatedAt: r.CreatedAt,
		}
	}
	return result, nil
}

func (s *PluginStore) CreatePluginSource(ctx context.Context, src domain.PluginSource) (*domain.PluginSource, error) {
	row, err := s.q.CreatePluginSource(ctx, gen.CreatePluginSourceParams{
		Type:    src.Type,
		Url:     src.URL,
		Trusted: src.Trusted,
		Enabled: src.Enabled,
	})
	if err != nil {
		return nil, fmt.Errorf("create plugin source: %w", err)
	}
	return &domain.PluginSource{
		ID:        row.ID,
		Type:      row.Type,
		URL:       row.Url,
		Trusted:   row.Trusted,
		Enabled:   row.Enabled,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (s *PluginStore) DeletePluginSource(ctx context.Context, id uuid.UUID) error {
	return s.q.DeletePluginSource(ctx, id)
}

// ── Plugin Policies ────────────────────────────────────────────

func (s *PluginStore) ListPluginPolicies(ctx context.Context) ([]domain.PluginPolicy, error) {
	rows, err := s.q.ListPluginPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list plugin policies: %w", err)
	}
	result := make([]domain.PluginPolicy, len(rows))
	for i, r := range rows {
		result[i] = domain.PluginPolicy{
			ID:        r.ID,
			Rule:      r.Rule,
			Pattern:   r.Pattern,
			CreatedAt: r.CreatedAt,
		}
		if r.Kind.Valid {
			result[i].Kind = r.Kind.String
		}
	}
	return result, nil
}

func (s *PluginStore) CreatePluginPolicy(ctx context.Context, policy domain.PluginPolicy) (*domain.PluginPolicy, error) {
	row, err := s.q.CreatePluginPolicy(ctx, gen.CreatePluginPolicyParams{
		Rule:    policy.Rule,
		Pattern: policy.Pattern,
		Kind:    textOrNull(policy.Kind),
	})
	if err != nil {
		return nil, fmt.Errorf("create plugin policy: %w", err)
	}
	result := domain.PluginPolicy{
		ID:        row.ID,
		Rule:      row.Rule,
		Pattern:   row.Pattern,
		CreatedAt: row.CreatedAt,
	}
	if row.Kind.Valid {
		result.Kind = row.Kind.String
	}
	return &result, nil
}

func (s *PluginStore) DeletePluginPolicy(ctx context.Context, id uuid.UUID) error {
	return s.q.DeletePluginPolicy(ctx, id)
}

// ── Row conversion ─────────────────────────────────────────────

func pluginRowToDomain(r gen.PluginCatalog) domain.PluginEntry {
	entry := domain.PluginEntry{
		ID:           r.ID,
		Name:         r.Name,
		Kind:         domain.PluginKind(r.Kind),
		Version:      r.Version,
		Status:       domain.PluginStatus(r.Status),
		Error:        nullableTextToString(r.Error),
		Descriptor:   r.Descriptor,
		Config:       r.Config,
		Addr:         r.Addr,
		Healthy:      r.Healthy,
		RegisteredAt: r.RegisteredAt,
		EnabledAt:    r.EnabledAt,
		UpdatedAt:    r.UpdatedAt,
	}
	return entry
}
