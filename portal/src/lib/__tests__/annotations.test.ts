import { describe, it, expect } from "vitest";
import { extractAnnotations, generateStrategySnippet } from "../annotations";

describe("extractAnnotations", () => {
  it("parses SQL annotations (-- @key: value)", () => {
    const source = `-- @merge_strategy: incremental
-- @unique_key: id, email
-- @watermark_column: updated_at

SELECT * FROM {{ ref("bronze.orders") }}`;

    const result = extractAnnotations(source);
    expect(result.merge_strategy).toBe("incremental");
    expect(result.unique_key).toEqual(["id", "email"]);
    expect(result.watermark_column).toBe("updated_at");
  });

  it("parses Python annotations (# @key: value)", () => {
    const source = `# @merge_strategy: full_refresh
# @description: Load raw orders
"""Pipeline docstring."""
import duckdb`;

    const result = extractAnnotations(source);
    expect(result.merge_strategy).toBe("full_refresh");
    expect(result.raw["description"]).toBe("Load raw orders");
  });

  it("parses comma-separated unique_key", () => {
    const source = `-- @unique_key: id, email, tenant_id

SELECT 1`;

    const result = extractAnnotations(source);
    expect(result.unique_key).toEqual(["id", "email", "tenant_id"]);
  });

  it("stops at first non-comment non-empty line", () => {
    const source = `-- @merge_strategy: incremental
SELECT * FROM foo
-- @unique_key: id`;

    const result = extractAnnotations(source);
    expect(result.merge_strategy).toBe("incremental");
    expect(result.unique_key).toBeNull();
  });

  it("returns nulls when no annotations present", () => {
    const source = `SELECT * FROM foo`;

    const result = extractAnnotations(source);
    expect(result.merge_strategy).toBeNull();
    expect(result.unique_key).toBeNull();
    expect(result.watermark_column).toBeNull();
    expect(result.partition_column).toBeNull();
    expect(result.scd_valid_from).toBeNull();
    expect(result.scd_valid_to).toBeNull();
  });

  it("handles empty string", () => {
    const result = extractAnnotations("");
    expect(result.merge_strategy).toBeNull();
    expect(result.raw).toEqual({});
  });

  it("parses all SCD2 strategy fields", () => {
    const source = `-- @merge_strategy: scd2
-- @unique_key: customer_id
-- @scd_valid_from: effective_date
-- @scd_valid_to: end_date

SELECT * FROM {{ ref("bronze.customers") }}`;

    const result = extractAnnotations(source);
    expect(result.merge_strategy).toBe("scd2");
    expect(result.unique_key).toEqual(["customer_id"]);
    expect(result.scd_valid_from).toBe("effective_date");
    expect(result.scd_valid_to).toBe("end_date");
  });

  it("parses snapshot strategy with partition_column", () => {
    const source = `-- @merge_strategy: snapshot
-- @partition_column: event_date

SELECT * FROM events`;

    const result = extractAnnotations(source);
    expect(result.merge_strategy).toBe("snapshot");
    expect(result.partition_column).toBe("event_date");
  });

  it("stores non-strategy annotations in raw only", () => {
    const source = `-- @description: My pipeline
-- @materialized: view
-- @merge_strategy: incremental

SELECT 1`;

    const result = extractAnnotations(source);
    expect(result.raw["description"]).toBe("My pipeline");
    expect(result.raw["materialized"]).toBe("view");
    expect(result.raw["merge_strategy"]).toBe("incremental");
  });

  it("skips empty lines between annotations", () => {
    const source = `-- @merge_strategy: incremental

-- @unique_key: id

SELECT 1`;

    const result = extractAnnotations(source);
    expect(result.merge_strategy).toBe("incremental");
    // empty lines are allowed between comment blocks
    expect(result.unique_key).toEqual(["id"]);
  });

  it("handles single unique_key without comma", () => {
    const source = `-- @unique_key: id

SELECT 1`;

    const result = extractAnnotations(source);
    expect(result.unique_key).toEqual(["id"]);
  });

  it("parses @materialized: view", () => {
    const source = `-- @materialized: view
-- @merge_strategy: full_refresh

SELECT 1`;

    const result = extractAnnotations(source);
    expect(result.materialized).toBe("view");
  });

  it("parses @archive_landing_zones: true as boolean", () => {
    const source = `-- @archive_landing_zones: true
-- @merge_strategy: incremental
-- @unique_key: id

SELECT 1`;

    const result = extractAnnotations(source);
    expect(result.archive_landing_zones).toBe(true);
  });

  it("parses @archive_landing_zones: false as false (not null)", () => {
    const source = `-- @archive_landing_zones: false

SELECT 1`;

    const result = extractAnnotations(source);
    expect(result.archive_landing_zones).toBe(false);
  });

  describe("malformed annotations", () => {
    it("ignores annotation missing value after colon", () => {
      // Regex requires `.+` after the colon, so `-- @key:` with nothing matches nothing
      const source = `-- @merge_strategy:
-- @unique_key: id

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBeNull();
      expect(result.unique_key).toEqual(["id"]);
    });

    it("ignores annotation missing colon", () => {
      const source = `-- @merge_strategy incremental

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBeNull();
      expect(result.raw).toEqual({});
    });

    it("ignores annotation with invalid key characters (hyphens)", () => {
      // \\w+ only matches [a-zA-Z0-9_], so hyphens break the match
      const source = `-- @merge-strategy: incremental

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBeNull();
      expect(result.raw).toEqual({});
    });

    it("ignores annotation missing @ prefix", () => {
      const source = `-- merge_strategy: incremental

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBeNull();
    });
  });

  describe("whitespace handling", () => {
    it("handles extra whitespace between -- and @key", () => {
      const source = `--    @merge_strategy: incremental

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("incremental");
    });

    it("trims leading whitespace from indented comment lines", () => {
      const source = `    -- @merge_strategy: full_refresh
    -- @unique_key: id

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("full_refresh");
      expect(result.unique_key).toEqual(["id"]);
    });

    it("trims trailing whitespace from annotation values", () => {
      const source = "-- @merge_strategy: incremental   \n-- @watermark_column: updated_at   \n\nSELECT 1";

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("incremental");
      expect(result.watermark_column).toBe("updated_at");
    });

    it("handles tab characters in indentation", () => {
      const source = `\t-- @merge_strategy: append_only

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("append_only");
    });
  });

  describe("Windows line endings (\\r\\n)", () => {
    it("parses annotations with CRLF line endings", () => {
      const source = "-- @merge_strategy: incremental\r\n-- @unique_key: id, email\r\n\r\nSELECT 1";

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("incremental");
      expect(result.unique_key).toEqual(["id", "email"]);
    });

    it("parses annotations with mixed LF and CRLF line endings", () => {
      const source = "-- @merge_strategy: scd2\r\n-- @unique_key: cust_id\n-- @scd_valid_from: start_dt\r\n\nSELECT 1";

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("scd2");
      expect(result.unique_key).toEqual(["cust_id"]);
      expect(result.scd_valid_from).toBe("start_dt");
    });
  });

  describe("comment styles", () => {
    it("does not parse block comments (/* */)", () => {
      const source = `/* @merge_strategy: incremental */

SELECT 1`;

      const result = extractAnnotations(source);
      // Block comment line doesn't start with -- or #, so parser breaks
      expect(result.merge_strategy).toBeNull();
      expect(result.raw).toEqual({});
    });

    it("skips non-annotation comment lines without breaking", () => {
      const source = `-- This is a header comment
-- @merge_strategy: incremental
-- Another comment
-- @unique_key: id

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("incremental");
      expect(result.unique_key).toEqual(["id"]);
    });

    it("handles mixing SQL and Python comment prefixes", () => {
      const source = `-- @merge_strategy: incremental
# @unique_key: id

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("incremental");
      expect(result.unique_key).toEqual(["id"]);
    });
  });

  describe("empty and minimal input", () => {
    it("handles whitespace-only input", () => {
      const result = extractAnnotations("   \n  \n  ");
      expect(result.merge_strategy).toBeNull();
      expect(result.raw).toEqual({});
    });

    it("handles input with only newlines", () => {
      const result = extractAnnotations("\n\n\n");
      expect(result.merge_strategy).toBeNull();
      expect(result.raw).toEqual({});
    });

    it("handles a single annotation with no trailing SQL", () => {
      const source = `-- @merge_strategy: full_refresh`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("full_refresh");
    });
  });

  describe("duplicate annotations", () => {
    it("last duplicate annotation wins", () => {
      const source = `-- @merge_strategy: incremental
-- @merge_strategy: full_refresh

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("full_refresh");
      expect(result.raw["merge_strategy"]).toBe("full_refresh");
    });

    it("last duplicate unique_key wins", () => {
      const source = `-- @unique_key: id
-- @unique_key: email, tenant_id

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.unique_key).toEqual(["email", "tenant_id"]);
    });
  });

  describe("special characters in values", () => {
    it("handles values with double quotes", () => {
      const source = `-- @description: Load "raw" orders

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toBe('Load "raw" orders');
    });

    it("handles values with single quotes", () => {
      const source = `-- @description: Tom's pipeline

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toBe("Tom's pipeline");
    });

    it("handles values with backslashes", () => {
      const source = `-- @description: path\\to\\file

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toBe("path\\to\\file");
    });

    it("handles values with colons", () => {
      const source = `-- @description: time: 12:30:00

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toBe("time: 12:30:00");
    });

    it("handles values with parentheses and brackets", () => {
      const source = `-- @description: func(a, b) [test]

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toBe("func(a, b) [test]");
    });
  });

  describe("unicode characters in values", () => {
    it("handles unicode characters in annotation values", () => {
      const source = `-- @description: Pipeline pour les commandes francaises

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toBe("Pipeline pour les commandes francaises");
    });

    it("handles emoji in annotation values", () => {
      const source = `-- @description: rocket launch pipeline \u{1F680}

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toContain("\u{1F680}");
    });

    it("handles CJK characters in annotation values", () => {
      const source = `-- @description: \u30C7\u30FC\u30BF\u30D1\u30A4\u30D7\u30E9\u30A4\u30F3

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toBe("\u30C7\u30FC\u30BF\u30D1\u30A4\u30D7\u30E9\u30A4\u30F3");
    });
  });

  describe("long annotation values", () => {
    it("handles very long annotation values", () => {
      const longValue = "a".repeat(1000);
      const source = `-- @description: ${longValue}

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.raw["description"]).toBe(longValue);
      expect(result.raw["description"]).toHaveLength(1000);
    });

    it("handles unique_key with many comma-separated values", () => {
      const keys = Array.from({ length: 50 }, (_, i) => `col_${i}`);
      const source = `-- @unique_key: ${keys.join(", ")}

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.unique_key).toEqual(keys);
      expect(result.unique_key).toHaveLength(50);
    });
  });

  describe("annotation position edge cases", () => {
    it("parses annotations that are the entire file (no SQL body)", () => {
      const source = `-- @merge_strategy: incremental
-- @unique_key: id
-- @watermark_column: updated_at`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("incremental");
      expect(result.unique_key).toEqual(["id"]);
      expect(result.watermark_column).toBe("updated_at");
    });

    it("stops parsing when non-comment code appears between annotations", () => {
      const source = `-- @merge_strategy: incremental
CREATE TABLE foo AS
-- @unique_key: id

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("incremental");
      // CREATE TABLE line stops the parser, so unique_key is never reached
      expect(result.unique_key).toBeNull();
    });
  });

  describe("merge strategy validation", () => {
    it("passes through unknown strategy values (not null)", () => {
      const source = `-- @merge_strategy: unknown_strategy

SELECT 1`;

      const result = extractAnnotations(source);
      // Unknown strategies are passed through for display with warning, not nullified
      expect(result.merge_strategy).toBe("unknown_strategy");
    });

    it("handles archive_landing_zones with mixed case TRUE", () => {
      const source = `-- @archive_landing_zones: TRUE

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.archive_landing_zones).toBe(true);
    });

    it("handles archive_landing_zones with non-boolean value as false", () => {
      const source = `-- @archive_landing_zones: yes

SELECT 1`;

      const result = extractAnnotations(source);
      // "yes".toLowerCase() !== "true", so it evaluates to false (not null)
      expect(result.archive_landing_zones).toBe(false);
    });
  });

  describe("unique_key parsing edge cases", () => {
    it("filters out empty strings from trailing comma", () => {
      const source = `-- @unique_key: id, email,

SELECT 1`;

      const result = extractAnnotations(source);
      // trailing comma produces empty string which is filtered by .filter(Boolean)
      expect(result.unique_key).toEqual(["id", "email"]);
    });

    it("handles unique_key with extra spaces between commas", () => {
      const source = `-- @unique_key:   id  ,  email  ,  name

SELECT 1`;

      const result = extractAnnotations(source);
      expect(result.unique_key).toEqual(["id", "email", "name"]);
    });
  });

  describe("CRLF line endings", () => {
    it("parses annotations with CRLF line endings", () => {
      const source = "-- @merge_strategy: incremental\r\n-- @unique_key: id, email\r\n\r\nSELECT 1";

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("incremental");
      expect(result.unique_key).toEqual(["id", "email"]);
    });
  });

  describe("annotation stops at code boundary", () => {
    it("stops at Python triple-quoted docstring", () => {
      const source = `# @merge_strategy: full_refresh
"""This is a docstring."""
# @unique_key: id`;

      const result = extractAnnotations(source);
      expect(result.merge_strategy).toBe("full_refresh");
      // Triple-quoted line is not a comment, parser stops
      expect(result.unique_key).toBeNull();
    });
  });

  describe("all six valid strategies are recognized", () => {
    const strategies = [
      "full_refresh", "incremental", "append_only",
      "delete_insert", "scd2", "snapshot",
    ] as const;

    for (const s of strategies) {
      it(`recognizes ${s} as a valid strategy`, () => {
        const source = `-- @merge_strategy: ${s}\n\nSELECT 1`;
        const result = extractAnnotations(source);
        expect(result.merge_strategy).toBe(s);
      });
    }
  });
});

describe("generateStrategySnippet", () => {
  it("generates full_refresh snippet for SQL", () => {
    const snippet = generateStrategySnippet("full_refresh", "--");
    expect(snippet).toContain("-- @merge_strategy: full_refresh");
    expect(snippet).not.toContain("@unique_key");
  });

  it("generates incremental snippet for SQL", () => {
    const snippet = generateStrategySnippet("incremental", "--");
    expect(snippet).toContain("-- @merge_strategy: incremental");
    expect(snippet).toContain("-- @unique_key:");
    expect(snippet).toContain("-- @watermark_column:");
  });

  it("generates scd2 snippet for Python", () => {
    const snippet = generateStrategySnippet("scd2", "#");
    expect(snippet).toContain("# @merge_strategy: scd2");
    expect(snippet).toContain("# @unique_key:");
    expect(snippet).toContain("# @scd_valid_from:");
    expect(snippet).toContain("# @scd_valid_to:");
  });

  it("generates snapshot snippet with partition_column", () => {
    const snippet = generateStrategySnippet("snapshot", "--");
    expect(snippet).toContain("-- @merge_strategy: snapshot");
    expect(snippet).toContain("-- @partition_column:");
  });

  it("generates delete_insert snippet with unique_key", () => {
    const snippet = generateStrategySnippet("delete_insert", "--");
    expect(snippet).toContain("-- @merge_strategy: delete_insert");
    expect(snippet).toContain("-- @unique_key:");
    expect(snippet).not.toContain("@watermark_column");
  });

  it("generates append_only snippet without extra fields", () => {
    const snippet = generateStrategySnippet("append_only", "#");
    expect(snippet).toContain("# @merge_strategy: append_only");
    expect(snippet).not.toContain("@unique_key");
  });

  it("uses placeholder values in generated snippets", () => {
    const snippet = generateStrategySnippet("incremental", "--");
    expect(snippet).toContain("id, email");
    expect(snippet).toContain("updated_at");
  });

  it("generates multi-line snippet with correct line count for scd2", () => {
    const snippet = generateStrategySnippet("scd2", "--");
    const lines = snippet.split("\n");
    // scd2: merge_strategy + unique_key + scd_valid_from + scd_valid_to = 4 lines
    expect(lines).toHaveLength(4);
  });

  it("generates single-line snippet for strategies with no extra fields", () => {
    const snippet = generateStrategySnippet("full_refresh", "--");
    const lines = snippet.split("\n");
    expect(lines).toHaveLength(1);
  });

  it("produces parseable snippet that extractAnnotations can read back", () => {
    const snippet = generateStrategySnippet("incremental", "--");
    const source = snippet + "\n\nSELECT 1";
    const parsed = extractAnnotations(source);
    expect(parsed.merge_strategy).toBe("incremental");
    expect(parsed.unique_key).toBeTruthy();
    expect(parsed.watermark_column).toBeTruthy();
  });

  it("produces parseable Python snippet that extractAnnotations can read back", () => {
    const snippet = generateStrategySnippet("scd2", "#");
    const source = snippet + "\n\nimport duckdb";
    const parsed = extractAnnotations(source);
    expect(parsed.merge_strategy).toBe("scd2");
    expect(parsed.unique_key).toBeTruthy();
    expect(parsed.scd_valid_from).toBeTruthy();
    expect(parsed.scd_valid_to).toBeTruthy();
  });

  it("snapshot snippet uses event_date as partition_column placeholder", () => {
    const snippet = generateStrategySnippet("snapshot", "--");
    expect(snippet).toContain("event_date");
  });

  it("scd2 snippet uses valid_from and valid_to as placeholders", () => {
    const snippet = generateStrategySnippet("scd2", "#");
    expect(snippet).toContain("valid_from");
    expect(snippet).toContain("valid_to");
  });
});
