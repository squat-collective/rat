package storage

import (
	"path/filepath"
	"strings"
)

// detectFileType classifies a file based on its path and filename.
// Returns one of: pipeline-sql, pipeline-py, config, meta, doc, test, hook, or empty string.
func detectFileType(path string) string {
	base := filepath.Base(path)
	dir := filepath.Dir(path)

	// Test files
	if strings.Contains(dir, "/tests/") || strings.Contains(dir, "/test/") ||
		strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, "_test.py") {
		return "test"
	}

	// Hook files
	if strings.Contains(dir, "/hooks/") || strings.HasPrefix(base, "hook_") ||
		strings.HasPrefix(base, "pre_") || strings.HasPrefix(base, "post_") {
		return "hook"
	}

	// Pipeline files
	if base == "pipeline.sql" || strings.HasSuffix(base, ".sql") {
		return "pipeline-sql"
	}
	if base == "pipeline.py" {
		return "pipeline-py"
	}

	// Config files
	if base == "config.yaml" || base == "config.yml" || base == "schema.yaml" || base == "schema.yml" {
		return "config"
	}

	// Meta files
	if strings.HasSuffix(base, ".meta.yaml") || strings.HasSuffix(base, ".meta.yml") ||
		base == "meta.yaml" || base == "meta.yml" {
		return "meta"
	}

	// Doc files
	if strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".txt") ||
		strings.HasSuffix(base, ".rst") || base == "README" {
		return "doc"
	}

	return ""
}

// detectContentType returns the MIME type for a file based on its extension.
func detectContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".sql":
		return "application/sql"
	case ".py":
		return "text/x-python"
	case ".yaml", ".yml":
		return "application/x-yaml"
	case ".json":
		return "application/json"
	case ".md":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".toml":
		return "application/toml"
	case ".csv":
		return "text/csv"
	case ".sh":
		return "application/x-sh"
	default:
		return "application/octet-stream"
	}
}
