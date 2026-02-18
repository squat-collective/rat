export type TriggerType =
  | "landing_zone_upload"
  | "cron"
  | "pipeline_success"
  | "webhook"
  | "file_pattern"
  | "cron_dependency";

export interface PipelineTrigger {
  id: string;
  pipeline_id: string;
  type: TriggerType;
  config: Record<string, unknown>;
  enabled: boolean;
  cooldown_seconds: number;
  last_triggered_at: string | null;
  last_run_id: string | null;
  created_at: string;
  updated_at: string;
  /** The webhook endpoint URL (POST). Token is NOT in the URL — pass it via header. */
  webhook_url?: string;
  /** The secret token to send via X-Webhook-Token header or Authorization: Bearer <token>. */
  webhook_token?: string;
}

export interface TriggerListResponse {
  triggers: PipelineTrigger[];
  total: number;
}

export interface CreateTriggerRequest {
  type: TriggerType;
  config: Record<string, unknown>;
  enabled?: boolean;
  cooldown_seconds?: number;
}

export interface UpdateTriggerRequest {
  config?: Record<string, unknown>;
  enabled?: boolean;
  cooldown_seconds?: number;
}

export interface LandingZoneUploadConfig {
  namespace: string;
  zone_name: string;
}

export interface CronConfig {
  cron_expr: string;
}

export interface PipelineSuccessConfig {
  namespace: string;
  layer: string;
  pipeline: string;
}

export interface WebhookConfig {
  token: string;
}

export interface FilePatternConfig {
  namespace: string;
  zone_name: string;
  pattern: string;
}

export interface CronDependencyConfig {
  cron_expr: string;
  dependencies: string[];
}
