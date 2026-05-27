package main

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"
)

// Installer orchestrates the create-namespace → create-pipelines → write-SQL
// → create-quality-tests → trigger-bronze-runs flow for one demo manifest.
// Each step that 409s ("already exists") is treated as success, so installing
// the same demo twice is safe.
type Installer struct {
	ratd  *ratdClient
	files embed.FS // embedded demo files; paths look like "demos/<id>/<path>"
}

func newInstaller(c *ratdClient, files embed.FS) *Installer {
	return &Installer{ratd: c, files: files}
}

// InstallResult is what the L3 panel renders after a click.
type InstallResult struct {
	DemoID    string   `json:"demo_id"`
	Namespace string   `json:"namespace"`
	Steps     []string `json:"steps"`
	Errors    []string `json:"errors,omitempty"`
}

func (i *Installer) Install(ctx context.Context, m *Manifest, nsOverride string) *InstallResult {
	res := &InstallResult{DemoID: m.ID, Namespace: m.Namespace}
	if strings.TrimSpace(nsOverride) != "" {
		res.Namespace = nsOverride
	}
	ns := res.Namespace

	// 1. Namespace
	if err := i.ratd.CreateNamespace(ctx, ns); err != nil && !isConflict(err) {
		res.Errors = append(res.Errors, fmt.Sprintf("namespace %s: %s", ns, err))
		return res
	}
	res.Steps = append(res.Steps, "namespace "+ns)

	// 2. Pipelines (create + write source file)
	for _, p := range m.Pipelines {
		cp, err := i.ratd.CreatePipeline(ctx, ns, p.Layer, p.Name, p.Type, p.Description)
		if err != nil && !isConflict(err) {
			res.Errors = append(res.Errors, fmt.Sprintf("pipeline %s.%s: %s", p.Layer, p.Name, err))
			continue
		}
		content, err := i.readDemoFile(m.ID, p.File)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("read %s: %s", p.File, err))
			continue
		}
		fileName := "pipeline.sql"
		if p.Type == "python" {
			fileName = "pipeline.py"
		}
		filePath := strings.TrimRight(cp.S3Path, "/") + "/" + fileName
		if err := i.ratd.WriteFile(ctx, filePath, content); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("write %s: %s", filePath, err))
			continue
		}
		res.Steps = append(res.Steps, fmt.Sprintf("pipeline %s.%s", p.Layer, p.Name))
	}

	// 3a. Table metadata. The endpoint stores against a (ns, layer, name)
	// key regardless of whether the Iceberg table itself exists yet — so
	// it's safe to apply at install time, before the first run materialises
	// the table. The descriptions then show up in the Explorer immediately.
	for _, p := range m.Pipelines {
		if p.TableMetadata == nil {
			continue
		}
		err := i.ratd.SetTableMetadata(
			ctx, ns, p.Layer, p.Name,
			p.TableMetadata.Description,
			p.TableMetadata.ColumnDescriptions,
		)
		if err != nil {
			res.Errors = append(res.Errors,
				fmt.Sprintf("metadata %s.%s: %s", p.Layer, p.Name, err))
			continue
		}
		res.Steps = append(res.Steps,
			fmt.Sprintf("metadata %s.%s", p.Layer, p.Name))
	}

	// 3b. Schedules — declared in the manifest, applied at install. The
	// scheduler picks them up live; no restart required.
	for _, s := range m.Schedules {
		// `enabled` defaults to true when the field is omitted; only the
		// explicit `"enabled": false` should disable.
		enabled := true
		if err := i.ratd.CreateSchedule(ctx, ns, s.Layer, s.Pipeline, s.Cron, enabled); err != nil && !isConflict(err) {
			res.Errors = append(res.Errors,
				fmt.Sprintf("schedule %s.%s (%s): %s", s.Layer, s.Pipeline, s.Cron, err))
			continue
		}
		res.Steps = append(res.Steps,
			fmt.Sprintf("schedule %s.%s every '%s'", s.Layer, s.Pipeline, s.Cron))
	}

	// 3c. Quality tests
	for _, qt := range m.Tests {
		sql, err := i.readDemoFile(m.ID, qt.File)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("read %s: %s", qt.File, err))
			continue
		}
		severity := qt.Severity
		if severity == "" {
			severity = "error"
		}
		err = i.ratd.CreateQualityTest(ctx, ns, qt.Layer, qt.Pipeline, qt.Name, sql, severity, qt.Description)
		if err != nil && !isConflict(err) {
			res.Errors = append(res.Errors, fmt.Sprintf("test %s: %s", qt.Name, err))
			continue
		}
		res.Steps = append(res.Steps, fmt.Sprintf("test %s.%s.%s", qt.Layer, qt.Pipeline, qt.Name))
	}

	// 4. Submit bronze runs, lightly staggered. The runner's DuckDB process
	// is not happy with many parallel executions (it crashed with 4 in
	// flight during testing) — a few seconds between submissions is enough
	// to keep them sequential in practice. Silver and gold runs are left to
	// the user (or the scheduler).
	first := true
	for _, p := range m.Pipelines {
		if p.Layer != "bronze" {
			continue
		}
		if !first {
			select {
			case <-ctx.Done():
				return res
			case <-time.After(3 * time.Second):
			}
		}
		first = false
		if _, err := i.ratd.SubmitRun(ctx, ns, p.Layer, p.Name, "manual"); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("run %s.%s: %s", p.Layer, p.Name, err))
			continue
		}
		res.Steps = append(res.Steps, fmt.Sprintf("run submitted: %s.%s", p.Layer, p.Name))
	}

	return res
}

func (i *Installer) readDemoFile(demoID, rel string) (string, error) {
	full := "demos/" + demoID + "/" + rel
	b, err := i.files.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("file %s: %w", full, err)
	}
	return string(b), nil
}
