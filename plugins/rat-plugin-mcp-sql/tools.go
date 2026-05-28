package main

// tools wires this plugin's MCP tools to ratd's query endpoint. All tools
// are READ-ONLY by construction: every SQL the model submits is passed
// through isReadOnly() and rejected if it isn't a pure SELECT/WITH/SHOW/
// EXPLAIN/DESCRIBE. The model never gets a chance to drop a table.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Hard cap on rows returned to the model. Anything more than this pollutes
// the context window without adding signal — and DuckDB's query path will
// happily return millions of rows otherwise.
const defaultRowLimit = 200
const maxRowLimit = 1000

func registerTools(s *Server, r *ratdClient) {
	s.Add(Tool{
		Name: "run_query",
		Description: "Execute a READ-ONLY SQL query (SELECT / WITH / SHOW / EXPLAIN / DESCRIBE) " +
			"against the warehouse via DuckDB. Returns columns + rows. " +
			"Reference tables as namespace.layer.name (e.g. shop.bronze.orders). " +
			"Anything that mutates (INSERT, UPDATE, DELETE, CREATE, DROP, ALTER) is rejected.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sql": map[string]any{
					"type":        "string",
					"description": "The SQL statement. Must be a single read-only statement.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Optional row limit (default %d, max %d).", defaultRowLimit, maxRowLimit),
				},
			},
			"required": []string{"sql"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			sql, err := argString(args, "sql")
			if err != nil {
				return "", err
			}
			if err := assertReadOnly(sql); err != nil {
				return "", err
			}
			limit := argIntOpt(args, "limit", defaultRowLimit)
			if limit <= 0 || limit > maxRowLimit {
				limit = defaultRowLimit
			}
			res, err := r.executeQuery(ctx, sql, limit)
			if err != nil {
				return "", err
			}
			return formatResult(sql, res), nil
		},
	})

	s.Add(Tool{
		Name:        "sample_table",
		Description: "Return a few sample rows from a table — the fastest way for the model to see what real data looks like before querying.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string"},
				"layer":     map[string]any{"type": "string", "enum": []string{"bronze", "silver", "gold"}},
				"name":      map[string]any{"type": "string"},
				"limit":     map[string]any{"type": "integer", "description": "Number of rows (default 10, max 50)."},
			},
			"required": []string{"namespace", "layer", "name"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			ns, err := argString(args, "namespace")
			if err != nil {
				return "", err
			}
			layer, err := argString(args, "layer")
			if err != nil {
				return "", err
			}
			name, err := argString(args, "name")
			if err != nil {
				return "", err
			}
			limit := argIntOpt(args, "limit", 10)
			if limit <= 0 || limit > 50 {
				limit = 10
			}
			sql := fmt.Sprintf(`SELECT * FROM %s.%s.%s LIMIT %d`, ns, layer, name, limit)
			res, err := r.executeQuery(ctx, sql, limit)
			if err != nil {
				return "", err
			}
			return formatResult(sql, res), nil
		},
	})

	s.Add(Tool{
		Name:        "explain_query",
		Description: "Return DuckDB's query plan (EXPLAIN) for a SQL statement, without executing it. Useful before running expensive aggregations.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sql": map[string]any{"type": "string"},
			},
			"required": []string{"sql"},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			sql, err := argString(args, "sql")
			if err != nil {
				return "", err
			}
			if err := assertReadOnly(sql); err != nil {
				return "", err
			}
			plan, err := r.executeQuery(ctx, "EXPLAIN "+sql, 1000)
			if err != nil {
				return "", err
			}
			return formatResult("EXPLAIN "+sql, plan), nil
		},
	})
}

// ── Read-only enforcement ──────────────────────────────────────────

// Allowed leading verbs. Anything else is rejected before the SQL is sent
// to ratd. We strip leading whitespace and comments before checking.
var allowedVerb = regexp.MustCompile(`(?i)^\s*(SELECT|WITH|SHOW|EXPLAIN|DESCRIBE|PRAGMA)\b`)

// Forbidden tokens anywhere in the statement — even inside a CTE, a hostile
// model could prepend DDL/DML. This is belt-and-braces alongside the leading
// verb check.
var forbidden = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|MERGE|CREATE|DROP|ALTER|TRUNCATE|GRANT|REVOKE|COPY|ATTACH|INSTALL|LOAD|CALL|VACUUM|CHECKPOINT|EXPORT|IMPORT)\b`)

// stripCommentsAndSemis trims SQL comments + trailing semicolons so the
// regex checks aren't fooled by `-- INSERT` or `; DROP`.
func stripCommentsAndSemis(sql string) string {
	// Strip line comments.
	lines := strings.Split(sql, "\n")
	for i, ln := range lines {
		if idx := strings.Index(ln, "--"); idx >= 0 {
			lines[i] = ln[:idx]
		}
	}
	out := strings.Join(lines, "\n")
	// Strip /* ... */ blocks.
	for {
		start := strings.Index(out, "/*")
		if start < 0 {
			break
		}
		end := strings.Index(out[start:], "*/")
		if end < 0 {
			out = out[:start]
			break
		}
		out = out[:start] + out[start+end+2:]
	}
	return strings.TrimRight(strings.TrimSpace(out), ";")
}

func assertReadOnly(sql string) error {
	cleaned := stripCommentsAndSemis(sql)
	if cleaned == "" {
		return fmt.Errorf("SQL is empty")
	}
	if strings.Contains(cleaned, ";") {
		return fmt.Errorf("multiple statements are not allowed (only one read-only statement per call)")
	}
	if !allowedVerb.MatchString(cleaned) {
		return fmt.Errorf("only read-only statements are allowed (SELECT, WITH, SHOW, EXPLAIN, DESCRIBE, PRAGMA)")
	}
	if forbidden.MatchString(cleaned) {
		return fmt.Errorf("statement contains a forbidden keyword (INSERT/UPDATE/DELETE/CREATE/DROP/ALTER/...)")
	}
	return nil
}

// ── Result formatting for the LLM ─────────────────────────────────

// formatResult returns a compact, model-friendly summary. We pretty-print
// the columns + rows as JSON so it stays valid and parseable, and prepend a
// one-line summary so the model doesn't need to count.
func formatResult(sql string, r *QueryResult) string {
	colNames := make([]string, 0, len(r.Columns))
	for _, c := range r.Columns {
		if n, ok := c["name"].(string); ok {
			colNames = append(colNames, n)
		}
	}
	header := fmt.Sprintf("%d rows × %d columns in %dms\ncolumns: %s",
		r.TotalRows, len(r.Columns), r.DurationMs, strings.Join(colNames, ", "))
	return header + "\n\n" + jsonString(map[string]any{
		"columns": r.Columns,
		"rows":    r.Rows,
	})
}
