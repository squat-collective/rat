package main

// Manifest is a demo's machine-readable definition. Each demo lives in
// `demos/{id}/manifest.json` alongside its SQL files; the install handler
// walks the manifest to create the namespace, pipelines, quality tests,
// table metadata and schedules, and trigger the bronze runs.
type Manifest struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Namespace   string             `json:"namespace"`
	Pipelines   []ManifestPipe     `json:"pipelines"`
	Tests       []ManifestTest     `json:"quality_tests,omitempty"`
	Schedules   []ManifestSchedule `json:"schedules,omitempty"`
}

// ManifestPipe is one pipeline to create. File is the path within the demo's
// own folder (e.g. "bronze/missions.sql"). Optional table_metadata is applied
// once the pipeline has been created so the Explorer shows the descriptions.
type ManifestPipe struct {
	Layer         string         `json:"layer"`
	Name          string         `json:"name"`
	Type          string         `json:"type"` // "sql" or "python"
	Description   string         `json:"description"`
	File          string         `json:"file"`
	TableMetadata *TableMetadata `json:"table_metadata,omitempty"`
}

// TableMetadata is what the docs-assistant plugin or the metadata UI writes —
// a table-level description and per-column descriptions. The demo-loader
// pre-populates this so users see fully documented tables without having to
// generate docs themselves.
type TableMetadata struct {
	Description        string            `json:"description"`
	ColumnDescriptions map[string]string `json:"column_descriptions"`
}

// ManifestTest is one quality test to create.
type ManifestTest struct {
	Layer       string `json:"layer"`    // the layer of the pipeline the test attaches to
	Pipeline    string `json:"pipeline"` // the pipeline name
	Name        string `json:"name"`     // the test's own name
	Severity    string `json:"severity"` // "error" or "warn"
	Description string `json:"description,omitempty"`
	File        string `json:"file"`
}

// ManifestSchedule is one cron schedule for a pipeline. The install creates it
// via POST /api/v1/schedules so the demo runs unattended after the bronze seed.
type ManifestSchedule struct {
	Layer    string `json:"layer"`
	Pipeline string `json:"pipeline"`
	Cron     string `json:"cron"`              // 5-field cron expression (e.g. "0 * * * *")
	Enabled  bool   `json:"enabled,omitempty"` // defaults to true if omitted
}
