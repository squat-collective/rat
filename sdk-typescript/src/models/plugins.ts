export type PluginStatus = "registered" | "enabled" | "disabled" | "error";
export type PluginKind = "platform" | "runner" | "portal";

/** A Python entry point discovered by the runner container. */
export interface RunnerPlugin {
  name: string;         // entry point name ("soft_delete", "env_var")
  group: string;        // "rat.strategies", "rat.hooks", etc.
  version: string;      // package version
  package_name: string; // Python package name ("rat-plugin-soft-delete")
}

export interface PluginEntry {
  id: string;
  name: string;
  kind: PluginKind;
  version: string;
  status: PluginStatus;
  error?: string;
  descriptor?: Record<string, unknown>;
  config?: Record<string, unknown>;
  addr: string;
  healthy: boolean;
  registered_at: string;
  enabled_at?: string;
  updated_at: string;
}

export interface PluginSource {
  id: string;
  type: string;
  url: string;
  trusted: boolean;
  enabled: boolean;
  created_at: string;
}

export interface CreatePluginSourceRequest {
  type: string;
  url: string;
  trusted?: boolean;
  enabled?: boolean;
}

export interface PluginPolicy {
  id: string;
  rule: "allow" | "deny";
  pattern: string;
  kind?: string;
  created_at: string;
}

export interface CreatePluginPolicyRequest {
  rule: "allow" | "deny";
  pattern: string;
  kind?: string;
}
