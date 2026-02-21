import type {
  PluginEntry,
  PluginSource,
  PluginPolicy,
  CreatePluginSourceRequest,
  CreatePluginPolicyRequest,
} from "../models/plugins";
import { BaseResource } from "./base";

export class PluginsResource extends BaseResource {
  /** List all registered plugins, optionally filtered by status and/or kind. */
  async list(params?: {
    status?: string;
    kind?: string;
  }): Promise<PluginEntry[]> {
    const query = new URLSearchParams();
    if (params?.status) query.set("status", params.status);
    if (params?.kind) query.set("kind", params.kind);
    const qs = query.toString();
    return this.transport.request<PluginEntry[]>(
      "GET",
      `/api/v1/plugins${qs ? `?${qs}` : ""}`,
    );
  }

  /** Get a single plugin by name. */
  async get(name: string): Promise<PluginEntry> {
    return this.transport.request<PluginEntry>(
      "GET",
      `/api/v1/plugins/${name}`,
    );
  }

  /** Enable a plugin. */
  async enable(name: string): Promise<{ status: string; name: string }> {
    return this.transport.request(
      "PUT",
      `/api/v1/plugins/${name}/enable`,
    );
  }

  /** Disable a plugin. */
  async disable(name: string): Promise<{ status: string; name: string }> {
    return this.transport.request(
      "PUT",
      `/api/v1/plugins/${name}/disable`,
    );
  }

  /** Update a plugin's configuration. */
  async updateConfig(
    name: string,
    config: Record<string, unknown>,
  ): Promise<PluginEntry> {
    return this.transport.request<PluginEntry>(
      "PUT",
      `/api/v1/plugins/${name}/config`,
      { json: config },
    );
  }

  /** Remove a plugin. */
  async remove(name: string): Promise<void> {
    await this.transport.request("DELETE", `/api/v1/plugins/${name}`);
  }

  // ── Sources ──────────────────────────────────────────────────────────

  /** List all plugin sources. */
  async listSources(): Promise<PluginSource[]> {
    return this.transport.request<PluginSource[]>(
      "GET",
      "/api/v1/plugin-sources",
    );
  }

  /** Create a new plugin source. */
  async createSource(req: CreatePluginSourceRequest): Promise<PluginSource> {
    return this.transport.request<PluginSource>(
      "POST",
      "/api/v1/plugin-sources",
      { json: req },
    );
  }

  /** Delete a plugin source by ID. */
  async deleteSource(id: string): Promise<void> {
    await this.transport.request("DELETE", `/api/v1/plugin-sources/${id}`);
  }

  // ── Policies ─────────────────────────────────────────────────────────

  /** List all plugin policies. */
  async listPolicies(): Promise<PluginPolicy[]> {
    return this.transport.request<PluginPolicy[]>(
      "GET",
      "/api/v1/plugin-policies",
    );
  }

  /** Create a new plugin policy. */
  async createPolicy(req: CreatePluginPolicyRequest): Promise<PluginPolicy> {
    return this.transport.request<PluginPolicy>(
      "POST",
      "/api/v1/plugin-policies",
      { json: req },
    );
  }

  /** Delete a plugin policy by ID. */
  async deletePolicy(id: string): Promise<void> {
    await this.transport.request("DELETE", `/api/v1/plugin-policies/${id}`);
  }
}
