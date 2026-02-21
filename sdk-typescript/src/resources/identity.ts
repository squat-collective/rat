import type {
  IdentityCapabilities,
  IdentityUserListResponse,
  IdentityUser,
  IdentityUserSearchResponse,
  IdentityGroupListResponse,
} from "../models/identity";
import { BaseResource } from "./base";

/** Filters for listing identity users. */
export interface IdentityUserFilters {
  search?: string;
  limit?: number;
  offset?: number;
}

export class IdentityResource extends BaseResource {
  /** Get capabilities of the identity provider. */
  async getCapabilities(): Promise<IdentityCapabilities> {
    return this.transport.request<IdentityCapabilities>(
      "GET",
      "/api/v1/identity/capabilities",
    );
  }

  /** List users with optional search and pagination. */
  async listUsers(filters?: IdentityUserFilters): Promise<IdentityUserListResponse> {
    const params = new URLSearchParams();
    if (filters?.search) params.set("search", filters.search);
    if (filters?.limit != null) params.set("limit", String(filters.limit));
    if (filters?.offset != null) params.set("offset", String(filters.offset));
    const qs = params.toString();
    return this.transport.request<IdentityUserListResponse>(
      "GET",
      `/api/v1/identity/users${qs ? `?${qs}` : ""}`,
    );
  }

  /** Get a single user by ID. */
  async getUser(userId: string): Promise<IdentityUser> {
    const resp = await this.transport.request<{ user: IdentityUser }>(
      "GET",
      `/api/v1/identity/users/${userId}`,
    );
    return resp.user;
  }

  /** Search users for autocomplete (lightweight, no group lookup). */
  async searchUsers(query: string, limit?: number): Promise<IdentityUserSearchResponse> {
    const params = new URLSearchParams({ q: query });
    if (limit != null) params.set("limit", String(limit));
    return this.transport.request<IdentityUserSearchResponse>(
      "GET",
      `/api/v1/identity/users/search?${params.toString()}`,
    );
  }

  /** List all groups from the identity provider. */
  async listGroups(): Promise<IdentityGroupListResponse> {
    return this.transport.request<IdentityGroupListResponse>(
      "GET",
      "/api/v1/identity/groups",
    );
  }
}
