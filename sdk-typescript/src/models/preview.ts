export interface PreviewRequest {
  limit?: number;
  sample_files?: string[];
  code?: string;
}

export interface PreviewResponse {
  columns: PreviewColumn[];
  rows: Record<string, unknown>[];
  total_row_count: number;
  phases: PhaseProfile[];
  explain_output: string;
  memory_peak_bytes: number;
  logs: PreviewLogEntry[];
  error?: string;
  warnings: string[];
}

export interface PreviewColumn {
  name: string;
  type: string;
}

export interface PhaseProfile {
  name: string;
  duration_ms: number;
  metadata?: Record<string, string>;
}

export interface PreviewLogEntry {
  timestamp: string;
  level: string;
  message: string;
}
