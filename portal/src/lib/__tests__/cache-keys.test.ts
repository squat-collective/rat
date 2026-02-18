import { describe, it, expect } from "vitest";
import { KEYS } from "../cache-keys";

describe("KEYS", () => {
  describe("pipelines", () => {
    it("returns base key when called with no args", () => {
      expect(KEYS.pipelines()).toBe("pipelines");
    });

    it("returns namespaced key when namespace is provided", () => {
      expect(KEYS.pipelines("myns")).toBe("pipelines-myns-all");
    });

    it("returns layered key when layer is provided", () => {
      expect(KEYS.pipelines(undefined, "silver")).toBe("pipelines-all-silver");
    });

    it("returns fully qualified key when both are provided", () => {
      expect(KEYS.pipelines("myns", "gold")).toBe("pipelines-myns-gold");
    });
  });

  describe("pipeline", () => {
    it("returns a deterministic key from namespace, layer, name", () => {
      expect(KEYS.pipeline("ns", "silver", "orders")).toBe(
        "pipeline-ns-silver-orders",
      );
    });
  });

  describe("pipelineRetention", () => {
    it("returns a retention-prefixed key", () => {
      expect(KEYS.pipelineRetention("ns", "gold", "agg")).toBe(
        "pipeline-retention-ns-gold-agg",
      );
    });
  });

  describe("runs", () => {
    it("returns base key when called with no params", () => {
      expect(KEYS.runs()).toBe("runs");
    });

    it("returns parameterized key when filters are given", () => {
      const key = KEYS.runs({ namespace: "ns", status: "running" });
      expect(key).toContain("runs-");
      expect(key).toContain("namespace");
      expect(key).toContain("running");
    });

    it("serializes params deterministically via JSON.stringify", () => {
      const a = KEYS.runs({ namespace: "ns", status: "failed" });
      const b = KEYS.runs({ namespace: "ns", status: "failed" });
      expect(a).toBe(b);
    });
  });

  describe("run", () => {
    it("returns key with run id", () => {
      expect(KEYS.run("abc-123")).toBe("run-abc-123");
    });
  });

  describe("runLogs", () => {
    it("returns log key with run id", () => {
      expect(KEYS.runLogs("run-42")).toBe("run-logs-run-42");
    });
  });

  describe("tables", () => {
    it("returns base key when called with no args", () => {
      expect(KEYS.tables()).toBe("tables");
    });

    it("returns filtered key with namespace", () => {
      expect(KEYS.tables("myns")).toBe("tables-myns-all");
    });

    it("returns filtered key with layer", () => {
      expect(KEYS.tables(undefined, "bronze")).toBe("tables-all-bronze");
    });

    it("returns fully qualified key", () => {
      expect(KEYS.tables("myns", "silver")).toBe("tables-myns-silver");
    });
  });

  describe("table", () => {
    it("returns deterministic table key", () => {
      expect(KEYS.table("ns", "silver", "orders")).toBe(
        "table-ns-silver-orders",
      );
    });
  });

  describe("tablePreview", () => {
    it("returns preview-prefixed key", () => {
      expect(KEYS.tablePreview("ns", "bronze", "raw")).toBe(
        "table-preview-ns-bronze-raw",
      );
    });
  });

  describe("files", () => {
    it("returns base key when called with no args", () => {
      expect(KEYS.files()).toBe("files-all");
    });

    it("includes prefix in key", () => {
      expect(KEYS.files("ns/pipelines")).toBe("files-ns/pipelines");
    });

    it("includes both prefix and exclude in key", () => {
      expect(KEYS.files("ns/pipelines", "_processed")).toBe(
        "files-ns/pipelines-_processed",
      );
    });

    it("includes only exclude when prefix is undefined", () => {
      expect(KEYS.files(undefined, "_processed")).toBe("files-_processed");
    });
  });

  describe("file", () => {
    it("returns file key with path", () => {
      expect(KEYS.file("ns/pipelines/silver/orders/pipeline.sql")).toBe(
        "file-ns/pipelines/silver/orders/pipeline.sql",
      );
    });
  });

  describe("querySchema", () => {
    it("returns constant key", () => {
      expect(KEYS.querySchema()).toBe("query-schema");
    });
  });

  describe("landing zones", () => {
    it("landingZones returns base key without namespace", () => {
      expect(KEYS.landingZones()).toBe("landing-zones");
    });

    it("landingZones returns namespaced key", () => {
      expect(KEYS.landingZones("myns")).toBe("landing-zones-myns");
    });

    it("landingZone returns detail key", () => {
      expect(KEYS.landingZone("myns", "uploads")).toBe(
        "landing-zone-myns-uploads",
      );
    });

    it("landingFiles returns file list key", () => {
      expect(KEYS.landingFiles("myns", "uploads")).toBe(
        "landing-files-myns-uploads",
      );
    });

    it("landingSamples returns samples key", () => {
      expect(KEYS.landingSamples("myns", "uploads")).toBe(
        "landing-samples-myns-uploads",
      );
    });

    it("processedFiles returns processed key", () => {
      expect(KEYS.processedFiles("myns", "uploads")).toBe(
        "processed-myns-uploads",
      );
    });

    it("zoneLifecycle returns lifecycle key", () => {
      expect(KEYS.zoneLifecycle("myns", "uploads")).toBe(
        "zone-lifecycle-myns-uploads",
      );
    });
  });

  describe("namespaces", () => {
    it("returns constant key", () => {
      expect(KEYS.namespaces()).toBe("namespaces");
    });
  });

  describe("triggers", () => {
    it("returns qualified triggers key", () => {
      expect(KEYS.triggers("ns", "silver", "orders")).toBe(
        "triggers-ns-silver-orders",
      );
    });
  });

  describe("qualityTests", () => {
    it("returns qualified quality key", () => {
      expect(KEYS.qualityTests("ns", "gold", "agg")).toBe(
        "quality-ns-gold-agg",
      );
    });
  });

  describe("lineage", () => {
    it("returns namespaced lineage key", () => {
      expect(KEYS.lineage("myns")).toBe("lineage-myns");
    });

    it("returns all-lineage key without namespace", () => {
      expect(KEYS.lineage()).toBe("lineage-all");
    });
  });

  describe("features", () => {
    it("returns constant key", () => {
      expect(KEYS.features()).toBe("features");
    });
  });

  describe("retention keys", () => {
    it("retentionConfig returns constant key", () => {
      expect(KEYS.retentionConfig()).toBe("retention-config");
    });

    it("reaperStatus returns constant key", () => {
      expect(KEYS.reaperStatus()).toBe("reaper-status");
    });
  });
});

describe("KEYS.match", () => {
  describe("pipelines matcher", () => {
    it("matches pipeline detail keys", () => {
      expect(KEYS.match.pipelines("pipeline-ns-silver-orders")).toBe(true);
    });

    it("matches pipelines list keys", () => {
      expect(KEYS.match.pipelines("pipelines")).toBe(true);
      expect(KEYS.match.pipelines("pipelines-ns-all")).toBe(true);
    });

    it("does not match non-pipeline keys", () => {
      expect(KEYS.match.pipelines("runs")).toBe(false);
      expect(KEYS.match.pipelines("tables")).toBe(false);
    });

    it("returns false for non-string keys", () => {
      expect(KEYS.match.pipelines(123)).toBe(false);
      expect(KEYS.match.pipelines(null)).toBe(false);
      expect(KEYS.match.pipelines(undefined)).toBe(false);
    });
  });

  describe("runs matcher", () => {
    it("matches run list keys", () => {
      expect(KEYS.match.runs("runs")).toBe(true);
      expect(KEYS.match.runs("runs-{\"namespace\":\"ns\"}")).toBe(true);
    });

    it("does not match run detail keys (run- prefix)", () => {
      // run-xxx starts with "run" but not "runs"
      expect(KEYS.match.runs("run-abc")).toBe(false);
    });

    it("returns false for non-string keys", () => {
      expect(KEYS.match.runs(42)).toBe(false);
    });
  });

  describe("tables matcher", () => {
    it("matches tables list keys", () => {
      expect(KEYS.match.tables("tables")).toBe(true);
      expect(KEYS.match.tables("tables-ns-silver")).toBe(true);
    });

    it("does not match table detail keys", () => {
      // "table-..." does not start with "tables"
      expect(KEYS.match.tables("table-ns-silver-orders")).toBe(false);
    });
  });

  describe("lineage matcher", () => {
    it("matches lineage keys", () => {
      expect(KEYS.match.lineage("lineage-myns")).toBe(true);
      expect(KEYS.match.lineage("lineage-all")).toBe(true);
    });

    it("does not match unrelated keys", () => {
      expect(KEYS.match.lineage("pipelines")).toBe(false);
    });
  });

  describe("files matcher", () => {
    it("matches files keys with prefix", () => {
      expect(KEYS.match.files("files-ns/pipelines")).toBe(true);
    });

    it("does not match bare file key without dash", () => {
      // "files" without "-" does not match "files-" prefix
      expect(KEYS.match.files("files")).toBe(false);
    });

    it("does not match single file key", () => {
      expect(KEYS.match.files("file-some/path")).toBe(false);
    });
  });

  describe("landingZones matcher", () => {
    it("matches landing-zones list keys", () => {
      expect(KEYS.match.landingZones("landing-zones")).toBe(true);
      expect(KEYS.match.landingZones("landing-zones-myns")).toBe(true);
    });

    it("does not match landing-zone detail keys", () => {
      expect(KEYS.match.landingZones("landing-zone-ns-uploads")).toBe(false);
    });
  });

  describe("landingZone (detail) matcher", () => {
    it("matches specific zone detail keys", () => {
      const matcher = KEYS.match.landingZone("myns", "uploads");
      expect(matcher("landing-zone-myns-uploads")).toBe(true);
    });

    it("does not match different zone", () => {
      const matcher = KEYS.match.landingZone("myns", "uploads");
      expect(matcher("landing-zone-other-files")).toBe(false);
    });
  });

  describe("allLandingFiles matcher", () => {
    it("matches any landing-files key", () => {
      expect(KEYS.match.allLandingFiles("landing-files-ns-uploads")).toBe(true);
      expect(KEYS.match.allLandingFiles("landing-files-other-zone")).toBe(true);
    });

    it("does not match landing-zones keys", () => {
      expect(KEYS.match.allLandingFiles("landing-zones")).toBe(false);
    });
  });

  describe("landingFiles (specific) matcher", () => {
    it("matches specific zone file keys", () => {
      const matcher = KEYS.match.landingFiles("myns", "uploads");
      expect(matcher("landing-files-myns-uploads")).toBe(true);
    });

    it("does not match different zone", () => {
      const matcher = KEYS.match.landingFiles("myns", "uploads");
      expect(matcher("landing-files-other-zone")).toBe(false);
    });
  });

  describe("landingSamples (specific) matcher", () => {
    it("matches specific zone sample keys", () => {
      const matcher = KEYS.match.landingSamples("myns", "uploads");
      expect(matcher("landing-samples-myns-uploads")).toBe(true);
    });

    it("does not match different zone", () => {
      const matcher = KEYS.match.landingSamples("myns", "uploads");
      expect(matcher("landing-samples-other-zone")).toBe(false);
    });
  });
});
