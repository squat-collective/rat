import type { QueryColumn } from "./query";

export interface TableInfo {
  namespace: string;
  layer: string;
  name: string;
  row_count: number;
  size_bytes: number;
  description?: string;
}

export interface TableDetail extends TableInfo {
  columns: QueryColumn[];
  owner?: string | null;
}

export interface UpdateTableMetadataRequest {
  description?: string;
  owner?: string | null;
  column_descriptions?: Record<string, string>;
}

export interface TableListResponse {
  tables: TableInfo[];
  total: number;
}

export interface SchemaEntry {
  namespace: string;
  layer: string;
  name: string;
  columns: QueryColumn[];
}

export interface SchemaResponse {
  tables: SchemaEntry[];
}
