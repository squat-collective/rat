// Package domain defines the core business types shared across ratd.
// These types represent the platform's data model — not HTTP or gRPC specifics.
//
// P10-37: Design note on JSON tags in domain types.
// Domain types carry json tags because they are directly serialized in API responses.
// This is intentional: Go's stdlib encoding/json uses struct tags for field mapping,
// and having separate API response types for every domain model would add excessive
// boilerplate without measurable benefit.
//
// When the API shape diverges from the domain type (e.g., computed fields, omitted
// internal fields), define a response struct in the api package instead. Examples:
//   - PipelineRetentionResponse in retention.go (combines system + per-pipeline config)
//   - ZoneLifecycleResponse in retention.go (subset of LandingZone fields)
//   - PipelineListResponse in pipelines.go (adds latest_run, test_count)
//
// Internal-only fields are tagged with `json:"-"` to prevent accidental exposure:
//   - Pipeline.DeletedAt (soft-delete timestamp, DB-only)
//   - Run.S3Overrides (transient cloud credentials, never persisted or serialized)
package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrAlreadyExists indicates a create operation conflicted with an existing resource.
var ErrAlreadyExists = errors.New("resource already exists")

// Layer represents a medallion architecture layer.
type Layer string

const (
	LayerBronze Layer = "bronze"
	LayerSilver Layer = "silver"
	LayerGold   Layer = "gold"
)

// ValidLayer checks if a string is a valid layer.
func ValidLayer(s string) bool {
	switch Layer(s) {
	case LayerBronze, LayerSilver, LayerGold:
		return true
	}
	return false
}

// Pipeline represents a data pipeline registered in the platform.
type Pipeline struct {
	ID                uuid.UUID         `json:"id"`
	Namespace         string            `json:"namespace"`
	Layer             Layer             `json:"layer"`
	Name              string            `json:"name"`
	Type              string            `json:"type"` // "sql" or "python"
	S3Path            string            `json:"s3_path"`
	Description       string            `json:"description"`
	Owner             *string           `json:"owner"`                        // nil for Community (single user)
	PublishedAt       *time.Time        `json:"published_at,omitempty"`
	PublishedVersions map[string]string `json:"published_versions,omitempty"` // file path → S3 version ID
	DraftDirty        bool              `json:"draft_dirty"`
	MaxVersions       int               `json:"max_versions"`
	RetentionConfig   json.RawMessage   `json:"retention_config,omitempty"` // per-pipeline overrides (null = system default)
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	DeletedAt         *time.Time        `json:"-"`
}

// PipelineVersion represents a published version snapshot of a pipeline.
type PipelineVersion struct {
	ID                uuid.UUID         `json:"id"`
	PipelineID        uuid.UUID         `json:"pipeline_id"`
	VersionNumber     int               `json:"version_number"`
	Message           string            `json:"message"`
	PublishedVersions map[string]string `json:"published_versions"`
	CreatedAt         time.Time         `json:"created_at"`
}

// RunStatus represents the state of a pipeline run.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSuccess   RunStatus = "success"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// Run represents a single pipeline execution.
type Run struct {
	ID          uuid.UUID  `json:"id"`
	PipelineID  uuid.UUID  `json:"pipeline_id"`
	Status      RunStatus  `json:"status"`
	Trigger     string     `json:"trigger"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	DurationMs  *int       `json:"duration_ms"`
	RowsWritten *int64     `json:"rows_written"`
	Error       *string    `json:"error"`
	LogsS3Path  *string    `json:"logs_s3_path"`
	CreatedAt   time.Time  `json:"created_at"`

	// S3Overrides holds per-run S3 credentials injected by the cloud plugin.
	// Transient — not persisted in Postgres. Passed to the executor on submit.
	S3Overrides map[string]string `json:"-"`
}

// Schedule represents a cron-based trigger for a pipeline.
type Schedule struct {
	ID         uuid.UUID  `json:"id"`
	PipelineID uuid.UUID  `json:"pipeline_id"`
	CronExpr   string     `json:"cron"`
	Enabled    bool       `json:"enabled"`
	LastRunID  *uuid.UUID `json:"last_run_id"`
	LastRunAt  *time.Time `json:"last_run_at"`
	NextRunAt  *time.Time `json:"next_run_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Namespace represents a logical grouping of pipelines, tables, and resources.
// Community edition has a single implicit "default" namespace.
type Namespace struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedBy   *string   `json:"created_by"` // nil for Community (single user)
	CreatedAt   time.Time `json:"created_at"`
}

// Features describes the active capabilities of the platform.
// Used by the portal to show/hide UI elements based on active plugins.
type Features struct {
	Edition    string                   `json:"edition"`
	Plugins    map[string]PluginFeature `json:"plugins"`
	Namespaces   bool                     `json:"namespaces"`
	MultiUser    bool                     `json:"multi_user"`
	LandingZones bool                     `json:"landing_zones"`
	License      *LicenseInfo             `json:"license,omitempty"`
}

// PluginFeature describes a single plugin's status.
type PluginFeature struct {
	Enabled bool   `json:"enabled"`
	Type    string `json:"type,omitempty"`
}

// LandingZone represents a standalone file drop area for raw data.
type LandingZone struct {
	ID                  uuid.UUID `json:"id"`
	Namespace           string    `json:"namespace"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	Owner               *string   `json:"owner,omitempty"`
	ExpectedSchema      string    `json:"expected_schema"`
	ProcessedMaxAgeDays *int      `json:"processed_max_age_days,omitempty"` // _processed/ file retention (nil = never auto-purge)
	AutoPurge           bool      `json:"auto_purge"`                       // enable automatic _processed/ cleanup
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// TableMetadata stores user-maintained documentation for Iceberg tables.
// Tables are dynamic (from Nessie/Iceberg), so metadata lives in Postgres separately.
type TableMetadata struct {
	ID                 uuid.UUID         `json:"id"`
	Namespace          string            `json:"namespace"`
	Layer              string            `json:"layer"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	Owner              *string           `json:"owner,omitempty"`
	ColumnDescriptions map[string]string `json:"column_descriptions"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

// LandingFile represents a file uploaded to a landing zone.
type LandingFile struct {
	ID          uuid.UUID `json:"id"`
	ZoneID      uuid.UUID `json:"zone_id"`
	Filename    string    `json:"filename"`
	S3Path      string    `json:"s3_path"`
	SizeBytes   int64     `json:"size_bytes"`
	ContentType string    `json:"content_type"`
	UploadedBy  *string   `json:"uploaded_by,omitempty"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

// TriggerType represents the type of pipeline trigger.
type TriggerType string

const (
	TriggerTypeLandingZoneUpload TriggerType = "landing_zone_upload"
	TriggerTypeCron              TriggerType = "cron"
	TriggerTypePipelineSuccess   TriggerType = "pipeline_success"
	TriggerTypeWebhook           TriggerType = "webhook"
	TriggerTypeFilePattern       TriggerType = "file_pattern"
	TriggerTypeCronDependency    TriggerType = "cron_dependency"
)

// ValidTriggerType returns true if s is a known trigger type.
func ValidTriggerType(s string) bool {
	switch TriggerType(s) {
	case TriggerTypeLandingZoneUpload, TriggerTypeCron, TriggerTypePipelineSuccess,
		TriggerTypeWebhook, TriggerTypeFilePattern, TriggerTypeCronDependency:
		return true
	}
	return false
}

// PipelineTrigger represents an event-driven trigger for a pipeline.
type PipelineTrigger struct {
	ID              uuid.UUID       `json:"id"`
	PipelineID      uuid.UUID       `json:"pipeline_id"`
	Type            TriggerType     `json:"type"`
	Config          json.RawMessage `json:"config"`
	Enabled         bool            `json:"enabled"`
	CooldownSeconds int             `json:"cooldown_seconds"`
	LastTriggeredAt *time.Time      `json:"last_triggered_at"`
	LastRunID       *uuid.UUID      `json:"last_run_id"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// AuditEntry represents a single audit log record.
type AuditEntry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// RetentionConfig holds system-wide data retention settings.
// Stored as JSONB in platform_settings under key "retention".
type RetentionConfig struct {
	RunsMaxPerPipeline            int `json:"runs_max_per_pipeline"`
	RunsMaxAgeDays                int `json:"runs_max_age_days"`
	LogsMaxAgeDays                int `json:"logs_max_age_days"`
	QualityResultsMaxPerTest      int `json:"quality_results_max_per_test"`
	SoftDeletePurgeDays           int `json:"soft_delete_purge_days"`
	StuckRunTimeoutMinutes        int `json:"stuck_run_timeout_minutes"`
	AuditLogMaxAgeDays            int `json:"audit_log_max_age_days"`
	NessieOrphanBranchMaxAgeHours int `json:"nessie_orphan_branch_max_age_hours"`
	ReaperIntervalMinutes         int `json:"reaper_interval_minutes"`
	IcebergSnapshotMaxAgeDays     int `json:"iceberg_snapshot_max_age_days"`
	IcebergOrphanFileMaxAgeDays   int `json:"iceberg_orphan_file_max_age_days"`
}

// DefaultRetentionConfig returns the default retention config matching Strategy Doc #22.
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		RunsMaxPerPipeline:            100,
		RunsMaxAgeDays:                90,
		LogsMaxAgeDays:                30,
		QualityResultsMaxPerTest:      100,
		SoftDeletePurgeDays:           30,
		StuckRunTimeoutMinutes:        30,
		AuditLogMaxAgeDays:            365,
		NessieOrphanBranchMaxAgeHours: 6,
		ReaperIntervalMinutes:         15,
		IcebergSnapshotMaxAgeDays:     7,
		IcebergOrphanFileMaxAgeDays:   3,
	}
}

// ReaperStatus tracks the last reaper run stats.
type ReaperStatus struct {
	LastRunAt      *time.Time `json:"last_run_at"`
	RunsPruned     int        `json:"runs_pruned"`
	LogsPruned     int        `json:"logs_pruned"`
	QualityPruned  int        `json:"quality_pruned"`
	PipelinesPurged int       `json:"pipelines_purged"`
	RunsFailed     int        `json:"runs_failed"`
	BranchesCleaned int       `json:"branches_cleaned"`
	LZFilesCleaned int        `json:"lz_files_cleaned"`
	AuditPruned    int        `json:"audit_pruned"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// FeatureFlags holds runtime-configurable feature toggles.
// Stored as JSONB in platform_settings under key "feature_flags".
// Community defaults enable all community features. Pro features default to false.
type FeatureFlags struct {
	PipelinePreview         bool `json:"pipeline_preview"`
	QualityTests            bool `json:"quality_tests"`
	LandingZones            bool `json:"landing_zones"`
	PipelineTriggers        bool `json:"pipeline_triggers"`
	PipelineVersions        bool `json:"pipeline_versions"`
	QueryEngine             bool `json:"query_engine"`
	AuditLog                bool `json:"audit_log"`
	NamespaceQuotas         bool `json:"namespace_quotas"`          // Pro only
	DistributedRateLimiting bool `json:"distributed_rate_limiting"` // Pro only
}

// DefaultFeatureFlags returns community defaults (all community features enabled).
func DefaultFeatureFlags() FeatureFlags {
	return FeatureFlags{
		PipelinePreview:         true,
		QualityTests:            true,
		LandingZones:            true,
		PipelineTriggers:        true,
		PipelineVersions:        true,
		QueryEngine:             true,
		AuditLog:                true,
		NamespaceQuotas:         false,
		DistributedRateLimiting: false,
	}
}

// IsEnabled checks if a named feature flag is enabled.
// Returns false for unknown flag names.
func (f FeatureFlags) IsEnabled(name string) bool {
	switch name {
	case "pipeline_preview":
		return f.PipelinePreview
	case "quality_tests":
		return f.QualityTests
	case "landing_zones":
		return f.LandingZones
	case "pipeline_triggers":
		return f.PipelineTriggers
	case "pipeline_versions":
		return f.PipelineVersions
	case "query_engine":
		return f.QueryEngine
	case "audit_log":
		return f.AuditLog
	case "namespace_quotas":
		return f.NamespaceQuotas
	case "distributed_rate_limiting":
		return f.DistributedRateLimiting
	default:
		return false
	}
}

// NamespaceQuota defines resource limits for a single namespace.
// Pro only — Community edition does not enforce quotas.
// Stored in Postgres keyed by namespace name. A zero value for any limit
// means "unlimited" (no enforcement for that resource type).
type NamespaceQuota struct {
	Namespace         string    `json:"namespace"`
	MaxPipelines      int       `json:"max_pipelines"`       // max pipeline count (0 = unlimited)
	MaxRunsPerDay     int       `json:"max_runs_per_day"`    // max pipeline runs per calendar day (0 = unlimited)
	MaxStorageBytes   int64     `json:"max_storage_bytes"`   // max S3 storage in bytes (0 = unlimited)
	MaxConcurrentRuns int       `json:"max_concurrent_runs"` // max simultaneous running pipelines (0 = unlimited)
	UpdatedAt         time.Time `json:"updated_at"`
}

// DefaultNamespaceQuota returns a quota with no limits (all zeros = unlimited).
func DefaultNamespaceQuota(namespace string) NamespaceQuota {
	return NamespaceQuota{Namespace: namespace}
}

// QuotaCheckResult holds the result of a quota enforcement check.
type QuotaCheckResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"` // human-readable denial reason
}

// LicenseInfo describes the license status for display.
type LicenseInfo struct {
	Valid     bool     `json:"valid"`
	Tier      string   `json:"tier,omitempty"`
	OrgID     string   `json:"org_id,omitempty"`
	Plugins   []string `json:"plugins,omitempty"`
	SeatLimit int      `json:"seat_limit,omitempty"`
	ExpiresAt *string  `json:"expires_at,omitempty"` // RFC3339
	Error     string   `json:"error,omitempty"`
}
