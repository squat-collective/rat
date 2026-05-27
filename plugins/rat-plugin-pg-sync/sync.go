package main

// sync.go is the glue: resolve the secret → render a SQL pipeline →
// stamp pipeline + schedule into ratd. The generated SQL uses DuckDB's
// postgres extension to ATTACH the external database and SELECT.
// The URL is baked into the file (the runner sandbox forbids HTTP
// imports inside Python pipelines, so runtime secret resolution from a
// pipeline isn't possible). Rotating the secret means re-applying the
// table, which regenerates the pipeline file with the new URL.

import (
	"context"
	"fmt"
	"strings"
	"text/template"
)

type syncEngine struct {
	ratd    *ratdClient
	secrets *secretsClient
	store   *store
}

func newSyncEngine(ratd *ratdClient, secrets *secretsClient, st *store) *syncEngine {
	return &syncEngine{ratd: ratd, secrets: secrets, store: st}
}

// pipelineName turns a table-sync row into the deterministic pipeline
// name we use under target_namespace.target_layer. The pipeline name IS
// the Iceberg table name (RAT's convention), so we use the user-chosen
// target_name verbatim — no prefix — to keep the table they see in the
// query editor identical to what they typed.
func pipelineName(t *TableSync) string { return sanitize(t.TargetName) }

func sanitize(s string) string {
	// Pipeline names allow [a-z0-9_]; lower-case + replace anything else.
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// Apply (re)generates the pipeline file + schedule for a table sync.
// Idempotent: safe to call on every create, update, and rotation.
func (e *syncEngine) Apply(ctx context.Context, t *TableSync) error {
	conn, ok := e.store.getConnection(t.Connection)
	if !ok {
		return fmt.Errorf("connection %q not found", t.Connection)
	}
	url, err := e.secrets.Resolve(ctx, conn.SecretName)
	if err != nil {
		return fmt.Errorf("resolve secret %q: %w", conn.SecretName, err)
	}

	sql, err := renderPipelineSQL(t, url)
	if err != nil {
		return fmt.Errorf("render SQL: %w", err)
	}

	if err := e.ratd.CreateNamespace(ctx, t.TargetNamespace); err != nil {
		return err
	}

	pname := pipelineName(t)
	created, err := e.ratd.CreatePipeline(ctx, t.TargetNamespace, t.TargetLayer, pname,
		"sql",
		fmt.Sprintf("pg-sync from %s.%s (%s)", t.SourceSchema, t.SourceTable, t.Mode))
	if err != nil {
		return err
	}
	// The runner looks for the conventional filename `pipeline.sql` (or
	// `pipeline.py`) under the pipeline's S3 dir — not a name-mirrored
	// file. See examples/rat-plugin-demo-loader/install.go for the same
	// convention.
	if err := e.ratd.WriteFile(ctx, strings.TrimRight(created.S3Path, "/")+"/pipeline.sql", sql); err != nil {
		return fmt.Errorf("write pipeline file: %w", err)
	}

	// Replace existing schedules wholesale: simpler than diffing the cron
	// expression and handles the "user changed cadence" path uniformly.
	existing, err := e.ratd.FindSchedulesFor(ctx, t.TargetNamespace, t.TargetLayer, pname)
	if err != nil {
		return fmt.Errorf("list schedules: %w", err)
	}
	for _, s := range existing {
		if err := e.ratd.DeleteSchedule(ctx, s.ID); err != nil {
			return fmt.Errorf("clear old schedule: %w", err)
		}
	}
	if t.Enabled {
		if _, err := e.ratd.CreateSchedule(ctx, t.TargetNamespace, t.TargetLayer, pname, t.Cron, true); err != nil {
			return fmt.Errorf("create schedule: %w", err)
		}
	}
	return nil
}

// Teardown removes the schedule + pipeline file for a table. Best-effort:
// we keep going past individual errors so a half-broken state at least
// gets cleaned up as far as it can be.
func (e *syncEngine) Teardown(ctx context.Context, t *TableSync) error {
	pname := pipelineName(t)
	var firstErr error

	schedules, err := e.ratd.FindSchedulesFor(ctx, t.TargetNamespace, t.TargetLayer, pname)
	if err != nil {
		firstErr = err
	}
	for _, s := range schedules {
		if err := e.ratd.DeleteSchedule(ctx, s.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := e.ratd.DeletePipeline(ctx, t.TargetNamespace, t.TargetLayer, pname); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// SyncNow triggers an immediate run of the generated pipeline. Returns
// the run_id so the UI can deep-link to the run view.
func (e *syncEngine) SyncNow(ctx context.Context, t *TableSync) (string, error) {
	return e.ratd.SubmitRun(ctx, t.TargetNamespace, t.TargetLayer, pipelineName(t), "pg-sync:manual")
}

// ── Pipeline SQL template ──────────────────────────────────────────

// The pipeline body uses DuckDB's postgres extension. The runner allows
// INSTALL + LOAD + ATTACH in SQL pipelines (only Python pipelines run
// in the sandboxed exec()). Multi-statement scripts are fine in DuckDB
// — execute() returns the result of the last statement, which is the
// SELECT we want.
const pipelineTpl = `-- ============================================================================
-- {{ .Pipeline }} — pg-sync from {{ .Source }}
-- ----------------------------------------------------------------------------
-- Auto-generated by rat-plugin-pg-sync. Do not edit by hand — your
-- changes will be overwritten the next time the source table sync is
-- updated. To change the SQL, edit the table sync in /x/pg-sync.
--
-- Connection: {{ .Connection }}   (secret: {{ .SecretName }})
-- Mode:       {{ .Mode }}
{{- if .Watermark }}
-- Watermark:  {{ .Watermark }}
{{- end }}
-- ============================================================================
-- @merge_strategy: {{ .Strategy }}
{{- if .Watermark }}
-- @watermark_column: {{ .Watermark }}
{{- end }}
{{- if .PrimaryKey }}
-- @unique_key: {{ .PrimaryKey }}
{{- end }}
-- @description: pg-sync from {{ .Source }} ({{ .Mode }} mode)

INSTALL postgres;
LOAD postgres;
ATTACH '{{ .URL }}' AS pg_src (TYPE postgres, READ_ONLY true);

{{- if .IsIncremental }}
SELECT * FROM pg_src.{{ .Source }}
{{ "{%" }} if watermark_value {{ "%}" }}
WHERE {{ .Watermark }} > '{{ "{{" }} watermark_value {{ "}}" }}'
{{ "{%" }} endif {{ "%}" }}
;
{{- else }}
SELECT * FROM pg_src.{{ .Source }};
{{- end }}
`

type sqlTemplateData struct {
	Pipeline      string
	Connection    string
	SecretName    string
	Source        string
	URL           string
	Mode          SyncMode
	Strategy      string
	Watermark     string
	PrimaryKey    string
	IsIncremental bool
}

func renderPipelineSQL(t *TableSync, url string) (string, error) {
	// snapshot     → full_refresh: rewrite the whole table each run.
	// incremental  → incremental:  the runner reads MAX(watermark_value)
	//   from the existing iceberg table, our Jinja filters source rows
	//   newer than that, and the runner's incremental strategy dedupes
	//   appended rows by `unique_key` (the user-supplied primary key).
	//   Without unique_key the runner silently falls back to full_refresh,
	//   which is why Validate() requires PrimaryKey for incremental mode.
	strategy := "full_refresh"
	if t.Mode == ModeIncremental {
		strategy = "incremental"
	}
	conn, _ := globalStoreLookup(t.Connection) // safe: caller already validated
	data := sqlTemplateData{
		Pipeline:      pipelineName(t),
		Connection:    t.Connection,
		SecretName:    conn.SecretName,
		Source:        fmt.Sprintf("%s.%s", t.SourceSchema, t.SourceTable),
		URL:           url,
		Mode:          t.Mode,
		Strategy:      strategy,
		Watermark:     t.WatermarkColumn,
		PrimaryKey:    t.PrimaryKey,
		IsIncremental: t.Mode == ModeIncremental,
	}
	tpl, err := template.New("pipeline").Parse(pipelineTpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tpl.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// globalStoreLookup is a tiny shim so the SQL renderer can reach the
// connection's secret name without dragging the whole store through the
// signature chain. Set by main() at boot.
var globalStoreLookup = func(name string) (*Connection, bool) { return nil, false }
