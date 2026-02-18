export type RunStatus =
  | "pending"
  | "running"
  | "success"
  | "failed"
  | "cancelled";

export interface Run {
  id: string;
  pipeline_id: string;
  status: RunStatus;
  trigger: string;
  started_at: string | null;
  finished_at: string | null;
  duration_ms: number | null;
  rows_written: number | null;
  error: string | null;
  logs_s3_path: string | null;
  created_at: string;
}

export interface RunListResponse {
  runs: Run[];
  total: number;
}

export interface CreateRunRequest {
  namespace: string;
  layer: string;
  pipeline: string;
  trigger?: string;
}

export interface CreateRunResponse {
  run_id: string;
  status: string;
}

export interface RunLog {
  timestamp: string;
  level: string;
  message: string;
}

export interface RunLogsResponse {
  logs: RunLog[];
  status: string;
}
