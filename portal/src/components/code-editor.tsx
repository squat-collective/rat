"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { EditorView, keymap, hoverTooltip } from "@codemirror/view";
import { EditorState } from "@codemirror/state";
import { sql } from "@codemirror/lang-sql";
import { python } from "@codemirror/lang-python";
import { yaml } from "@codemirror/lang-yaml";
import { oneDark } from "@codemirror/theme-one-dark";
import { basicSetup } from "codemirror";
import type {
  Completion,
  CompletionContext,
  CompletionSource,
} from "@codemirror/autocomplete";
import { snippetCompletion } from "@codemirror/autocomplete";
import { indentUnit } from "@codemirror/language";
import { indentWithTab } from "@codemirror/commands";
import {
  type SchemaData,
  buildCmSchema,
  aliasColumnCompletion,
  columnLinter,
} from "@/lib/sql-schema";

interface CodeEditorProps {
  content: string;
  language: string;
  activeTab: string | null;
  onContentChange: (content: string) => void;
  onSaveRef: React.MutableRefObject<() => void>;
  onPreviewRef?: React.MutableRefObject<() => void>;
  getSelectionRef?: React.MutableRefObject<() => string>;
  landingZones?: string[];
  schema?: SchemaData;
}

// -- Signature docs (shared between hover tooltips + autocomplete info) --
const SIGNATURE_DOCS: Record<
  string,
  { signature: string; description: string }
> = {
  // Jinja / pipeline functions
  ref: {
    signature: "ref(table_ref) → iceberg_scan()",
    description:
      "Resolves a table reference to its Iceberg scan path.\nAccepts 'layer.name' or 'namespace.layer.name' format.",
  },
  landing_zone: {
    signature: "landing_zone(zone_name) → str",
    description:
      "Returns the S3 landing zone path for the given zone name.\nUsed to reference uploaded files in pipeline SQL.",
  },
  is_incremental: {
    signature: "is_incremental() → bool",
    description:
      "Returns True if the pipeline uses incremental merge strategy.\nUse inside {% if is_incremental() %} blocks.",
  },
  is_scd2: {
    signature: "is_scd2() → bool",
    description:
      "Returns True if the pipeline uses SCD Type 2 merge strategy.\nUse inside {% if is_scd2() %} blocks.",
  },
  is_snapshot: {
    signature: "is_snapshot() → bool",
    description:
      "Returns True if the pipeline uses snapshot (partition-aware) strategy.\nUse inside {% if is_snapshot() %} blocks.",
  },
  is_append_only: {
    signature: "is_append_only() → bool",
    description:
      "Returns True if the pipeline uses append-only merge strategy.\nUse inside {% if is_append_only() %} blocks.",
  },
  is_delete_insert: {
    signature: "is_delete_insert() → bool",
    description:
      "Returns True if the pipeline uses delete+insert merge strategy.\nUse inside {% if is_delete_insert() %} blocks.",
  },
  // Pipeline variables
  this: {
    signature: "this → str",
    description:
      "Fully qualified identifier for the current pipeline's output table.",
  },
  run_started_at: {
    signature: "run_started_at → str",
    description: "ISO 8601 timestamp of when the current run started.",
  },
  watermark_value: {
    signature: "watermark_value → str | None",
    description:
      "Maximum value of the watermark column from the last successful run.",
  },
  // Python globals
  duckdb_conn: {
    signature: "duckdb_conn: DuckDBPyConnection",
    description:
      "Pre-configured DuckDB connection with S3 and Iceberg extensions loaded.",
  },
  pa: {
    signature: "pa: pyarrow",
    description: "PyArrow module for Arrow table manipulation.",
  },
  config: {
    signature: "config: PipelineConfig",
    description:
      "Pipeline configuration object with merge_strategy, unique_key, watermark_column, etc.",
  },
  result: {
    signature: "result: pa.Table",
    description:
      "Set this variable to a PyArrow Table to write pipeline output.",
  },
  // DuckDB functions
  read_csv_auto: {
    signature: "read_csv_auto(path, ...) → Table",
    description:
      "Reads CSV files with automatic delimiter and type detection.",
  },
  read_parquet: {
    signature: "read_parquet(path, ...) → Table",
    description: "Reads Parquet files into a DuckDB table.",
  },
  read_json_auto: {
    signature: "read_json_auto(path, ...) → Table",
    description: "Reads JSON files with automatic structure detection.",
  },
  iceberg_scan: {
    signature: "iceberg_scan(path) → Table",
    description: "Scans an Apache Iceberg table.",
  },
  list_files: {
    signature: "list_files(path) → Table",
    description: "Lists files in a directory (local or S3).",
  },
  parquet_metadata: {
    signature: "parquet_metadata(path) → Table",
    description:
      "Returns metadata about a Parquet file (row groups, columns).",
  },
  parquet_schema: {
    signature: "parquet_schema(path) → Table",
    description: "Returns the schema of a Parquet file.",
  },
  unnest: {
    signature: "unnest(list) → rows",
    description: "Expands a list column into individual rows.",
  },
  struct_pack: {
    signature: "struct_pack(k := v, ...) → struct",
    description: "Creates a struct from key-value pairs.",
  },
  list_aggregate: {
    signature: "list_aggregate(list, name) → scalar",
    description: "Runs an aggregate function over a list.",
  },
  epoch_ms: {
    signature: "epoch_ms(ms) → timestamp",
    description: "Converts milliseconds since epoch to a timestamp.",
  },
  strftime: {
    signature: "strftime(fmt, ts) → str",
    description: "Formats a timestamp using the given format string.",
  },
  strptime: {
    signature: "strptime(str, fmt) → timestamp",
    description: "Parses a string into a timestamp using the given format.",
  },
  regexp_extract: {
    signature: "regexp_extract(str, regex, group) → str",
    description: "Extracts a substring matching a regex group.",
  },
  regexp_matches: {
    signature: "regexp_matches(str, regex) → bool",
    description: "Tests if a string matches a regular expression.",
  },
  list_transform: {
    signature: "list_transform(list, lambda) → list",
    description: "Applies a lambda function to each element of a list.",
  },
  list_filter: {
    signature: "list_filter(list, lambda) → list",
    description: "Filters a list using a lambda predicate.",
  },
  generate_series: {
    signature: "generate_series(start, stop, step) → Table",
    description: "Generates a series of numbers from start to stop.",
  },
  range: {
    signature: "range(start, stop, step) → Table",
    description: "Generates a range of numbers (exclusive end).",
  },
  columns: {
    signature: "columns(regex) → columns",
    description: "Selects columns by regex pattern match.",
  },
};

function infoText(label: string): string | undefined {
  const doc = SIGNATURE_DOCS[label];
  if (!doc) return undefined;
  return `${doc.signature}\n\n${doc.description}`;
}

// -- SQL (Jinja) template context --
const SQL_CONTEXT: Completion[] = [
  snippetCompletion("ref('${1:layer.name}')", {
    label: "ref",
    type: "function",
    detail: "ref('layer.name') — Iceberg table scan",
    info: infoText("ref"),
  }),
  snippetCompletion("landing_zone('${1:zone_name}')", {
    label: "landing_zone",
    type: "function",
    detail: "landing_zone('name') — S3 landing path",
    info: infoText("landing_zone"),
  }),
  {
    label: "this",
    type: "variable",
    detail: "current pipeline table identifier",
    info: infoText("this"),
  },
  {
    label: "run_started_at",
    type: "variable",
    detail: "ISO timestamp of current run",
    info: infoText("run_started_at"),
  },
  snippetCompletion("is_incremental()", {
    label: "is_incremental",
    type: "function",
    detail: "is_incremental() — True if merge strategy",
    info: infoText("is_incremental"),
  }),
  snippetCompletion("is_scd2()", {
    label: "is_scd2",
    type: "function",
    detail: "is_scd2() — True if SCD2 strategy",
    info: infoText("is_scd2"),
  }),
  snippetCompletion("is_snapshot()", {
    label: "is_snapshot",
    type: "function",
    detail: "is_snapshot() — True if snapshot strategy",
    info: infoText("is_snapshot"),
  }),
  snippetCompletion("is_append_only()", {
    label: "is_append_only",
    type: "function",
    detail: "is_append_only() — True if append-only strategy",
    info: infoText("is_append_only"),
  }),
  snippetCompletion("is_delete_insert()", {
    label: "is_delete_insert",
    type: "function",
    detail: "is_delete_insert() — True if delete+insert strategy",
    info: infoText("is_delete_insert"),
  }),
  {
    label: "watermark_value",
    type: "variable",
    detail: "max watermark column value",
    info: infoText("watermark_value"),
  },
];

// -- Python pipeline globals --
const PYTHON_CONTEXT: Completion[] = [
  {
    label: "duckdb_conn",
    type: "variable",
    detail: "DuckDB connection (S3-configured)",
    info: infoText("duckdb_conn"),
  },
  {
    label: "pa",
    type: "variable",
    detail: "PyArrow module",
    info: infoText("pa"),
  },
  snippetCompletion("ref('${1:layer.name}')", {
    label: "ref",
    type: "function",
    detail: "ref('layer.name') — Iceberg table path",
    info: infoText("ref"),
  }),
  snippetCompletion("landing_zone('${1:zone_name}')", {
    label: "landing_zone",
    type: "function",
    detail: "landing_zone('name') — S3 landing path",
    info: infoText("landing_zone"),
  }),
  {
    label: "this",
    type: "variable",
    detail: "current pipeline table identifier",
    info: infoText("this"),
  },
  {
    label: "run_started_at",
    type: "variable",
    detail: "ISO timestamp of current run",
    info: infoText("run_started_at"),
  },
  snippetCompletion("is_incremental()", {
    label: "is_incremental",
    type: "function",
    detail: "is_incremental() — True if merge strategy",
    info: infoText("is_incremental"),
  }),
  snippetCompletion("is_scd2()", {
    label: "is_scd2",
    type: "function",
    detail: "is_scd2() — True if SCD2 strategy",
    info: infoText("is_scd2"),
  }),
  snippetCompletion("is_snapshot()", {
    label: "is_snapshot",
    type: "function",
    detail: "is_snapshot() — True if snapshot strategy",
    info: infoText("is_snapshot"),
  }),
  snippetCompletion("is_append_only()", {
    label: "is_append_only",
    type: "function",
    detail: "is_append_only() — True if append-only strategy",
    info: infoText("is_append_only"),
  }),
  snippetCompletion("is_delete_insert()", {
    label: "is_delete_insert",
    type: "function",
    detail: "is_delete_insert() — True if delete+insert strategy",
    info: infoText("is_delete_insert"),
  }),
  {
    label: "config",
    type: "variable",
    detail: "PipelineConfig (merge_strategy, unique_key, ...)",
    info: infoText("config"),
  },
  {
    label: "result",
    type: "variable",
    detail: "set to PyArrow Table for output",
    info: infoText("result"),
  },
];

// -- DuckDB built-in functions --
const DUCKDB_FUNCTIONS: Completion[] = [
  {
    label: "read_csv_auto",
    type: "function",
    detail: "read_csv_auto(path) — CSV reader",
    info: infoText("read_csv_auto"),
  },
  {
    label: "read_parquet",
    type: "function",
    detail: "read_parquet(path) — Parquet reader",
    info: infoText("read_parquet"),
  },
  {
    label: "read_json_auto",
    type: "function",
    detail: "read_json_auto(path) — JSON reader",
    info: infoText("read_json_auto"),
  },
  {
    label: "iceberg_scan",
    type: "function",
    detail: "iceberg_scan(path) — Iceberg scan",
    info: infoText("iceberg_scan"),
  },
  {
    label: "list_files",
    type: "function",
    detail: "list_files(path) — List files in directory",
    info: infoText("list_files"),
  },
  {
    label: "parquet_metadata",
    type: "function",
    detail: "parquet_metadata(path) — Parquet file metadata",
    info: infoText("parquet_metadata"),
  },
  {
    label: "parquet_schema",
    type: "function",
    detail: "parquet_schema(path) — Parquet file schema",
    info: infoText("parquet_schema"),
  },
  {
    label: "unnest",
    type: "function",
    detail: "unnest(list) — Expand list to rows",
    info: infoText("unnest"),
  },
  {
    label: "struct_pack",
    type: "function",
    detail: "struct_pack(k := v, ...) — Create struct",
    info: infoText("struct_pack"),
  },
  {
    label: "list_aggregate",
    type: "function",
    detail: "list_aggregate(list, name) — Aggregate over list",
    info: infoText("list_aggregate"),
  },
  {
    label: "epoch_ms",
    type: "function",
    detail: "epoch_ms(ms) — Milliseconds to timestamp",
    info: infoText("epoch_ms"),
  },
  {
    label: "strftime",
    type: "function",
    detail: "strftime(fmt, ts) — Format timestamp",
    info: infoText("strftime"),
  },
  {
    label: "strptime",
    type: "function",
    detail: "strptime(str, fmt) — Parse timestamp",
    info: infoText("strptime"),
  },
  {
    label: "regexp_extract",
    type: "function",
    detail: "regexp_extract(str, regex, group) — Regex extract",
    info: infoText("regexp_extract"),
  },
  {
    label: "regexp_matches",
    type: "function",
    detail: "regexp_matches(str, regex) — Regex match test",
    info: infoText("regexp_matches"),
  },
  {
    label: "list_transform",
    type: "function",
    detail: "list_transform(list, lambda) — Map over list",
    info: infoText("list_transform"),
  },
  {
    label: "list_filter",
    type: "function",
    detail: "list_filter(list, lambda) — Filter list",
    info: infoText("list_filter"),
  },
  {
    label: "generate_series",
    type: "function",
    detail: "generate_series(start, stop, step) — Number series",
    info: infoText("generate_series"),
  },
  {
    label: "range",
    type: "function",
    detail: "range(start, stop, step) — Number range",
    info: infoText("range"),
  },
  {
    label: "columns",
    type: "function",
    detail: "columns(regex) — Select columns by pattern",
    info: infoText("columns"),
  },
];

// -- Annotation schema (pipeline config + quality test annotations) --
const ANNOTATION_SCHEMA: Record<string, string[] | null> = {
  merge_strategy: ["full_refresh", "incremental", "append_only", "delete_insert", "scd2", "snapshot"],
  materialized: ["table", "view"],
  unique_key: null,
  watermark_column: null,
  partition_column: null,
  scd_valid_from: null,
  scd_valid_to: null,
  description: null,
  archive_landing_zones: ["true", "false"],
  severity: ["error", "warn"],
  tags: ["completeness", "accuracy", "consistency", "freshness", "uniqueness", "validity"],
  remediation: null,
};

function pipelineContextCompletion(lang: string): CompletionSource {
  const items =
    lang === "python" ? PYTHON_CONTEXT : [...SQL_CONTEXT, ...DUCKDB_FUNCTIONS];
  return (context: CompletionContext) => {
    const word = context.matchBefore(/\w+/);
    if (!word || word.text.length < 2) return null;

    return {
      from: word.from,
      options: items,
      filter: true,
    };
  };
}

function landingZoneNameCompletion(zones: string[]): CompletionSource {
  return (context: CompletionContext) => {
    const match = context.matchBefore(/landing_zone\(\s*['"][\w-]*/);
    if (!match) return null;

    const quoteIdx = match.text.search(/['"][^'"]*$/);
    if (quoteIdx === -1) return null;
    const from = match.from + quoteIdx + 1;

    return {
      from,
      options: zones.map((z) => ({
        label: z,
        type: "variable" as const,
        detail: "landing zone",
      })),
    };
  };
}

function refTableCompletion(schema: SchemaData | undefined): CompletionSource {
  return (context: CompletionContext) => {
    if (!schema) return null;

    // Match ref('...' or ref("...  (cursor inside the string)
    const match = context.matchBefore(/ref\(\s*['"][\w.]*/);
    if (!match) return null;

    const quoteIdx = match.text.search(/['"][^'"]*$/);
    if (quoteIdx === -1) return null;
    const from = match.from + quoteIdx + 1;

    // Build layer.table completions from the schema
    const options: Completion[] = [];
    for (const ns of Object.keys(schema)) {
      for (const layer of Object.keys(schema[ns])) {
        for (const table of Object.keys(schema[ns][layer])) {
          options.push({
            label: `${layer}.${table}`,
            type: "variable" as const,
            detail: `${ns}.${layer}.${table}`,
            boost: 1,
          });
        }
      }
    }

    return { from, options };
  };
}

function annotationCompletion(): CompletionSource {
  return (context: CompletionContext) => {
    // Value completion: -- @key: val or # @key: val
    const valueMatch = context.matchBefore(/(--|#)\s*@(\w+):\s*\w*/);
    if (valueMatch) {
      const m = valueMatch.text.match(/@(\w+):\s*/);
      if (m) {
        const key = m[1];
        const values = ANNOTATION_SCHEMA[key];
        if (values) {
          const keyColonEnd = valueMatch.text.indexOf(m[0]) + m[0].length;
          const from = valueMatch.from + keyColonEnd;
          return {
            from,
            options: values.map((v) => ({
              label: v,
              type: "enum" as const,
              detail: `${key} value`,
            })),
          };
        }
      }
    }

    // Key completion: -- @word or # @word
    const keyMatch = context.matchBefore(/(--|#)\s*@\w*/);
    if (!keyMatch) return null;
    const atIdx = keyMatch.text.indexOf("@");
    if (atIdx === -1) return null;
    const from = keyMatch.from + atIdx + 1;

    return {
      from,
      options: Object.keys(ANNOTATION_SCHEMA).map((key) => {
        const values = ANNOTATION_SCHEMA[key];
        return snippetCompletion(`${key}: \${}`, {
          label: key,
          type: "keyword",
          detail: values ? `enum: ${values.join(", ")}` : "free text",
        });
      }),
    };
  };
}

// -- Hover tooltip for function signatures --
const functionSignatureTooltip = hoverTooltip((view, pos) => {
  const word = view.state.wordAt(pos);
  if (!word) return null;

  const text = view.state.sliceDoc(word.from, word.to);
  const doc = SIGNATURE_DOCS[text];
  if (!doc) return null;

  return {
    pos: word.from,
    end: word.to,
    above: true,
    create: () => {
      const dom = document.createElement("div");
      dom.className = "cm-signature-tooltip";

      const sig = document.createElement("div");
      sig.className = "cm-signature-sig";
      sig.textContent = doc.signature;
      dom.appendChild(sig);

      const desc = document.createElement("div");
      desc.className = "cm-signature-desc";
      desc.textContent = doc.description;
      dom.appendChild(desc);

      return { dom };
    },
  };
});

const tooltipTheme = EditorView.theme({
  ".cm-signature-tooltip": {
    padding: "6px 10px",
    maxWidth: "400px",
  },
  ".cm-signature-sig": {
    fontFamily: "monospace",
    fontSize: "13px",
    color: "#e5c07b",
    marginBottom: "4px",
  },
  ".cm-signature-desc": {
    fontSize: "12px",
    color: "#abb2bf",
    whiteSpace: "pre-wrap",
  },
});

export function CodeEditor({
  content,
  language,
  activeTab,
  onContentChange,
  onSaveRef,
  onPreviewRef,
  getSelectionRef,
  landingZones,
  schema,
}: CodeEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const [initError, setInitError] = useState<string | null>(null);

  // Keep a stable ref for the onChange callback so extensions can be memoized
  const onContentChangeRef = useRef(onContentChange);
  onContentChangeRef.current = onContentChange;

  const extensions = useMemo(() => {
    const cmSchema = schema ? buildCmSchema(schema) : undefined;

    const langExt =
      language === "python"
        ? python()
        : language === "yaml"
          ? yaml()
          : cmSchema
            ? sql({
                schema: cmSchema as NonNullable<
                  Parameters<typeof sql>[0]
                >["schema"],
                upperCaseKeywords: true,
              })
            : sql();

    const zones = landingZones ?? [];

    // IMPORTANT: Create stable function references ONCE so CodeMirror's
    // autocomplete can track sources across transactions via reference equality.
    const contextSource = pipelineContextCompletion(language);
    const zoneSource = landingZoneNameCompletion(zones);
    const refSource = refTableCompletion(schema);
    const annotationSource = annotationCompletion();

    const isSql = language !== "python" && language !== "yaml";
    const aliasSource = isSql && schema
      ? aliasColumnCompletion(schema, "jinja")
      : null;
    const colLint = isSql && schema
      ? columnLinter(schema, "jinja")
      : null;

    return [
      basicSetup,
      langExt,
      oneDark,
      indentUnit.of("    "),
      EditorState.languageData.of(() => [
        { autocomplete: contextSource },
        { autocomplete: zoneSource },
        { autocomplete: refSource },
        { autocomplete: annotationSource },
        ...(aliasSource ? [{ autocomplete: aliasSource }] : []),
      ]),
      ...(colLint ? [colLint] : []),
      functionSignatureTooltip,
      tooltipTheme,
      keymap.of([
        indentWithTab,
        {
          key: "Mod-s",
          run: () => {
            onSaveRef.current();
            return true;
          },
        },
        {
          key: "Mod-Shift-Enter",
          run: () => {
            onPreviewRef?.current();
            return true;
          },
        },
        {
          key: "Escape",
          run: (view) => {
            view.contentDOM.blur();
            return true;
          },
        },
      ]),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) {
          onContentChangeRef.current(update.state.doc.toString());
        }
      }),
      EditorView.theme({
        "&": { height: "100%" },
        ".cm-scroller": { overflow: "auto" },
      }),
    ];
  }, [language, landingZones, schema, onSaveRef, onPreviewRef]);

  useEffect(() => {
    if (!containerRef.current) return;

    viewRef.current?.destroy();

    try {
      const state = EditorState.create({
        doc: content,
        extensions,
      });

      viewRef.current = new EditorView({
        state,
        parent: containerRef.current,
      });

      if (getSelectionRef) {
        getSelectionRef.current = () => {
          const view = viewRef.current;
          if (!view) return "";
          const { from, to } = view.state.selection.main;
          return view.state.sliceDoc(from, to);
        };
      }
    } catch (err) {
      console.error("[CodeEditor] failed to create editor:", err);
      setInitError(err instanceof Error ? err.message : "Failed to initialize editor");
    }

    return () => {
      viewRef.current?.destroy();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTab, content, extensions]);

  if (initError) {
    return (
      <div className="flex-1 overflow-hidden flex items-center justify-center p-4">
        <div className="text-center space-y-2">
          <p className="text-xs text-destructive font-mono">Editor failed to initialize</p>
          <p className="text-[10px] text-muted-foreground">{initError}</p>
          <textarea
            className="w-full h-48 bg-background border border-border/50 p-2 text-xs font-mono"
            defaultValue={content}
            onChange={(e) => onContentChange(e.target.value)}
          />
        </div>
      </div>
    );
  }

  return <div ref={containerRef} className="flex-1 overflow-hidden" />;
}
