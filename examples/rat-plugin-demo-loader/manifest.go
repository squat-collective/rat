package main

// Manifest is a demo's machine-readable definition. Each demo lives in
// `demos/{id}/manifest.json` alongside its SQL files; the install handler
// walks the manifest to create the namespace, pipelines and quality tests
// and trigger the bronze runs.
type Manifest struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Namespace   string         `json:"namespace"`
	Pipelines   []ManifestPipe `json:"pipelines"`
	Tests       []ManifestTest `json:"quality_tests,omitempty"`
}

// ManifestPipe is one pipeline to create. File is the path within the demo's
// own folder (e.g. "bronze/missions.sql").
type ManifestPipe struct {
	Layer       string `json:"layer"`
	Name        string `json:"name"`
	Type        string `json:"type"` // "sql" or "python"
	Description string `json:"description"`
	File        string `json:"file"`
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
