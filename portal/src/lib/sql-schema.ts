import type {
  Completion,
  CompletionContext,
  CompletionSource,
} from "@codemirror/autocomplete";
import { linter, type Diagnostic } from "@codemirror/lint";

// SchemaData: namespace → layer → table → { column: type }
export type SchemaData = Record<
  string,
  Record<string, Record<string, Record<string, string>>>
>;

// Build a CodeMirror-compatible schema object from SchemaData.
// Adds short-form aliases (layer.table) for the first namespace.
export function buildCmSchema(schema: SchemaData) {
  const result: Record<string, unknown> = {};
  const colNames = (cols: Record<string, string>) => Object.keys(cols);

  for (const ns of Object.keys(schema)) {
    result[ns] = {};
    for (const layer of Object.keys(schema[ns])) {
      (result[ns] as Record<string, unknown>)[layer] = {};
      for (const table of Object.keys(schema[ns][layer])) {
        (
          (result[ns] as Record<string, unknown>)[layer] as Record<
            string,
            unknown
          >
        )[table] = colNames(schema[ns][layer][table]);
      }
    }
  }

  const namespaces = Object.keys(schema);
  if (namespaces.length > 0) {
    const firstNs = namespaces[0];
    for (const layer of Object.keys(schema[firstNs])) {
      if (!result[layer]) result[layer] = {};
      for (const table of Object.keys(schema[firstNs][layer])) {
        (result[layer] as Record<string, unknown>)[table] = colNames(
          schema[firstNs][layer][table],
        );
      }
    }
  }

  return result;
}

type AliasMode = "jinja" | "plain";

interface AliasInfo {
  tableName: string;
  columns: Record<string, string>;
}

const SQL_KEYWORDS = new Set([
  "ON", "WHERE", "AND", "OR", "SET", "INTO", "VALUES", "GROUP",
  "ORDER", "HAVING", "LIMIT", "OFFSET", "UNION", "EXCEPT", "INTERSECT",
  "INNER", "OUTER", "LEFT", "RIGHT", "CROSS", "FULL", "NATURAL",
  "JOIN", "FROM", "SELECT", "AS", "WHEN", "THEN", "ELSE", "END",
  "CASE", "NOT", "IN", "IS", "NULL", "LIKE", "BETWEEN", "EXISTS",
  "WITH", "RECURSIVE", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP",
  "ALTER", "TABLE", "INDEX", "VIEW",
]);

// Resolve a table reference (e.g. "layer.table" or "ns.layer.table" or bare "table")
// to its columns from the schema.
function resolveTableColumns(
  ref: string,
  schema: SchemaData,
): Record<string, string> | null {
  const parts = ref.split(".");
  const namespaces = Object.keys(schema);

  if (parts.length === 3) {
    // ns.layer.table
    return schema[parts[0]]?.[parts[1]]?.[parts[2]] ?? null;
  }

  if (parts.length === 2) {
    // layer.table — search across namespaces
    for (const ns of namespaces) {
      const cols = schema[ns]?.[parts[0]]?.[parts[1]];
      if (cols) return cols;
    }
    return null;
  }

  if (parts.length === 1) {
    // bare table name — search all namespaces and layers
    for (const ns of namespaces) {
      for (const layer of Object.keys(schema[ns])) {
        const cols = schema[ns][layer][parts[0]];
        if (cols) return cols;
      }
    }
    return null;
  }

  return null;
}

// Extract table-alias mappings from SQL text.
export function extractAliases(
  sqlText: string,
  mode: AliasMode,
  schema: SchemaData,
): Map<string, AliasInfo> {
  const aliases = new Map<string, AliasInfo>();

  if (mode === "jinja") {
    // Match {{ ref('layer.table') }} alias or {{ ref('layer.table') }} AS alias
    const jinjaRe =
      /\{\{\s*ref\(\s*['"]([^'"]+)['"]\s*\)\s*\}\}\s+(?:AS\s+)?(\w+)/gi;
    let m;
    while ((m = jinjaRe.exec(sqlText)) !== null) {
      const tableRef = m[1];
      const alias = m[2];
      if (SQL_KEYWORDS.has(alias.toUpperCase())) continue;
      const columns = resolveTableColumns(tableRef, schema);
      if (columns) {
        aliases.set(alias, { tableName: tableRef, columns });
      }
    }
  }

  // Plain SQL: FROM/JOIN table alias (works in both modes)
  const fromJoinRe =
    /(?:FROM|JOIN)\s+(\w+(?:\.\w+)*)\s+(?:AS\s+)?(\w+)/gi;
  let m;
  while ((m = fromJoinRe.exec(sqlText)) !== null) {
    const tableRef = m[1];
    const alias = m[2];
    if (SQL_KEYWORDS.has(alias.toUpperCase())) continue;
    if (aliases.has(alias)) continue; // jinja match takes priority
    const columns = resolveTableColumns(tableRef, schema);
    if (columns) {
      aliases.set(alias, { tableName: tableRef, columns });
    }
  }

  return aliases;
}

// CompletionSource that offers column completions when typing `alias.`
export function aliasColumnCompletion(
  schema: SchemaData,
  mode: AliasMode,
): CompletionSource {
  return (context: CompletionContext) => {
    // Match word.word* at cursor (alias dot prefix)
    const match = context.matchBefore(/(\w+)\.\w*/);
    if (!match) return null;

    const dotIdx = match.text.indexOf(".");
    if (dotIdx === -1) return null;
    const prefix = match.text.slice(0, dotIdx);

    const fullText = context.state.doc.toString();
    const aliases = extractAliases(fullText, mode, schema);

    const info = aliases.get(prefix);
    if (!info) return null;

    const from = match.from + dotIdx + 1;
    const options: Completion[] = Object.entries(info.columns).map(
      ([col, colType]) => ({
        label: col,
        type: "property" as const,
        detail: colType,
        boost: 1,
      }),
    );

    return { from, options };
  };
}

// Linter that flags unknown `alias.column` references.
// Conservative: only flags when the alias IS recognized but the column IS NOT.
export function columnLinter(schema: SchemaData, mode: AliasMode) {
  return linter(
    (view) => {
      const diagnostics: Diagnostic[] = [];
      const text = view.state.doc.toString();
      const aliases = extractAliases(text, mode, schema);

      if (aliases.size === 0) return diagnostics;

      // Find all alias.column patterns
      const aliasColRe = /\b(\w+)\.(\w+)\b/g;
      let m;
      while ((m = aliasColRe.exec(text)) !== null) {
        const alias = m[1];
        const column = m[2];
        const info = aliases.get(alias);
        if (!info) continue; // unknown alias — don't flag (conservative)

        // Skip if inside a Jinja block {{ ... }}
        const before = text.slice(0, m.index);
        const lastOpen = before.lastIndexOf("{{");
        const lastClose = before.lastIndexOf("}}");
        if (lastOpen > lastClose) continue;

        // Skip if inside a string literal (simple heuristic: odd number of quotes before)
        const singleQuotes = (before.match(/'/g) || []).length;
        const doubleQuotes = (before.match(/"/g) || []).length;
        if (singleQuotes % 2 !== 0 || doubleQuotes % 2 !== 0) continue;

        if (!info.columns[column]) {
          diagnostics.push({
            from: m.index + alias.length + 1,
            to: m.index + m[0].length,
            severity: "warning",
            source: "rat",
            message: `Unknown column "${column}" on ${info.tableName}`,
          });
        }
      }

      return diagnostics;
    },
    { delay: 750 },
  );
}
