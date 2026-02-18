import { describe, expect, it } from "vitest";
import { RatClient } from "../src/client";
import { HealthResource } from "../src/resources/health";
import { PipelinesResource } from "../src/resources/pipelines";
import { RunsResource } from "../src/resources/runs";
import { QueryResource } from "../src/resources/query";
import { TablesResource } from "../src/resources/tables";
import { StorageResource } from "../src/resources/storage";
import { NamespacesResource } from "../src/resources/namespaces";
import { LandingResource } from "../src/resources/landing";
import { TriggersResource } from "../src/resources/triggers";
import { QualityResource } from "../src/resources/quality";
import { LineageResource } from "../src/resources/lineage";
import { RetentionResource } from "../src/resources/retention";

describe("RatClient", () => {
  it("initializes all resources", () => {
    const client = new RatClient();

    expect(client.health).toBeInstanceOf(HealthResource);
    expect(client.pipelines).toBeInstanceOf(PipelinesResource);
    expect(client.runs).toBeInstanceOf(RunsResource);
    expect(client.query).toBeInstanceOf(QueryResource);
    expect(client.tables).toBeInstanceOf(TablesResource);
    expect(client.storage).toBeInstanceOf(StorageResource);
    expect(client.namespaces).toBeInstanceOf(NamespacesResource);
    expect(client.landing).toBeInstanceOf(LandingResource);
    expect(client.triggers).toBeInstanceOf(TriggersResource);
    expect(client.quality).toBeInstanceOf(QualityResource);
    expect(client.lineage).toBeInstanceOf(LineageResource);
    expect(client.retention).toBeInstanceOf(RetentionResource);
  });

  it("uses default API URL", () => {
    const client = new RatClient();
    expect(client.config.apiUrl).toBe("http://localhost:8080");
  });

  it("accepts custom API URL", () => {
    const client = new RatClient({ apiUrl: "http://custom:9090" });
    expect(client.config.apiUrl).toBe("http://custom:9090");
  });

  it("accepts custom headers", () => {
    const client = new RatClient({
      headers: { "X-Custom": "value" },
    });
    expect(client.config.headers).toEqual({ "X-Custom": "value" });
  });

  it("has exactly 12 resource properties", () => {
    const client = new RatClient();
    const resourceNames = [
      "health",
      "pipelines",
      "runs",
      "query",
      "tables",
      "storage",
      "namespaces",
      "landing",
      "triggers",
      "quality",
      "lineage",
      "retention",
    ];
    for (const name of resourceNames) {
      expect(client).toHaveProperty(name);
    }
  });
});
