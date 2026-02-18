export interface Namespace {
  name: string;
  description: string;
  created_at: string;
}

export interface UpdateNamespaceRequest {
  description: string;
}

export interface NamespaceListResponse {
  namespaces: Namespace[];
  total: number;
}
