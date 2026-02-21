/** IdentityGroup represents a group from the identity provider. */
export interface IdentityGroup {
  id: string;
  name: string;
  path: string;
}

/** IdentityUser represents a full user record from the identity provider. */
export interface IdentityUser {
  id: string;
  email: string;
  display_name: string;
  first_name: string;
  last_name: string;
  enabled: boolean;
  created_at: string;
  groups: IdentityGroup[];
  attributes: Record<string, string>;
}

/** IdentityUserSummary is a lightweight user representation for autocomplete. */
export interface IdentityUserSummary {
  id: string;
  email: string;
  display_name: string;
}

/** IdentityCapabilities describes what the identity provider supports. */
export interface IdentityCapabilities {
  provider_name: string;
  can_create_users: boolean;
  can_update_users: boolean;
  can_delete_users: boolean;
  can_reset_password: boolean;
  can_manage_groups: boolean;
}

/** IdentityUserListResponse wraps a paginated list of users. */
export interface IdentityUserListResponse {
  users: IdentityUser[];
  total_count: number;
}

/** IdentityUserSearchResponse wraps lightweight user search results. */
export interface IdentityUserSearchResponse {
  users: IdentityUserSummary[];
}

/** IdentityGroupListResponse wraps the list of all provider groups. */
export interface IdentityGroupListResponse {
  groups: IdentityGroup[];
}
