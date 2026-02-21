/** Permission system types — Pro feature, stubbed for portal build. */

export type PrincipalType = "user" | "group" | "role";

export interface Grant {
  grant_id: string;
  principal_type: PrincipalType;
  principal_id: string;
  resource: string;
  verb: string;
  granted_by: string;
  created_at: string;
}

export interface GrantListResponse {
  grants: Grant[];
}

export interface CreateGrantRequest {
  principal_type: PrincipalType;
  principal_id: string;
  resource: string;
  verb: string;
}

export interface ResourceAccessEntry {
  principal_type: PrincipalType;
  principal_id: string;
  verb: string;
  source: string;
}

export interface ResourceAccessResponse {
  access: ResourceAccessEntry[];
}

export interface Verb {
  name: string;
  implies?: string[];
  description?: string;
}

export interface VerbListResponse {
  verbs: Verb[];
}

export interface Group {
  group_id: string;
  name: string;
  description?: string;
}

export interface GroupListResponse {
  groups: Group[];
}

export interface GroupMember {
  member_type: PrincipalType;
  member_id: string;
}

export interface GroupMemberListResponse {
  members: GroupMember[];
}
