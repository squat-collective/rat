package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rat-data/rat/platform/internal/domain"
)

// SettingsStore implements api.SettingsStore backed by Postgres.
type SettingsStore struct {
	pool *pgxpool.Pool
}

// NewSettingsStore creates a SettingsStore backed by the given pool.
func NewSettingsStore(pool *pgxpool.Pool) *SettingsStore {
	return &SettingsStore{pool: pool}
}

// GetSetting returns the JSONB value for a given key from platform_settings.
func (s *SettingsStore) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var value json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM platform_settings WHERE key = $1`, key,
	).Scan(&value)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("setting %q not found", key)
		}
		return nil, fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

// PutSetting upserts a JSONB value for a given key in platform_settings.
func (s *SettingsStore) PutSetting(ctx context.Context, key string, value json.RawMessage) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO platform_settings (key, value, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("put setting %q: %w", key, err)
	}
	return nil
}

// GetReaperStatus returns the singleton reaper status row.
func (s *SettingsStore) GetReaperStatus(ctx context.Context) (*domain.ReaperStatus, error) {
	var (
		lastRunAt      *time.Time
		runsPruned     int
		logsPruned     int
		qualityPruned  int
		pipelinesPurged int
		runsFailed     int
		branchesCleaned int
		lzFilesCleaned int
		auditPruned    int
		updatedAt      time.Time
	)

	err := s.pool.QueryRow(ctx,
		`SELECT last_run_at, runs_pruned, logs_pruned, quality_pruned, pipelines_purged,
		        runs_failed, branches_cleaned, lz_files_cleaned, audit_pruned, updated_at
		 FROM reaper_status WHERE id = 1`,
	).Scan(&lastRunAt, &runsPruned, &logsPruned, &qualityPruned, &pipelinesPurged,
		&runsFailed, &branchesCleaned, &lzFilesCleaned, &auditPruned, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("get reaper status: %w", err)
	}

	return &domain.ReaperStatus{
		LastRunAt:       lastRunAt,
		RunsPruned:      runsPruned,
		LogsPruned:      logsPruned,
		QualityPruned:   qualityPruned,
		PipelinesPurged: pipelinesPurged,
		RunsFailed:      runsFailed,
		BranchesCleaned: branchesCleaned,
		LZFilesCleaned:  lzFilesCleaned,
		AuditPruned:     auditPruned,
		UpdatedAt:       updatedAt,
	}, nil
}

// GetFeatureFlags returns the current feature flags from platform_settings.
// Returns community defaults if the key doesn't exist yet.
func (s *SettingsStore) GetFeatureFlags(ctx context.Context) (*domain.FeatureFlags, error) {
	raw, err := s.GetSetting(ctx, "feature_flags")
	if err != nil {
		// Key not found — return defaults.
		defaults := domain.DefaultFeatureFlags()
		return &defaults, nil
	}

	var flags domain.FeatureFlags
	if err := json.Unmarshal(raw, &flags); err != nil {
		return nil, fmt.Errorf("unmarshal feature flags: %w", err)
	}
	return &flags, nil
}

// PutFeatureFlags writes the feature flags to platform_settings.
func (s *SettingsStore) PutFeatureFlags(ctx context.Context, flags *domain.FeatureFlags) error {
	raw, err := json.Marshal(flags)
	if err != nil {
		return fmt.Errorf("marshal feature flags: %w", err)
	}
	return s.PutSetting(ctx, "feature_flags", raw)
}

// UpdateReaperStatus updates the singleton reaper status row with the latest run stats.
func (s *SettingsStore) UpdateReaperStatus(ctx context.Context, status *domain.ReaperStatus) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE reaper_status SET
			last_run_at = NOW(),
			runs_pruned = $1,
			logs_pruned = $2,
			quality_pruned = $3,
			pipelines_purged = $4,
			runs_failed = $5,
			branches_cleaned = $6,
			lz_files_cleaned = $7,
			audit_pruned = $8,
			updated_at = NOW()
		 WHERE id = 1`,
		status.RunsPruned, status.LogsPruned, status.QualityPruned, status.PipelinesPurged,
		status.RunsFailed, status.BranchesCleaned, status.LZFilesCleaned, status.AuditPruned,
	)
	if err != nil {
		return fmt.Errorf("update reaper status: %w", err)
	}
	return nil
}
