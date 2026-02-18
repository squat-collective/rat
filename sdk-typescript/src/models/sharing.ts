export interface ShareAccess {
  id: string;
  resource_type: string;
  resource_id: string;
  granted_to: string;
  permission: "read" | "write" | "admin";
  granted_by: string;
  created_at: string;
}

export interface ShareListResponse {
  shares: ShareAccess[];
  total: number;
}

export interface ShareResourceRequest {
  resource_type: string;
  resource_id: string;
  granted_to: string;
  permission: "read" | "write" | "admin";
}

export interface TransferOwnershipRequest {
  resource_type: string;
  resource_id: string;
  new_owner: string;
}
