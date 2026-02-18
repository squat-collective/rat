// Package quota provides per-namespace resource quota enforcement.
//
// Community edition uses the NoopEnforcer which always allows operations.
// Pro edition provides a real enforcer backed by Postgres quota records
// and run-count tracking.
//
// Quota enforcement is checked at API boundaries before resource-creating
// operations (pipeline creation, run submission) to provide fast feedback.
package quota

import (
	"context"
	"log/slog"

	"github.com/rat-data/rat/platform/internal/domain"
)

// Enforcer checks whether a resource operation is allowed by the namespace's quota.
// Implementations must be safe for concurrent use.
type Enforcer interface {
	// CheckPipelineCreate checks if creating a new pipeline would exceed the
	// namespace's max_pipelines quota. Returns allowed=true if no quota is set
	// or the limit has not been reached.
	CheckPipelineCreate(ctx context.Context, namespace string) (*domain.QuotaCheckResult, error)

	// CheckRunSubmit checks if submitting a new run would exceed the namespace's
	// max_runs_per_day or max_concurrent_runs quota.
	CheckRunSubmit(ctx context.Context, namespace string) (*domain.QuotaCheckResult, error)

	// CheckStorageUsage checks if the namespace's S3 storage usage is within
	// the max_storage_bytes quota. Returns allowed=true if no quota is set
	// or current usage is below the limit.
	CheckStorageUsage(ctx context.Context, namespace string, additionalBytes int64) (*domain.QuotaCheckResult, error)

	// GetQuota returns the current quota configuration for a namespace.
	// Returns a zero-valued quota (all unlimited) if none is configured.
	GetQuota(ctx context.Context, namespace string) (*domain.NamespaceQuota, error)

	// SetQuota creates or updates the quota for a namespace.
	// Pro only — Community edition's NoopEnforcer returns an error.
	SetQuota(ctx context.Context, quota *domain.NamespaceQuota) error
}

// QuotaStore is the persistence interface for quota data.
// Implemented by Postgres store (Pro) — Community edition does not need this.
type QuotaStore interface {
	GetQuota(ctx context.Context, namespace string) (*domain.NamespaceQuota, error)
	SetQuota(ctx context.Context, quota *domain.NamespaceQuota) error
	CountPipelines(ctx context.Context, namespace string) (int, error)
	CountRunsToday(ctx context.Context, namespace string) (int, error)
	CountActiveRuns(ctx context.Context, namespace string) (int, error)
}

// NoopEnforcer always allows operations. Used in Community edition
// where namespace quotas are not available.
type NoopEnforcer struct{}

// NewNoopEnforcer creates a no-op enforcer for Community edition.
func NewNoopEnforcer() *NoopEnforcer {
	return &NoopEnforcer{}
}

func (n *NoopEnforcer) CheckPipelineCreate(_ context.Context, _ string) (*domain.QuotaCheckResult, error) {
	return &domain.QuotaCheckResult{Allowed: true}, nil
}

func (n *NoopEnforcer) CheckRunSubmit(_ context.Context, _ string) (*domain.QuotaCheckResult, error) {
	return &domain.QuotaCheckResult{Allowed: true}, nil
}

func (n *NoopEnforcer) CheckStorageUsage(_ context.Context, _ string, _ int64) (*domain.QuotaCheckResult, error) {
	return &domain.QuotaCheckResult{Allowed: true}, nil
}

func (n *NoopEnforcer) GetQuota(_ context.Context, namespace string) (*domain.NamespaceQuota, error) {
	q := domain.DefaultNamespaceQuota(namespace)
	return &q, nil
}

func (n *NoopEnforcer) SetQuota(_ context.Context, _ *domain.NamespaceQuota) error {
	slog.Warn("quota.SetQuota called on NoopEnforcer — namespace quotas require Pro edition")
	return nil
}
