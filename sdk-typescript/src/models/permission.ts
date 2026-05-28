/** PrincipalType identifies the kind of principal in a grant or group membership. */
export type PrincipalType = "user" | "group" | "role";

/** Grant represents a relation tuple: (principal, verb, resource). */
export interface Grant {
  grant_id: string;
  principal_type: PrincipalType;
  principal_id: string;
  resource: string;
  verb: string;
  granted_by: string;
  created_at: string;
}

/** GrantListResponse wraps the list of matching grants. */
export interface GrantListResponse {
  grants: Grant[];
}

/** CreateGrantRequest is the body for creating a new grant. */
export interface CreateGrantRequest {
  principal_type: PrincipalType;
  principal_id: string;
  resource: string;
  verb: string;
}

/** CreateGrantResponse wraps the created grant. */
export interface CreateGrantResponse {
  grant: Grant;
}

/** VerbDefinition defines a verb and which other verbs it implies. */
export interface VerbDefinition {
  name: string;
  implies: string[];
  description?: string;
}

/** VerbListResponse wraps the list of all registered verbs. */
export interface VerbListResponse {
  verbs: VerbDefinition[];
}

/** GroupInfo describes an engine-managed group. */
export interface GroupInfo {
  group_id: string;
  name: string;
  description: string;
  created_at: string;
}

/** GroupListResponse wraps the list of all groups. */
export interface GroupListResponse {
  groups: GroupInfo[];
}

/** GroupMember describes a member of a group. */
export interface GroupMember {
  member_type: PrincipalType;
  member_id: string;
  added_at: string;
}

/** GroupMembersResponse wraps the list of group members. */
export interface GroupMembersResponse {
  members: GroupMember[];
}

/** EffectiveAccess describes a principal's effective access on a resource. */
export interface EffectiveAccess {
  principal_type: PrincipalType;
  principal_id: string;
  resource: string;
  verb: string;
  source: string;
}

/** ResourceAccessResponse wraps effective access entries for a resource. */
export interface ResourceAccessResponse {
  access: EffectiveAccess[];
}

/** PrincipalAccessResponse wraps effective access entries for a principal. */
export interface PrincipalAccessResponse {
  access: EffectiveAccess[];
}

/** CheckAccessRequest is the body for checking access. */
export interface CheckAccessRequest {
  user_id: string;
  groups?: string[];
  resource: string;
  verb: string;
}

/** CheckAccessResponse indicates whether access is allowed. */
export interface CheckAccessResponse {
  allowed: boolean;
  reason?: string;
}

/** RemoveResourceResponse confirms resource removal. */
export interface RemoveResourceResponse {
  resources_removed: number;
  grants_removed: number;
}

/** RevokeGrantResponse confirms grant revocation. */
export interface RevokeGrantResponse {
  revoked: boolean;
}

/** CreateGroupResponse wraps the created group. */
export interface CreateGroupResponse {
  group: GroupInfo;
}

/** DeleteGroupResponse confirms group deletion. */
export interface DeleteGroupResponse {
  deleted: boolean;
}

/** RemoveGroupMemberResponse confirms member removal. */
export interface RemoveGroupMemberResponse {
  removed: boolean;
}
