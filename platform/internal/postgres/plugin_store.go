package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
		result[i] = listRowToDomain(r)
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
	entry := getRowToDomain(row)
	return &entry, nil
}

func (s *PluginStore) UpsertPlugin(ctx context.Context, entry domain.PluginEntry) (*domain.PluginEntry, error) {
	descriptor := entry.Descriptor
	if descriptor == nil {
		descriptor = json.RawMessage("{}")
	}
	// Leave config as nil when the caller didn't supply one so the SQL's
	// COALESCE branch keeps whatever the plugin had persisted. The first
	// INSERT for a brand-new plugin name still works because the column
	// has a DEFAULT '{}' clause in the schema (migration 016).
	row, err := s.q.UpsertPlugin(ctx, gen.UpsertPluginParams{
		Name:       entry.Name,
		Kind:       string(entry.Kind),
		Version:    entry.Version,
		Status:     string(entry.Status),
		Error:      textOrNull(entry.Error),
		Descriptor: descriptor,
		Config:     entry.Config,
		Addr:       entry.Addr,
		Healthy:    entry.Healthy,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert plugin %s: %w", entry.Name, err)
	}
	result := upsertRowToDomain(row)
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

// UpdatePluginConfig writes new config for the named plugin.
//
// expectedVersion implements optimistic concurrency:
//   - nil → legacy last-write-wins (no version check); still bumps config_version.
//   - non-nil → only writes if the current config_version matches; otherwise
//     returns domain.ErrConfigVersionMismatch along with the current entry so
//     the caller can surface the latest config_version in the response.
//
// Returns (nil, nil) when the plugin does not exist (callers map to 404).
func (s *PluginStore) UpdatePluginConfig(ctx context.Context, name string, config json.RawMessage, expectedVersion *int64) (*domain.PluginEntry, error) {
	params := gen.UpdatePluginConfigParams{
		Name:   name,
		Config: config,
	}
	if expectedVersion != nil {
		params.ExpectedVersion = pgtype.Int8{Int64: *expectedVersion, Valid: true}
	}

	row, err := s.q.UpdatePluginConfig(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Neither branch of the CTE produced a row → the plugin does not
			// exist. Surface as not-found so the handler returns 404.
			return nil, nil
		}
		return nil, fmt.Errorf("update plugin config %s: %w", name, err)
	}

	result := updateRowToDomain(row)
	if !row.WasUpdated {
		// The UPDATE branch of the CTE did not fire because the supplied
		// expectedVersion did not match. Return the current entry alongside
		// the sentinel error so the caller can echo the live version back to
		// the client in the 409 response.
		return &result, domain.ErrConfigVersionMismatch
	}
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
//
// sqlc generates a distinct row type per query when the SELECT list adds an
// extra column (UpdatePluginConfig adds was_updated, so it can't share the
// row shape used by List/Get/Upsert). The shape is otherwise identical, so
// we centralise the conversion in one helper that takes individual fields
// and provide thin per-row adapters.

func pluginEntryFromFields(
	id uuid.UUID,
	name, kind, version, status string,
	errText pgtype.Text,
	descriptor, config []byte,
	configVersion int64,
	addr string,
	healthy bool,
	registeredAt time.Time,
	enabledAt *time.Time,
	updatedAt time.Time,
) domain.PluginEntry {
	return domain.PluginEntry{
		ID:            id,
		Name:          name,
		Kind:          domain.PluginKind(kind),
		Version:       version,
		Status:        domain.PluginStatus(status),
		Error:         nullableTextToString(errText),
		Descriptor:    descriptor,
		Config:        config,
		ConfigVersion: configVersion,
		Addr:          addr,
		Healthy:       healthy,
		RegisteredAt:  registeredAt,
		EnabledAt:     enabledAt,
		UpdatedAt:     updatedAt,
	}
}

func listRowToDomain(r gen.ListPluginsRow) domain.PluginEntry {
	return pluginEntryFromFields(
		r.ID, r.Name, r.Kind, r.Version, r.Status, r.Error,
		r.Descriptor, r.Config, r.ConfigVersion,
		r.Addr, r.Healthy, r.RegisteredAt, r.EnabledAt, r.UpdatedAt,
	)
}

func getRowToDomain(r gen.GetPluginByNameRow) domain.PluginEntry {
	return pluginEntryFromFields(
		r.ID, r.Name, r.Kind, r.Version, r.Status, r.Error,
		r.Descriptor, r.Config, r.ConfigVersion,
		r.Addr, r.Healthy, r.RegisteredAt, r.EnabledAt, r.UpdatedAt,
	)
}

func upsertRowToDomain(r gen.UpsertPluginRow) domain.PluginEntry {
	return pluginEntryFromFields(
		r.ID, r.Name, r.Kind, r.Version, r.Status, r.Error,
		r.Descriptor, r.Config, r.ConfigVersion,
		r.Addr, r.Healthy, r.RegisteredAt, r.EnabledAt, r.UpdatedAt,
	)
}

func updateRowToDomain(r gen.UpdatePluginConfigRow) domain.PluginEntry {
	return pluginEntryFromFields(
		r.ID, r.Name, r.Kind, r.Version, r.Status, r.Error,
		r.Descriptor, r.Config, r.ConfigVersion,
		r.Addr, r.Healthy, r.RegisteredAt, r.EnabledAt, r.UpdatedAt,
	)
}
