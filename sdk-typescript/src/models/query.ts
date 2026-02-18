export interface QueryColumn {
  name: string;
  type: string;
  description?: string;
}

export interface QueryResult {
  columns: QueryColumn[];
  rows: Record<string, unknown>[];
  total_rows: number;
  duration_ms: number;
}

export interface QueryRequest {
  sql: string;
  namespace?: string;
  limit?: number;
}
