import type {
  VerbListResponse,
  GrantListResponse,
  CreateGrantRequest,
  CreateGrantResponse,
  RevokeGrantResponse,
  GroupListResponse,
  CreateGroupResponse,
  DeleteGroupResponse,
  GroupMembersResponse,
  RemoveGroupMemberResponse,
  CheckAccessRequest,
  CheckAccessResponse,
  ResourceAccessResponse,
  PrincipalAccessResponse,
  RemoveResourceResponse,
  PrincipalType,
} from "../models/permission";
import { BaseResource } from "./base";

/** Filters for listing grants. */
export interface GrantFilters {
  resource?: string;
  principal_type?: PrincipalType;
  principal_id?: string;
}

export class PermissionsResource extends BaseResource {
  // ── Verbs ──────────────────────────────────────────────────────────────

  /** List all registered verbs and their implications. */
  async listVerbs(): Promise<VerbListResponse> {
    return this.transport.request<VerbListResponse>(
      "GET",
      "/api/v1/permissions/verbs",
    );
  }

  /** Register or update a verb with its implications. */
  async registerVerb(
    name: string,
    implies?: string[],
    description?: string,
  ): Promise<void> {
    await this.transport.request(
      "POST",
      "/api/v1/permissions/verbs",
      { json: { name, implies, description } },
    );
  }

  // ── Grants ─────────────────────────────────────────────────────────────

  /** List grants, optionally filtered by resource and/or principal. */
  async listGrants(filters?: GrantFilters): Promise<GrantListResponse> {
    const params = new URLSearchParams();
    if (filters?.resource) params.set("resource", filters.resource);
    if (filters?.principal_type) params.set("principal_type", filters.principal_type);
    if (filters?.principal_id) params.set("principal_id", filters.principal_id);
    const qs = params.toString();
    return this.transport.request<GrantListResponse>(
      "GET",
      `/api/v1/permissions/grants${qs ? `?${qs}` : ""}`,
    );
  }

  /** Create a new grant. */
  async createGrant(req: CreateGrantRequest): Promise<CreateGrantResponse> {
    return this.transport.request<CreateGrantResponse>(
      "POST",
      "/api/v1/permissions/grants",
      { json: req },
    );
  }

  /** Revoke a grant by ID. */
  async revokeGrant(grantId: string): Promise<RevokeGrantResponse> {
    return this.transport.request<RevokeGrantResponse>(
      "DELETE",
      `/api/v1/permissions/grants/${grantId}`,
    );
  }

  // ── Groups ─────────────────────────────────────────────────────────────

  /** List all engine-managed groups. */
  async listGroups(): Promise<GroupListResponse> {
    return this.transport.request<GroupListResponse>(
      "GET",
      "/api/v1/permissions/groups",
    );
  }

  /** Create a new group. */
  async createGroup(
    name: string,
    description?: string,
  ): Promise<CreateGroupResponse> {
    return this.transport.request<CreateGroupResponse>(
      "POST",
      "/api/v1/permissions/groups",
      { json: { name, description } },
    );
  }

  /** Delete a group and all its memberships and grants. */
  async deleteGroup(groupId: string): Promise<DeleteGroupResponse> {
    return this.transport.request<DeleteGroupResponse>(
      "DELETE",
      `/api/v1/permissions/groups/${groupId}`,
    );
  }

  /** List members of a group. */
  async listGroupMembers(groupId: string): Promise<GroupMembersResponse> {
    return this.transport.request<GroupMembersResponse>(
      "GET",
      `/api/v1/permissions/groups/${groupId}/members`,
    );
  }

  /** Add a member (user or group) to a group. */
  async addGroupMember(
    groupId: string,
    memberType: PrincipalType,
    memberId: string,
  ): Promise<void> {
    await this.transport.request(
      "POST",
      `/api/v1/permissions/groups/${groupId}/members`,
      { json: { member_type: memberType, member_id: memberId } },
    );
  }

  /** Remove a member from a group. */
  async removeGroupMember(
    groupId: string,
    memberType: PrincipalType,
    memberId: string,
  ): Promise<RemoveGroupMemberResponse> {
    return this.transport.request<RemoveGroupMemberResponse>(
      "DELETE",
      `/api/v1/permissions/groups/${groupId}/members`,
      { json: { member_type: memberType, member_id: memberId } },
    );
  }

  // ── Access Checks ──────────────────────────────────────────────────────

  /** Check if a user can perform a verb on a resource. */
  async checkAccess(req: CheckAccessRequest): Promise<CheckAccessResponse> {
    return this.transport.request<CheckAccessResponse>(
      "POST",
      "/api/v1/permissions/check",
      { json: req },
    );
  }

  /** List all effective access on a resource (who can do what). */
  async listResourceAccess(resource: string): Promise<ResourceAccessResponse> {
    return this.transport.request<ResourceAccessResponse>(
      "GET",
      `/api/v1/permissions/access/resource?resource=${encodeURIComponent(resource)}`,
    );
  }

  /** List all resources a principal can access (what can I do). */
  async listPrincipalAccess(
    userId: string,
    resourcePrefix?: string,
  ): Promise<PrincipalAccessResponse> {
    const params = new URLSearchParams({ user_id: userId });
    if (resourcePrefix) params.set("resource_prefix", resourcePrefix);
    return this.transport.request<PrincipalAccessResponse>(
      "GET",
      `/api/v1/permissions/access/principal?${params.toString()}`,
    );
  }

  // ── Resource Cleanup ───────────────────────────────────────────────────

  /** Remove a resource path and its grants. */
  async removeResource(
    resource: string,
    cascade?: boolean,
  ): Promise<RemoveResourceResponse> {
    return this.transport.request<RemoveResourceResponse>(
      "DELETE",
      "/api/v1/permissions/resources",
      { json: { resource, cascade } },
    );
  }
}
