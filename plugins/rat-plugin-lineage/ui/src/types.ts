// Mirrors the JSON shape the Go backend returns. Kept here so the
// plugin UI doesn't depend on @squat-collective/rat-client (which
// the old portal page imported). Same field names, same semantics.

export interface Namespace {
  name: string;
}

export interface RunSummary {
  id: string;
  status: string;
  started_at?: string;
  duration_ms?: number;
}

export interface QualitySummary {
  total: number;
  passed?: number;
  failed?: number;
  warned?: number;
}

export interface LineageTableStats {
  row_count: number;
  size_bytes: number;
}

export interface LandingInfo {
  file_count: number;
}

export interface LineageNode {
  id: string;
  type: "pipeline" | "table" | "landing_zone";
  namespace: string;
  layer?: string;
  name: string;
  latest_run?: RunSummary;
  quality?: QualitySummary;
  table_stats?: LineageTableStats;
  landing_info?: LandingInfo;
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
