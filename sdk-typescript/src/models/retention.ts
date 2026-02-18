export interface RetentionConfig {
  runs_max_per_pipeline: number;
  runs_max_age_days: number;
  logs_max_age_days: number;
  quality_results_max_per_test: number;
  soft_delete_purge_days: number;
  stuck_run_timeout_minutes: number;
  audit_log_max_age_days: number;
  nessie_orphan_branch_max_age_hours: number;
  reaper_interval_minutes: number;
  iceberg_snapshot_max_age_days: number;
  iceberg_orphan_file_max_age_days: number;
}

export interface RetentionConfigResponse {
  config: RetentionConfig;
}

export interface ReaperStatus {
  last_run_at: string | null;
  runs_pruned: number;
  logs_pruned: number;
  quality_pruned: number;
  pipelines_purged: number;
  runs_failed: number;
  branches_cleaned: number;
  lz_files_cleaned: number;
  audit_pruned: number;
  updated_at: string;
}

export interface PipelineRetentionResponse {
  system: RetentionConfig;
  overrides: Partial<RetentionConfig> | null;
  effective: RetentionConfig;
}

export interface ZoneLifecycleResponse {
  processed_max_age_days: number | null;
  auto_purge: boolean;
}

export interface ZoneLifecycleRequest {
  processed_max_age_days?: number;
  auto_purge?: boolean;
}
