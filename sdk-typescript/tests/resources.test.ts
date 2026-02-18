import { describe, expect, it, vi, beforeEach } from "vitest";
import { RatClient } from "../src/client";
import { NotFoundError } from "../src/errors";

let fetchCalls: { url: string; method: string; body?: string }[] = [];

function setupMockFetch(responseBody: object, status = 200) {
  fetchCalls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(async (url: string, init: RequestInit) => {
      fetchCalls.push({
        url,
        method: init.method ?? "GET",
        body: init.body as string | undefined,
      });
      if (status >= 400) {
        return {
          status,
          statusText: status === 404 ? "Not Found" : "Error",
          headers: new Headers(),
          json: async () => responseBody,
          text: async () =>
            typeof responseBody === "string"
              ? responseBody
              : JSON.stringify(responseBody),
        };
      }
      return {
        status,
        statusText: "OK",
        json: async () => responseBody,
        text: async () => JSON.stringify(responseBody),
      };
    }),
  );
}

describe("PipelinesResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("lists pipelines with filters", async () => {
    setupMockFetch({ pipelines: [], total: 0 });
    const client = new RatClient();
    await client.pipelines.list({ namespace: "default", layer: "bronze" });

    expect(fetchCalls[0].url).toContain("/api/v1/pipelines?");
    expect(fetchCalls[0].url).toContain("namespace=default");
    expect(fetchCalls[0].url).toContain("layer=bronze");
  });

  it("gets a single pipeline", async () => {
    setupMockFetch({ id: "1", name: "test" });
    const client = new RatClient();
    await client.pipelines.get("default", "bronze", "test");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/pipelines/default/bronze/test",
    );
    expect(fetchCalls[0].method).toBe("GET");
  });

  it("creates a pipeline", async () => {
    setupMockFetch({ namespace: "default", layer: "bronze", name: "test", s3_path: "s3://...", files_created: [] });
    const client = new RatClient();
    await client.pipelines.create({
      namespace: "default",
      layer: "bronze",
      name: "test",
      type: "sql",
    });

    expect(fetchCalls[0].method).toBe("POST");
    expect(fetchCalls[0].url).toContain("/api/v1/pipelines");
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.namespace).toBe("default");
    expect(body.name).toBe("test");
  });
});

describe("RunsResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("lists runs", async () => {
    setupMockFetch({ runs: [], total: 0 });
    const client = new RatClient();
    await client.runs.list({ namespace: "default" });

    expect(fetchCalls[0].url).toContain("/api/v1/runs?namespace=default");
  });

  it("creates a run", async () => {
    setupMockFetch({ run_id: "abc", status: "pending" });
    const client = new RatClient();
    await client.runs.create({
      namespace: "default",
      layer: "bronze",
      pipeline: "test",
    });

    expect(fetchCalls[0].method).toBe("POST");
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.pipeline).toBe("test");
  });

  it("cancels a run", async () => {
    setupMockFetch({ run_id: "abc", status: "cancelled" });
    const client = new RatClient();
    await client.runs.cancel("abc");

    expect(fetchCalls[0].url).toContain("/api/v1/runs/abc/cancel");
    expect(fetchCalls[0].method).toBe("POST");
  });
});

describe("QueryResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("executes a query", async () => {
    setupMockFetch({
      columns: [{ name: "id", type: "INTEGER" }],
      rows: [{ id: 1 }],
      total_rows: 1,
      duration_ms: 42,
    });
    const client = new RatClient();
    const result = await client.query.execute({ sql: "SELECT 1" });

    expect(fetchCalls[0].method).toBe("POST");
    expect(fetchCalls[0].url).toContain("/api/v1/query");
    expect(result.rows).toEqual([{ id: 1 }]);
  });
});

describe("TablesResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("lists tables", async () => {
    setupMockFetch({ tables: [], total: 0 });
    const client = new RatClient();
    await client.tables.list({ namespace: "default" });

    expect(fetchCalls[0].url).toContain("/api/v1/tables?namespace=default");
  });

  it("gets table detail", async () => {
    setupMockFetch({ namespace: "default", layer: "bronze", name: "orders", columns: [] });
    const client = new RatClient();
    await client.tables.get("default", "bronze", "orders");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/tables/default/bronze/orders",
    );
  });

  it("previews table data", async () => {
    setupMockFetch({ columns: [], rows: [], total_rows: 0, duration_ms: 10 });
    const client = new RatClient();
    await client.tables.preview("default", "bronze", "orders");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/tables/default/bronze/orders/preview",
    );
  });
});

describe("StorageResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("lists files", async () => {
    setupMockFetch({ files: [] });
    const client = new RatClient();
    await client.storage.list("default/pipelines");

    expect(fetchCalls[0].url).toContain("/api/v1/files?prefix=");
  });

  it("reads a file", async () => {
    setupMockFetch({ path: "test.sql", content: "SELECT 1", size: 8, modified: "2026-01-01" });
    const client = new RatClient();
    const file = await client.storage.read("test.sql");

    expect(file.content).toBe("SELECT 1");
  });

  it("writes a file", async () => {
    setupMockFetch({ path: "test.sql", status: "written" });
    const client = new RatClient();
    await client.storage.write("test.sql", "SELECT 2");

    expect(fetchCalls[0].method).toBe("PUT");
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.content).toBe("SELECT 2");
  });
});

describe("NamespacesResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("lists namespaces", async () => {
    setupMockFetch({ namespaces: [{ name: "default" }], total: 1 });
    const client = new RatClient();
    const result = await client.namespaces.list();

    expect(result.namespaces).toHaveLength(1);
  });

  it("creates a namespace", async () => {
    setupMockFetch({ name: "staging" });
    const client = new RatClient();
    await client.namespaces.create("staging");

    expect(fetchCalls[0].method).toBe("POST");
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.name).toBe("staging");
  });
});

// ─── LandingResource ────────────────────────────────────────────────

describe("LandingResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("lists landing zones without filters", async () => {
    setupMockFetch({ zones: [], total: 0 });
    const client = new RatClient();
    const result = await client.landing.list();

    expect(fetchCalls[0].url).toContain("/api/v1/landing-zones");
    expect(fetchCalls[0].method).toBe("GET");
    expect(result.zones).toEqual([]);
  });

  it("lists landing zones with namespace filter", async () => {
    setupMockFetch({ zones: [{ name: "uploads" }], total: 1 });
    const client = new RatClient();
    await client.landing.list({ namespace: "staging" });

    expect(fetchCalls[0].url).toContain("/api/v1/landing-zones?namespace=staging");
  });

  it("gets a single landing zone", async () => {
    setupMockFetch({
      id: "lz-1",
      namespace: "default",
      name: "uploads",
      description: "Upload zone",
      file_count: 5,
      total_bytes: 1024,
    });
    const client = new RatClient();
    const zone = await client.landing.get("default", "uploads");

    expect(fetchCalls[0].url).toContain("/api/v1/landing-zones/default/uploads");
    expect(fetchCalls[0].method).toBe("GET");
    expect(zone.name).toBe("uploads");
  });

  it("creates a landing zone", async () => {
    setupMockFetch({
      id: "lz-2",
      namespace: "default",
      name: "csv-inbox",
      description: "CSV uploads",
    });
    const client = new RatClient();
    await client.landing.create({
      namespace: "default",
      name: "csv-inbox",
      description: "CSV uploads",
    });

    expect(fetchCalls[0].method).toBe("POST");
    expect(fetchCalls[0].url).toContain("/api/v1/landing-zones");
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.namespace).toBe("default");
    expect(body.name).toBe("csv-inbox");
    expect(body.description).toBe("CSV uploads");
  });

  it("updates a landing zone", async () => {
    setupMockFetch({
      id: "lz-1",
      namespace: "default",
      name: "uploads",
      description: "Updated description",
    });
    const client = new RatClient();
    await client.landing.update("default", "uploads", {
      description: "Updated description",
    });

    expect(fetchCalls[0].method).toBe("PUT");
    expect(fetchCalls[0].url).toContain("/api/v1/landing-zones/default/uploads");
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.description).toBe("Updated description");
  });

  it("deletes a landing zone", async () => {
    setupMockFetch({}, 204);
    const client = new RatClient();
    await client.landing.delete("default", "uploads");

    expect(fetchCalls[0].method).toBe("DELETE");
    expect(fetchCalls[0].url).toContain("/api/v1/landing-zones/default/uploads");
  });

  it("lists files in a landing zone", async () => {
    setupMockFetch({
      files: [{ id: "f-1", filename: "data.csv", size_bytes: 512 }],
      total: 1,
    });
    const client = new RatClient();
    const result = await client.landing.listFiles("default", "uploads");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/landing-zones/default/uploads/files",
    );
    expect(fetchCalls[0].method).toBe("GET");
    expect(result.files).toHaveLength(1);
  });

  it("gets a single file from a landing zone", async () => {
    setupMockFetch({
      id: "f-1",
      zone_id: "lz-1",
      filename: "data.csv",
      s3_path: "s3://bucket/data.csv",
      size_bytes: 512,
    });
    const client = new RatClient();
    const file = await client.landing.getFile("default", "uploads", "f-1");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/landing-zones/default/uploads/files/f-1",
    );
    expect(fetchCalls[0].method).toBe("GET");
    expect(file.filename).toBe("data.csv");
  });

  it("deletes a file from a landing zone", async () => {
    setupMockFetch({}, 204);
    const client = new RatClient();
    await client.landing.deleteFile("default", "uploads", "f-1");

    expect(fetchCalls[0].method).toBe("DELETE");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/landing-zones/default/uploads/files/f-1",
    );
  });

  it("lists sample files", async () => {
    setupMockFetch({
      files: [{ path: "sample.csv", size: 256, modified: "2026-01-01", type: "csv" }],
      total: 1,
    });
    const client = new RatClient();
    const result = await client.landing.listSamples("default", "uploads");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/landing-zones/default/uploads/samples",
    );
    expect(fetchCalls[0].method).toBe("GET");
    expect(result.files).toHaveLength(1);
  });

  it("deletes a sample file with URL-encoded filename", async () => {
    setupMockFetch({}, 204);
    const client = new RatClient();
    await client.landing.deleteSample("default", "uploads", "my file.csv");

    expect(fetchCalls[0].method).toBe("DELETE");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/landing-zones/default/uploads/samples/my%20file.csv",
    );
  });

  it("throws NotFoundError when landing zone does not exist", async () => {
    setupMockFetch("landing zone not found", 404);
    const client = new RatClient({ maxRetries: 1 });

    await expect(client.landing.get("default", "nonexistent")).rejects.toThrow(
      NotFoundError,
    );
  });
});

// ─── TriggersResource ───────────────────────────────────────────────

describe("TriggersResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("lists triggers for a pipeline", async () => {
    setupMockFetch({
      triggers: [{ id: "t-1", type: "cron", enabled: true }],
      total: 1,
    });
    const client = new RatClient();
    const result = await client.triggers.list("default", "bronze", "orders");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/pipelines/default/bronze/orders/triggers",
    );
    expect(fetchCalls[0].method).toBe("GET");
    expect(result.triggers).toHaveLength(1);
  });

  it("gets a single trigger", async () => {
    setupMockFetch({
      id: "t-1",
      pipeline_id: "p-1",
      type: "cron",
      config: { cron_expr: "0 * * * *" },
      enabled: true,
    });
    const client = new RatClient();
    const trigger = await client.triggers.get("default", "bronze", "orders", "t-1");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/pipelines/default/bronze/orders/triggers/t-1",
    );
    expect(fetchCalls[0].method).toBe("GET");
    expect(trigger.type).toBe("cron");
  });

  it("creates a trigger", async () => {
    setupMockFetch({
      id: "t-2",
      pipeline_id: "p-1",
      type: "cron",
      config: { cron_expr: "0 */6 * * *" },
      enabled: true,
      cooldown_seconds: 60,
    });
    const client = new RatClient();
    await client.triggers.create("default", "silver", "aggregates", {
      type: "cron",
      config: { cron_expr: "0 */6 * * *" },
      enabled: true,
      cooldown_seconds: 60,
    });

    expect(fetchCalls[0].method).toBe("POST");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/pipelines/default/silver/aggregates/triggers",
    );
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.type).toBe("cron");
    expect(body.config.cron_expr).toBe("0 */6 * * *");
    expect(body.cooldown_seconds).toBe(60);
  });

  it("updates a trigger", async () => {
    setupMockFetch({
      id: "t-1",
      type: "cron",
      config: { cron_expr: "0 0 * * *" },
      enabled: false,
    });
    const client = new RatClient();
    await client.triggers.update("default", "bronze", "orders", "t-1", {
      enabled: false,
      config: { cron_expr: "0 0 * * *" },
    });

    expect(fetchCalls[0].method).toBe("PUT");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/pipelines/default/bronze/orders/triggers/t-1",
    );
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.enabled).toBe(false);
    expect(body.config.cron_expr).toBe("0 0 * * *");
  });

  it("deletes a trigger", async () => {
    setupMockFetch({}, 204);
    const client = new RatClient();
    await client.triggers.delete("default", "bronze", "orders", "t-1");

    expect(fetchCalls[0].method).toBe("DELETE");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/pipelines/default/bronze/orders/triggers/t-1",
    );
  });

  it("throws NotFoundError when trigger does not exist", async () => {
    setupMockFetch("trigger not found", 404);
    const client = new RatClient({ maxRetries: 1 });

    await expect(
      client.triggers.get("default", "bronze", "orders", "nonexistent"),
    ).rejects.toThrow(NotFoundError);
  });
});

// ─── QualityResource ────────────────────────────────────────────────

describe("QualityResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("lists quality tests for a pipeline", async () => {
    setupMockFetch({
      tests: [
        { name: "not_null_id", sql: "SELECT ...", severity: "error" },
      ],
      total: 1,
    });
    const client = new RatClient();
    const result = await client.quality.list("default", "bronze", "orders");

    expect(fetchCalls[0].url).toContain("/api/v1/tests/default/bronze/orders");
    expect(fetchCalls[0].method).toBe("GET");
    expect(result.tests).toHaveLength(1);
    expect(result.tests[0].name).toBe("not_null_id");
  });

  it("creates a quality test", async () => {
    setupMockFetch({
      name: "unique_email",
      severity: "error",
      path: "tests/unique_email.sql",
    });
    const client = new RatClient();
    const created = await client.quality.create("default", "silver", "users", {
      name: "unique_email",
      sql: "SELECT COUNT(*) - COUNT(DISTINCT email) FROM users",
      severity: "error",
      description: "Emails must be unique",
    });

    expect(fetchCalls[0].method).toBe("POST");
    expect(fetchCalls[0].url).toContain("/api/v1/tests/default/silver/users");
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.name).toBe("unique_email");
    expect(body.sql).toContain("DISTINCT email");
    expect(body.severity).toBe("error");
    expect(created.path).toBe("tests/unique_email.sql");
  });

  it("deletes a quality test", async () => {
    setupMockFetch({}, 204);
    const client = new RatClient();
    await client.quality.delete("default", "bronze", "orders", "not_null_id");

    expect(fetchCalls[0].method).toBe("DELETE");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/tests/default/bronze/orders/not_null_id",
    );
  });

  it("runs quality tests for a pipeline", async () => {
    setupMockFetch({
      results: [
        {
          name: "not_null_id",
          status: "passed",
          severity: "error",
          value: 0,
          expected: 0,
          duration_ms: 15,
        },
      ],
      passed: 1,
      failed: 0,
      total: 1,
    });
    const client = new RatClient();
    const result = await client.quality.run("default", "bronze", "orders");

    expect(fetchCalls[0].method).toBe("POST");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/tests/default/bronze/orders/run",
    );
    expect(result.passed).toBe(1);
    expect(result.failed).toBe(0);
    expect(result.results[0].status).toBe("passed");
  });

  it("throws NotFoundError when pipeline has no tests", async () => {
    setupMockFetch("pipeline not found", 404);
    const client = new RatClient({ maxRetries: 1 });

    await expect(
      client.quality.list("default", "bronze", "nonexistent"),
    ).rejects.toThrow(NotFoundError);
  });
});

// ─── LineageResource ────────────────────────────────────────────────

describe("LineageResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("gets full lineage graph without filters", async () => {
    setupMockFetch({
      nodes: [
        { id: "n1", type: "pipeline", namespace: "default", name: "orders" },
        { id: "n2", type: "table", namespace: "default", layer: "bronze", name: "orders" },
      ],
      edges: [{ source: "n1", target: "n2", type: "produces" }],
    });
    const client = new RatClient();
    const graph = await client.lineage.get();

    expect(fetchCalls[0].url).toContain("/api/v1/lineage");
    expect(fetchCalls[0].url).not.toContain("?");
    expect(fetchCalls[0].method).toBe("GET");
    expect(graph.nodes).toHaveLength(2);
    expect(graph.edges).toHaveLength(1);
    expect(graph.edges[0].type).toBe("produces");
  });

  it("gets lineage graph filtered by namespace", async () => {
    setupMockFetch({ nodes: [], edges: [] });
    const client = new RatClient();
    await client.lineage.get({ namespace: "staging" });

    expect(fetchCalls[0].url).toContain("/api/v1/lineage?namespace=staging");
  });

  it("returns empty graph for namespace with no pipelines", async () => {
    setupMockFetch({ nodes: [], edges: [] });
    const client = new RatClient();
    const graph = await client.lineage.get({ namespace: "empty" });

    expect(graph.nodes).toEqual([]);
    expect(graph.edges).toEqual([]);
  });

  it("throws NotFoundError on invalid namespace", async () => {
    setupMockFetch("namespace not found", 404);
    const client = new RatClient({ maxRetries: 1 });

    await expect(
      client.lineage.get({ namespace: "nonexistent" }),
    ).rejects.toThrow(NotFoundError);
  });
});

// ─── RetentionResource ──────────────────────────────────────────────

describe("RetentionResource", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("gets system retention config", async () => {
    setupMockFetch({
      config: {
        runs_max_per_pipeline: 100,
        runs_max_age_days: 90,
        logs_max_age_days: 30,
        quality_results_max_per_test: 50,
        soft_delete_purge_days: 7,
        stuck_run_timeout_minutes: 60,
        audit_log_max_age_days: 365,
        nessie_orphan_branch_max_age_hours: 24,
        reaper_interval_minutes: 60,
        iceberg_snapshot_max_age_days: 7,
        iceberg_orphan_file_max_age_days: 3,
      },
    });
    const client = new RatClient();
    const result = await client.retention.getConfig();

    expect(fetchCalls[0].url).toContain("/api/v1/admin/retention/config");
    expect(fetchCalls[0].method).toBe("GET");
    expect(result.config.runs_max_per_pipeline).toBe(100);
    expect(result.config.runs_max_age_days).toBe(90);
  });

  it("updates system retention config", async () => {
    const updatedConfig = {
      runs_max_per_pipeline: 50,
      runs_max_age_days: 60,
      logs_max_age_days: 14,
      quality_results_max_per_test: 25,
      soft_delete_purge_days: 3,
      stuck_run_timeout_minutes: 30,
      audit_log_max_age_days: 180,
      nessie_orphan_branch_max_age_hours: 12,
      reaper_interval_minutes: 30,
      iceberg_snapshot_max_age_days: 3,
      iceberg_orphan_file_max_age_days: 1,
    };
    setupMockFetch({ config: updatedConfig });
    const client = new RatClient();
    await client.retention.updateConfig(updatedConfig);

    expect(fetchCalls[0].method).toBe("PUT");
    expect(fetchCalls[0].url).toContain("/api/v1/admin/retention/config");
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.runs_max_per_pipeline).toBe(50);
    expect(body.reaper_interval_minutes).toBe(30);
  });

  it("gets reaper status", async () => {
    setupMockFetch({
      last_run_at: "2026-02-15T12:00:00Z",
      runs_pruned: 10,
      logs_pruned: 50,
      quality_pruned: 5,
      pipelines_purged: 0,
      runs_failed: 2,
      branches_cleaned: 3,
      lz_files_cleaned: 7,
      audit_pruned: 100,
      updated_at: "2026-02-15T12:00:05Z",
    });
    const client = new RatClient();
    const status = await client.retention.getStatus();

    expect(fetchCalls[0].url).toContain("/api/v1/admin/retention/status");
    expect(fetchCalls[0].method).toBe("GET");
    expect(status.runs_pruned).toBe(10);
    expect(status.branches_cleaned).toBe(3);
  });

  it("triggers a manual reaper run", async () => {
    setupMockFetch({
      last_run_at: "2026-02-16T08:00:00Z",
      runs_pruned: 5,
      logs_pruned: 20,
      quality_pruned: 0,
      pipelines_purged: 0,
      runs_failed: 0,
      branches_cleaned: 1,
      lz_files_cleaned: 0,
      audit_pruned: 0,
      updated_at: "2026-02-16T08:00:03Z",
    });
    const client = new RatClient();
    const status = await client.retention.triggerRun();

    expect(fetchCalls[0].method).toBe("POST");
    expect(fetchCalls[0].url).toContain("/api/v1/admin/retention/run");
    expect(status.runs_pruned).toBe(5);
  });

  it("gets pipeline retention config", async () => {
    setupMockFetch({
      system: { runs_max_per_pipeline: 100, runs_max_age_days: 90 },
      overrides: { runs_max_per_pipeline: 200 },
      effective: { runs_max_per_pipeline: 200, runs_max_age_days: 90 },
    });
    const client = new RatClient();
    const result = await client.retention.getPipelineRetention(
      "default",
      "bronze",
      "orders",
    );

    expect(fetchCalls[0].url).toContain(
      "/api/v1/pipelines/default/bronze/orders/retention",
    );
    expect(fetchCalls[0].method).toBe("GET");
    expect(result.overrides?.runs_max_per_pipeline).toBe(200);
    expect(result.effective.runs_max_per_pipeline).toBe(200);
  });

  it("updates pipeline retention overrides", async () => {
    setupMockFetch({}, 204);
    const client = new RatClient();
    await client.retention.updatePipelineRetention(
      "default",
      "bronze",
      "orders",
      { runs_max_per_pipeline: 200, runs_max_age_days: 30 },
    );

    expect(fetchCalls[0].method).toBe("PUT");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/pipelines/default/bronze/orders/retention",
    );
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.runs_max_per_pipeline).toBe(200);
    expect(body.runs_max_age_days).toBe(30);
  });

  it("gets landing zone lifecycle settings", async () => {
    setupMockFetch({
      processed_max_age_days: 7,
      auto_purge: true,
    });
    const client = new RatClient();
    const result = await client.retention.getZoneLifecycle("default", "uploads");

    expect(fetchCalls[0].url).toContain(
      "/api/v1/landing-zones/default/uploads/lifecycle",
    );
    expect(fetchCalls[0].method).toBe("GET");
    expect(result.processed_max_age_days).toBe(7);
    expect(result.auto_purge).toBe(true);
  });

  it("updates landing zone lifecycle settings", async () => {
    setupMockFetch({}, 204);
    const client = new RatClient();
    await client.retention.updateZoneLifecycle("default", "uploads", {
      processed_max_age_days: 14,
      auto_purge: false,
    });

    expect(fetchCalls[0].method).toBe("PUT");
    expect(fetchCalls[0].url).toContain(
      "/api/v1/landing-zones/default/uploads/lifecycle",
    );
    const body = JSON.parse(fetchCalls[0].body!);
    expect(body.processed_max_age_days).toBe(14);
    expect(body.auto_purge).toBe(false);
  });

  it("throws NotFoundError for nonexistent pipeline retention", async () => {
    setupMockFetch("pipeline not found", 404);
    const client = new RatClient({ maxRetries: 1 });

    await expect(
      client.retention.getPipelineRetention("default", "bronze", "nonexistent"),
    ).rejects.toThrow(NotFoundError);
  });
});
