export interface LineageNode {
  id: string;
  type: "pipeline" | "table" | "landing_zone";
  namespace: string;
  layer?: string;
  name: string;
  latest_run?: {
    id: string;
    status: string;
    started_at?: string;
    duration_ms?: number;
  };
  quality?: {
    total: number;
    passed: number;
    failed: number;
    warned: number;
  };
  table_stats?: {
    row_count: number;
    size_bytes: number;
  };
  landing_info?: {
    file_count: number;
  };
}

export interface LineageEdge {
  source: string;
  target: string;
  type: "ref" | "produces" | "landing_input";
}

export interface LineageGraph {
  nodes: LineageNode[];
  edges: LineageEdge[];
}
