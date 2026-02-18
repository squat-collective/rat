/**
 * Client-side annotation parser — TypeScript port of
 * runner/src/rat_runner/templating.py:extract_metadata().
 *
 * Parses `-- @key: value` (SQL) and `# @key: value` (Python) annotation
 * headers from pipeline source code. Code annotations are the single source
 * of truth for merge strategy; the UI displays them read-only.
 */

import type { MergeStrategy } from "@squat-collective/rat-client";

/** Parsed strategy-related annotations from pipeline source code. */
export interface ParsedStrategyConfig {
  merge_strategy: MergeStrategy | null;
  unique_key: string[] | null;
  watermark_column: string | null;
  partition_column: string | null;
  scd_valid_from: string | null;
  scd_valid_to: string | null;
  materialized: string | null;
  archive_landing_zones: boolean | null;
  /** All annotations found (including non-strategy ones like description). */
  raw: Record<string, string>;
}

const ANNOTATION_RE = /^(?:--|#)\s*@(\w+):\s*(.+)$/;

const VALID_STRATEGIES = new Set<string>([
  "full_refresh", "incremental", "append_only", "delete_insert", "scd2", "snapshot",
]);

/**
 * Extract @key: value annotations from pipeline source code.
 *
 * Mirrors the Python extract_metadata() behavior:
 * - Parses `-- @key: value` (SQL) and `# @key: value` (Python)
 * - Stops at first non-comment, non-empty line
 * - Returns null for unset fields (distinguishes "not set" from "set to default")
 */
export function extractAnnotations(source: string): ParsedStrategyConfig {
  const raw: Record<string, string> = {};

  for (const line of source.split("\n")) {
    const stripped = line.trim();
    const match = stripped.match(ANNOTATION_RE);
    if (match) {
      raw[match[1]] = match[2].trim();
    } else if (stripped && !stripped.startsWith("--") && !stripped.startsWith("#")) {
      break; // stop at first non-comment, non-empty line
    }
  }

  const mergeRaw = raw["merge_strategy"] ?? null;
  const mergeStrategy = mergeRaw && VALID_STRATEGIES.has(mergeRaw)
    ? (mergeRaw as MergeStrategy)
    : mergeRaw
      ? (mergeRaw as MergeStrategy) // pass through unknown values for display with warning
      : null;

  const uniqueKeyRaw = raw["unique_key"] ?? null;
  const uniqueKey = uniqueKeyRaw
    ? uniqueKeyRaw.split(",").map((k) => k.trim()).filter(Boolean)
    : null;

  const archiveRaw = raw["archive_landing_zones"] ?? null;
  const archiveLandingZones = archiveRaw !== null
    ? archiveRaw.toLowerCase() === "true"
    : null;

  return {
    merge_strategy: mergeStrategy,
    unique_key: uniqueKey,
    watermark_column: raw["watermark_column"] ?? null,
    partition_column: raw["partition_column"] ?? null,
    scd_valid_from: raw["scd_valid_from"] ?? null,
    scd_valid_to: raw["scd_valid_to"] ?? null,
    materialized: raw["materialized"] ?? null,
    archive_landing_zones: archiveLandingZones,
    raw,
  };
}

/** Fields required/optional per strategy (matches FIELD_VISIBILITY). */
const STRATEGY_FIELDS: Record<MergeStrategy, string[]> = {
  full_refresh: [],
  incremental: ["unique_key", "watermark_column"],
  append_only: [],
  delete_insert: ["unique_key"],
  scd2: ["unique_key", "scd_valid_from", "scd_valid_to"],
  snapshot: ["partition_column"],
};

/** Placeholder values shown in generated snippets. */
const FIELD_PLACEHOLDERS: Record<string, string> = {
  unique_key: "id, email",
  watermark_column: "updated_at",
  partition_column: "event_date",
  scd_valid_from: "valid_from",
  scd_valid_to: "valid_to",
};

/**
 * Generate a copy-paste annotation block for a given strategy.
 *
 * @param strategy - The merge strategy to generate a snippet for
 * @param commentPrefix - "--" for SQL or "#" for Python
 */
export function generateStrategySnippet(
  strategy: MergeStrategy,
  commentPrefix: "--" | "#",
): string {
  const lines = [`${commentPrefix} @merge_strategy: ${strategy}`];
  const fields = STRATEGY_FIELDS[strategy] ?? [];
  for (const field of fields) {
    const placeholder = FIELD_PLACEHOLDERS[field] ?? "value";
    lines.push(`${commentPrefix} @${field}: ${placeholder}`);
  }
  return lines.join("\n");
}
