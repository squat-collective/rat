export type Layer = "bronze" | "silver" | "gold";

export type MergeStrategy = "full_refresh" | "incremental" | "append_only" | "delete_insert" | "scd2" | "snapshot";

export interface PipelineConfig {
  description?: string;
  materialized?: "table" | "view";
  unique_key?: string[];
  merge_strategy?: MergeStrategy;
  watermark_column?: string;
  archive_landing_zones?: boolean;
  partition_column?: string;
  scd_valid_from?: string;
  scd_valid_to?: string;
}

export interface Pipeline {
  id: string;
  namespace: string;
  layer: Layer;
  name: string;
  type: string;
  s3_path: string;
  description: string;
  owner: string | null;
  published_at?: string;
  published_versions?: Record<string, string>;
  draft_dirty: boolean;
  max_versions: number;
  created_at: string;
  updated_at: string;
}

export interface PipelineVersion {
  id: string;
  pipeline_id: string;
  version_number: number;
  message: string;
  published_versions: Record<string, string>;
  created_at: string;
}

export interface PipelineVersionListResponse {
  versions: PipelineVersion[];
  total: number;
}

export interface PublishResponse {
  status: string;
  version: number;
  message: string;
  versions: Record<string, string>;
}

export interface RollbackRequest {
  version: number;
  message?: string;
}

export interface RollbackResponse {
  status: string;
  from_version: number;
  new_version: number;
  message: string;
}

export interface PipelineListResponse {
  pipelines: Pipeline[];
  total: number;
}

export interface CreatePipelineRequest {
  namespace: string;
  layer: Layer;
  name: string;
  type: string;
  source?: string;
  unique_key?: string;
  description?: string;
}

export interface UpdatePipelineRequest {
  description?: string;
  type?: string;
  owner?: string;
}

export interface CreatePipelineResponse {
  namespace: string;
  layer: string;
  name: string;
  s3_path: string;
  files_created: string[];
}
