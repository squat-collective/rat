"use client";

import useSWR, { useSWRConfig } from "swr";
import { useApiClient } from "@/providers/api-provider";
import { useCallback, useMemo, useState } from "react";
import type { UpdatePipelineRequest, CreateTriggerRequest, UpdateTriggerRequest, CreateQualityTestRequest, PreviewResponse, UpdateNamespaceRequest, UpdateLandingZoneRequest, UpdateTableMetadataRequest, PipelineConfig, CreatePluginSourceRequest, CreatePluginPolicyRequest, IdentityUser, IdentityCapabilities, Grant, GroupMember } from "@squat-collective/rat-client";
import yaml from "js-yaml";
import { KEYS } from "@/lib/cache-keys";

/** Pipelines */
export function usePipelines(params?: { namespace?: string; layer?: string }) {
  const api = useApiClient();
  return useSWR(
    KEYS.pipelines(params?.namespace, params?.layer),
    () => api.pipelines.list(params),
  );
}

export function usePipeline(ns: string, layer: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && layer && name ? KEYS.pipeline(ns, layer, name) : null,
    () => api.pipelines.get(ns, layer, name),
  );
}

export function useUpdatePipeline(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [updating, setUpdating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const update = useCallback(
    async (req: UpdatePipelineRequest) => {
      setUpdating(true);
      setError(null);
      try {
        const result = await api.pipelines.update(ns, layer, name, req);
        await mutate(KEYS.pipeline(ns, layer, name));
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setUpdating(false);
      }
    },
    [api, ns, layer, name, mutate],
  );

  return { update, updating, error };
}

/** Runs */
export function useRuns(params?: { namespace?: string; status?: string }) {
  const api = useApiClient();
  return useSWR(KEYS.runs(params), () => api.runs.list(params), {
    refreshInterval: 5000,
  });
}

export function useRun(id: string) {
  const api = useApiClient();
  return useSWR(id ? KEYS.run(id) : null, () => api.runs.get(id), {
    refreshInterval: (data) => {
      if (data?.status && !["running", "pending"].includes(data.status)) return 0;
      return 3000;
    },
    // Don't retry on 404s — the run simply doesn't exist
    onErrorRetry: (error, _key, _config, revalidate, { retryCount }) => {
      if (error?.statusCode === 404 || error?.name === "NotFoundError") return;
      if (retryCount >= 3) return;
      setTimeout(() => revalidate({ retryCount }), 5000 * (retryCount + 1));
    },
  });
}

export function useRunLogs(id: string) {
  const api = useApiClient();
  return useSWR(
    id ? KEYS.runLogs(id) : null,
    () => api.runs.logs(id),
    { refreshInterval: 3000 },
  );
}

export function useCreateRun(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const createRun = useCallback(async () => {
    setCreating(true);
    setError(null);
    try {
      const result = await api.runs.create({
        namespace: ns,
        layer,
        pipeline: name,
        trigger: "manual",
      });
      await mutate(KEYS.match.runs);
      return result;
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      setError(err);
      throw err;
    } finally {
      setCreating(false);
    }
  }, [api, ns, layer, name, mutate]);

  return { createRun, creating, error };
}

/** Tables */
export function useTables(params?: { namespace?: string; layer?: string }) {
  const api = useApiClient();
  return useSWR(
    KEYS.tables(params?.namespace, params?.layer),
    () => api.tables.list(params),
    { refreshInterval: 10000 },
  );
}

export function useTable(ns: string, layer: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && layer && name ? KEYS.table(ns, layer, name) : null,
    () => api.tables.get(ns, layer, name),
  );
}

export function useTablePreview(ns: string, layer: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && layer && name ? KEYS.tablePreview(ns, layer, name) : null,
    () => api.tables.preview(ns, layer, name),
  );
}

/** Files / Storage */
export function useFileTree(prefix?: string, exclude?: string) {
  const api = useApiClient();
  return useSWR(
    KEYS.files(prefix, exclude),
    () => api.storage.list(prefix, exclude),
  );
}

export function useFileContent(path: string | null) {
  const api = useApiClient();
  return useSWR(
    path ? KEYS.file(path) : null,
    () => api.storage.read(path!),
  );
}

/** Pipeline Config — reads and parses config.yaml from S3 */
export function usePipelineConfig(ns: string, layer: string, name: string) {
  const configPath = `${ns}/pipelines/${layer}/${name}/config.yaml`;
  const { data: fileContent, isLoading, mutate } = useFileContent(
    ns && layer && name ? configPath : null,
  );

  const config: PipelineConfig | null = useMemo(() => {
    if (!fileContent?.content) return null;
    try {
      const parsed = yaml.load(fileContent.content);
      if (typeof parsed !== "object" || parsed === null) return null;
      return parsed as PipelineConfig;
    } catch {
      return null;
    }
  }, [fileContent]);

  return { config, configPath, isLoading, mutate };
}

/** Query Schema — built from bulk schema endpoint (tables + columns) */
export function useQuerySchema() {
  const api = useApiClient();
  return useSWR(KEYS.querySchema(), async () => {
    const { tables } = await api.tables.schema();
    // Build schema tree: namespace -> layer -> table -> { column: type }
    const schema: Record<
      string,
      Record<string, Record<string, Record<string, string>>>
    > = {};
    for (const t of tables) {
      if (!schema[t.namespace]) schema[t.namespace] = {};
      if (!schema[t.namespace][t.layer]) schema[t.namespace][t.layer] = {};
      const cols: Record<string, string> = {};
      for (const c of t.columns) cols[c.name] = c.type;
      schema[t.namespace][t.layer][t.name] = cols;
    }
    return schema;
  });
}

/** Landing Zones */
export function useLandingZones(params?: { namespace?: string }) {
  const api = useApiClient();
  return useSWR(
    KEYS.landingZones(params?.namespace),
    () => api.landing.list(params),
    { refreshInterval: 10000 },
  );
}

export function useLandingZone(ns: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && name ? KEYS.landingZone(ns, name) : null,
    () => api.landing.get(ns, name),
  );
}

export function useLandingFiles(ns: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && name ? KEYS.landingFiles(ns, name) : null,
    () => api.landing.listFiles(ns, name),
    { refreshInterval: 5000 },
  );
}

export function useProcessedFiles(ns: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && name ? KEYS.processedFiles(ns, name) : null,
    () => api.storage.list(`${ns}/landing/${name}/_processed/`),
    { refreshInterval: 30000 },
  );
}

export function useLandingSamples(ns: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && name ? KEYS.landingSamples(ns, name) : null,
    () => api.landing.listSamples(ns, name),
    { refreshInterval: 10000 },
  );
}

/** Namespaces */
export function useNamespaces() {
  const api = useApiClient();
  return useSWR(KEYS.namespaces(), () => api.namespaces.list());
}

export function useUpdateNamespace(name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [updating, setUpdating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const update = useCallback(
    async (req: UpdateNamespaceRequest) => {
      setUpdating(true);
      setError(null);
      try {
        await api.namespaces.update(name, req);
        await mutate(KEYS.namespaces());
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setUpdating(false);
      }
    },
    [api, name, mutate],
  );

  return { update, updating, error };
}

export function useUpdateTableMetadata(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [updating, setUpdating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const update = useCallback(
    async (req: UpdateTableMetadataRequest) => {
      setUpdating(true);
      setError(null);
      try {
        const result = await api.tables.updateMetadata(ns, layer, name, req);
        await mutate(KEYS.table(ns, layer, name));
        await mutate(KEYS.match.tables);
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setUpdating(false);
      }
    },
    [api, ns, layer, name, mutate],
  );

  return { update, updating, error };
}

export function useUpdateLandingZone(ns: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [updating, setUpdating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const update = useCallback(
    async (req: UpdateLandingZoneRequest) => {
      setUpdating(true);
      setError(null);
      try {
        const result = await api.landing.update(ns, name, req);
        await mutate(KEYS.landingZone(ns, name));
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setUpdating(false);
      }
    },
    [api, ns, name, mutate],
  );

  return { update, updating, error };
}

/** Pipeline Triggers */
export function useTriggers(ns: string, layer: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && layer && name ? KEYS.triggers(ns, layer, name) : null,
    () => api.triggers.list(ns, layer, name),
    { refreshInterval: 10000 },
  );
}

export function useCreateTrigger(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const create = useCallback(
    async (req: CreateTriggerRequest) => {
      setCreating(true);
      setError(null);
      try {
        const result = await api.triggers.create(ns, layer, name, req);
        await mutate(KEYS.triggers(ns, layer, name));
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setCreating(false);
      }
    },
    [api, ns, layer, name, mutate],
  );

  return { create, creating, error };
}

export function useUpdateTrigger(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const update = useCallback(
    async (triggerId: string, req: UpdateTriggerRequest) => {
      const result = await api.triggers.update(ns, layer, name, triggerId, req);
      await mutate(KEYS.triggers(ns, layer, name));
      return result;
    },
    [api, ns, layer, name, mutate],
  );

  return { update };
}

export function useDeleteTrigger(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const deleteTrigger = useCallback(
    async (triggerId: string) => {
      await api.triggers.delete(ns, layer, name, triggerId);
      await mutate(KEYS.triggers(ns, layer, name));
    },
    [api, ns, layer, name, mutate],
  );

  return { deleteTrigger };
}

/** Quality Tests */
export function useQualityTests(ns: string, layer: string, name: string) {
  const api = useApiClient();
  return useSWR(
    ns && layer && name ? KEYS.qualityTests(ns, layer, name) : null,
    () => api.quality.list(ns, layer, name),
    { refreshInterval: 10000 },
  );
}

export function useCreateQualityTest(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const create = useCallback(
    async (req: CreateQualityTestRequest) => {
      setCreating(true);
      setError(null);
      try {
        const result = await api.quality.create(ns, layer, name, req);
        await mutate(KEYS.qualityTests(ns, layer, name));
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setCreating(false);
      }
    },
    [api, ns, layer, name, mutate],
  );

  return { create, creating, error };
}

export function useDeleteQualityTest(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const deleteTest = useCallback(
    async (testName: string) => {
      await api.quality.delete(ns, layer, name, testName);
      await mutate(KEYS.qualityTests(ns, layer, name));
    },
    [api, ns, layer, name, mutate],
  );

  return { deleteTest };
}

export function useRunQualityTests(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const [running, setRunning] = useState(false);
  const [results, setResults] = useState<Awaited<ReturnType<typeof api.quality.run>> | null>(null);
  const [error, setError] = useState<Error | null>(null);

  const runTests = useCallback(async () => {
    setRunning(true);
    setError(null);
    try {
      const res = await api.quality.run(ns, layer, name);
      setResults(res);
      return res;
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      setError(err);
      throw err;
    } finally {
      setRunning(false);
    }
  }, [api, ns, layer, name]);

  return { runTests, running, results, error };
}

/** Quality Test Preview */
export function usePreviewQualityTest(ns: string, layer: string, name: string) {
  const api = useApiClient();
  const [loading, setLoading] = useState<string | null>(null);
  const [results, setResults] = useState<Record<string, PreviewResponse>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});

  const preview = useCallback(
    async (testName: string, sql: string) => {
      setLoading(testName);
      setErrors((prev) => {
        const next = { ...prev };
        delete next[testName];
        return next;
      });
      try {
        const result = await api.pipelines.preview(ns, layer, name, {
          code: sql,
          limit: 100,
        });
        setResults((prev) => ({ ...prev, [testName]: result }));
        if (result.error) {
          setErrors((prev) => ({ ...prev, [testName]: result.error! }));
        }
      } catch (e) {
        setErrors((prev) => ({
          ...prev,
          [testName]: e instanceof Error ? e.message : String(e),
        }));
      } finally {
        setLoading(null);
      }
    },
    [api, ns, layer, name],
  );

  return { preview, loading, results, errors };
}

// Lineage moved to rat-plugin-lineage (the plugin fetches its own
// graph from /api/v1/x/lineage/graph). useLineage was removed; the
// SDK's api.lineage method still exists but points at the old
// /api/v1/lineage endpoint that no longer ships with ratd.

/** Health */
export function useFeatures() {
  const api = useApiClient();
  return useSWR(KEYS.features(), () => api.health.getFeatures());
}

/** Retention */
export function useRetentionConfig() {
  const api = useApiClient();
  return useSWR(KEYS.retentionConfig(), () => api.retention.getConfig());
}

export function useReaperStatus() {
  const api = useApiClient();
  return useSWR(KEYS.reaperStatus(), () => api.retention.getStatus(), {
    refreshInterval: 30000,
  });
}

export function useUpdateRetentionConfig() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [updating, setUpdating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const update = useCallback(
    async (config: Parameters<typeof api.retention.updateConfig>[0]) => {
      setUpdating(true);
      setError(null);
      try {
        const result = await api.retention.updateConfig(config);
        await mutate(KEYS.retentionConfig());
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setUpdating(false);
      }
    },
    [api, mutate],
  );

  return { update, updating, error };
}

export function useTriggerReaper() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [running, setRunning] = useState(false);

  const trigger = useCallback(async () => {
    setRunning(true);
    try {
      const result = await api.retention.triggerRun();
      await mutate(KEYS.reaperStatus());
      return result;
    } finally {
      setRunning(false);
    }
  }, [api, mutate]);

  return { trigger, running };
}

/** Identity — Pro feature stubs */
export function useIdentityUsers(params?: { search?: string; limit?: number; offset?: number }) {
  const api = useApiClient();
  const key = params ? JSON.stringify(params) : undefined;
  const query: Record<string, string> = {};
  if (params?.search) query.search = params.search;
  if (params?.limit !== undefined) query.limit = String(params.limit);
  if (params?.offset !== undefined) query.offset = String(params.offset);
  return useSWR(
    KEYS.identityUsers(key),
    async () => {
      return api.request<{ users: IdentityUser[]; total_count: number }>("GET", "/api/v1/identity/users", {
        params: Object.keys(query).length > 0 ? query : undefined,
      });
    },
  );
}

export function useIdentityCapabilities() {
  const api = useApiClient();
  return useSWR(KEYS.identityCapabilities(), async () => {
    return api.request<IdentityCapabilities>("GET", "/api/v1/identity/capabilities");
  });
}

/** Permissions — Pro feature stubs */
export function useGrants(filter?: { resource?: string; principal_type?: string }) {
  const api = useApiClient();
  const key = filter ? JSON.stringify(filter) : undefined;
  const query: Record<string, string> = {};
  if (filter?.resource) query.resource = filter.resource;
  if (filter?.principal_type) query.principal_type = filter.principal_type;
  return useSWR(KEYS.grants(key), async () => {
    return api.request<{ grants: Grant[] }>("GET", "/api/v1/permissions/grants", {
      params: Object.keys(query).length > 0 ? query : undefined,
    });
  });
}

export function useCreateGrant() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const createGrant = useCallback(
    async (req: { principal_type: string; principal_id: string; resource: string; verb: string }) => {
      setCreating(true);
      setError(null);
      try {
        const result = await api.request("POST", "/api/v1/permissions/grants", { json: req });
        await mutate((key: unknown) => typeof key === "string" && key.startsWith("grants"), undefined, { revalidate: true });
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setCreating(false);
      }
    },
    [api, mutate],
  );

  return { createGrant, creating, error };
}

export function useRevokeGrant() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [revoking, setRevoking] = useState(false);

  const revokeGrant = useCallback(
    async (grantId: string) => {
      setRevoking(true);
      try {
        await api.request("DELETE", `/api/v1/permissions/grants/${grantId}`);
        await mutate((key: unknown) => typeof key === "string" && key.startsWith("grants"), undefined, { revalidate: true });
      } finally {
        setRevoking(false);
      }
    },
    [api, mutate],
  );

  return { revokeGrant, revoking };
}

export function useVerbs() {
  const api = useApiClient();
  return useSWR(KEYS.verbs(), async () => {
    return api.request<{ verbs: Array<{ name: string; implies?: string[]; description?: string }> }>("GET", "/api/v1/permissions/verbs");
  });
}

export function useResourceAccess(resource: string) {
  const api = useApiClient();
  return useSWR(
    resource ? KEYS.resourceAccess(resource) : null,
    async () => {
      return api.request<{ access: Array<{ principal_type: string; principal_id: string; verb: string; source: string }> }>("GET", `/api/v1/permissions/access`, { params: { resource } });
    },
  );
}

export function useGroups() {
  const api = useApiClient();
  return useSWR(KEYS.groups(), async () => {
    return api.request<{ groups: Array<{ group_id: string; name: string; description?: string }> }>("GET", "/api/v1/permissions/groups");
  });
}

export function useCreateGroup() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const createGroup = useCallback(
    async (name: string, description?: string) => {
      setCreating(true);
      setError(null);
      try {
        const result = await api.request("POST", "/api/v1/permissions/groups", { json: { name, description } });
        await mutate(KEYS.groups());
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setCreating(false);
      }
    },
    [api, mutate],
  );

  return { createGroup, creating, error };
}

export function useDeleteGroup() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [deleting, setDeleting] = useState(false);

  const deleteGroup = useCallback(
    async (groupId: string) => {
      setDeleting(true);
      try {
        await api.request("DELETE", `/api/v1/permissions/groups/${groupId}`);
        await mutate(KEYS.groups());
      } finally {
        setDeleting(false);
      }
    },
    [api, mutate],
  );

  return { deleteGroup, deleting };
}

export function useGroupMembers(groupId: string) {
  const api = useApiClient();
  return useSWR(
    groupId ? KEYS.groupMembers(groupId) : null,
    async () => {
      return api.request<{ members: GroupMember[] }>("GET", `/api/v1/permissions/groups/${groupId}/members`);
    },
  );
}

export function useAddGroupMember(groupId: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [adding, setAdding] = useState(false);

  const addMember = useCallback(
    async (memberType: string, memberId: string) => {
      setAdding(true);
      try {
        await api.request("POST", `/api/v1/permissions/groups/${groupId}/members`, {
          json: { member_type: memberType, member_id: memberId },
        });
        await mutate(KEYS.groupMembers(groupId));
      } finally {
        setAdding(false);
      }
    },
    [api, groupId, mutate],
  );

  return { addMember, adding };
}

export function useRemoveGroupMember(groupId: string) {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [removing, setRemoving] = useState(false);

  const removeMember = useCallback(
    async (memberType: string, memberId: string) => {
      setRemoving(true);
      try {
        await api.request("DELETE", `/api/v1/permissions/groups/${groupId}/members`, {
          json: { member_type: memberType, member_id: memberId },
        });
        await mutate(KEYS.groupMembers(groupId));
      } finally {
        setRemoving(false);
      }
    },
    [api, groupId, mutate],
  );

  return { removeMember, removing };
}

/** Runner Plugins (installed Python packages) */
export function useRunnerPlugins() {
  const api = useApiClient();
  return useSWR(KEYS.runnerPlugins(), () => api.plugins.listRunnerPlugins());
}

/** Plugins */
export function usePlugins(filter?: { status?: string; kind?: string }) {
  const api = useApiClient();
  return useSWR(
    KEYS.plugins(filter?.status, filter?.kind),
    () => api.plugins.list(filter),
    { refreshInterval: 10000 },
  );
}

export function usePlugin(name: string) {
  const api = useApiClient();
  return useSWR(
    name ? KEYS.plugin(name) : null,
    () => api.plugins.get(name),
  );
}

export function useTogglePlugin() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const toggle = useCallback(
    async (name: string, enable: boolean) => {
      setLoading(true);
      setError(null);
      try {
        if (enable) {
          await api.plugins.enable(name);
        } else {
          await api.plugins.disable(name);
        }
        await mutate(KEYS.match.plugins);
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setLoading(false);
      }
    },
    [api, mutate],
  );

  return { toggle, loading, error };
}

export function useUpdatePluginConfig() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [updating, setUpdating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const updateConfig = useCallback(
    async (name: string, config: Record<string, unknown>) => {
      setUpdating(true);
      setError(null);
      try {
        const result = await api.plugins.updateConfig(name, config);
        await mutate(KEYS.match.plugins);
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setUpdating(false);
      }
    },
    [api, mutate],
  );

  return { updateConfig, updating, error };
}

export function useRemovePlugin() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const remove = useCallback(
    async (name: string) => {
      await api.plugins.remove(name);
      await mutate(KEYS.match.plugins);
    },
    [api, mutate],
  );

  return { remove };
}

/** Plugin Sources */
export function usePluginSources() {
  const api = useApiClient();
  return useSWR(KEYS.pluginSources(), () => api.plugins.listSources());
}

export function useCreatePluginSource() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const create = useCallback(
    async (req: CreatePluginSourceRequest) => {
      setCreating(true);
      setError(null);
      try {
        const result = await api.plugins.createSource(req);
        await mutate(KEYS.pluginSources());
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setCreating(false);
      }
    },
    [api, mutate],
  );

  return { create, creating, error };
}

export function useDeletePluginSource() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const deleteSource = useCallback(
    async (id: string) => {
      await api.plugins.deleteSource(id);
      await mutate(KEYS.pluginSources());
    },
    [api, mutate],
  );

  return { deleteSource };
}

/** Plugin Policies */
export function usePluginPolicies() {
  const api = useApiClient();
  return useSWR(KEYS.pluginPolicies(), () => api.plugins.listPolicies());
}

export function useCreatePluginPolicy() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const create = useCallback(
    async (req: CreatePluginPolicyRequest) => {
      setCreating(true);
      setError(null);
      try {
        const result = await api.plugins.createPolicy(req);
        await mutate(KEYS.pluginPolicies());
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setCreating(false);
      }
    },
    [api, mutate],
  );

  return { create, creating, error };
}

export function useDeletePluginPolicy() {
  const api = useApiClient();
  const { mutate } = useSWRConfig();

  const deletePolicy = useCallback(
    async (id: string) => {
      await api.plugins.deletePolicy(id);
      await mutate(KEYS.pluginPolicies());
    },
    [api, mutate],
  );

  return { deletePolicy };
}
