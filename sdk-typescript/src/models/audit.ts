export interface AuditEntry {
  id: string;
  timestamp: string;
  user: string;
  action: string;
  resource_type: string;
  resource_id: string;
  details: Record<string, unknown>;
}

export interface AuditListResponse {
  entries: AuditEntry[];
  total: number;
}
