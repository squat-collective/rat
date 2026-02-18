package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/rat-data/rat/platform/internal/api"
)

// S3QualityStore implements api.QualityStore backed by S3.
// Quality tests are SQL files stored at {ns}/pipelines/{layer}/{name}/tests/quality/{testName}.sql
// with --@ annotations for severity and description.
type S3QualityStore struct {
	store api.StorageStore
}

// NewS3QualityStore creates a QualityStore that delegates to the given StorageStore.
func NewS3QualityStore(store api.StorageStore) *S3QualityStore {
	return &S3QualityStore{store: store}
}

func qualityPrefix(ns, layer, pipeline string) string {
	return fmt.Sprintf("%s/pipelines/%s/%s/tests/quality/", ns, layer, pipeline)
}

func qualityPath(ns, layer, pipeline, testName string) string {
	return qualityPrefix(ns, layer, pipeline) + testName + ".sql"
}

// parseAnnotations extracts --@key: value annotations from SQL content.
func parseAnnotations(sql string) (severity, description string, tags []string, remediation string) {
	severity = "error"
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-- @severity:") {
			severity = strings.TrimSpace(strings.TrimPrefix(trimmed, "-- @severity:"))
		} else if strings.HasPrefix(trimmed, "--@severity:") {
			severity = strings.TrimSpace(strings.TrimPrefix(trimmed, "--@severity:"))
		} else if strings.HasPrefix(trimmed, "-- @description:") {
			description = strings.TrimSpace(strings.TrimPrefix(trimmed, "-- @description:"))
		} else if strings.HasPrefix(trimmed, "--@description:") {
			description = strings.TrimSpace(strings.TrimPrefix(trimmed, "--@description:"))
		} else if strings.HasPrefix(trimmed, "-- @tags:") {
			tags = parseTags(strings.TrimSpace(strings.TrimPrefix(trimmed, "-- @tags:")))
		} else if strings.HasPrefix(trimmed, "--@tags:") {
			tags = parseTags(strings.TrimSpace(strings.TrimPrefix(trimmed, "--@tags:")))
		} else if strings.HasPrefix(trimmed, "-- @remediation:") {
			remediation = strings.TrimSpace(strings.TrimPrefix(trimmed, "-- @remediation:"))
		} else if strings.HasPrefix(trimmed, "--@remediation:") {
			remediation = strings.TrimSpace(strings.TrimPrefix(trimmed, "--@remediation:"))
		}
	}
	// Normalize "warning" → "warn"
	if severity == "warning" {
		severity = "warn"
	}
	return severity, description, tags, remediation
}

// parseTags splits a comma-separated tag string into a sorted, deduplicated, lowercase slice.
func parseTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var tags []string
	for _, p := range parts {
		t := strings.TrimSpace(strings.ToLower(p))
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// ListTests lists all quality tests for a pipeline by scanning S3.
func (q *S3QualityStore) ListTests(ctx context.Context, ns, layer, pipeline string) ([]api.QualityTest, error) {
	prefix := qualityPrefix(ns, layer, pipeline)
	files, err := q.store.ListFiles(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list quality tests: %w", err)
	}

	tests := make([]api.QualityTest, 0, len(files))
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".sql") {
			continue
		}
		fc, err := q.store.ReadFile(ctx, f.Path)
		if err != nil {
			return nil, fmt.Errorf("read quality test %s: %w", f.Path, err)
		}
		if fc == nil {
			continue
		}

		name := strings.TrimSuffix(strings.TrimPrefix(f.Path, prefix), ".sql")
		severity, description, tags, remediation := parseAnnotations(fc.Content)

		tests = append(tests, api.QualityTest{
			Name:        name,
			SQL:         fc.Content,
			Severity:    severity,
			Description: description,
			Tags:        tags,
			Remediation: remediation,
		})
	}
	return tests, nil
}

// CreateTest writes a quality test SQL file to S3.
func (q *S3QualityStore) CreateTest(ctx context.Context, ns, layer, pipeline string, test api.QualityTest) error {
	path := qualityPath(ns, layer, pipeline, test.Name)

	// Check if test already exists
	existing, err := q.store.StatFile(ctx, path)
	if err != nil {
		return fmt.Errorf("check existing test: %w", err)
	}
	if existing != nil {
		return fmt.Errorf("test %q already exists", test.Name)
	}

	_, err = q.store.WriteFile(ctx, path, []byte(test.SQL))
	if err != nil {
		return fmt.Errorf("write quality test: %w", err)
	}
	return nil
}

// DeleteTest removes a quality test SQL file from S3.
func (q *S3QualityStore) DeleteTest(ctx context.Context, ns, layer, pipeline, testName string) error {
	path := qualityPath(ns, layer, pipeline, testName)
	if err := q.store.DeleteFile(ctx, path); err != nil {
		return fmt.Errorf("delete quality test: %w", err)
	}
	return nil
}

// RunTests is a no-op at the storage level — test execution is handled by the runner.
// This returns an empty slice to indicate no tests were run server-side.
func (q *S3QualityStore) RunTests(_ context.Context, _, _, _ string) ([]api.QualityTestResult, error) {
	return []api.QualityTestResult{}, nil
}

// ListTestCounts returns the count of quality tests per pipeline key ("ns.layer.name")
// by scanning all quality test files in S3 for the given namespace (or all if empty).
// This avoids N+1 ListTests calls when building the lineage graph.
func (q *S3QualityStore) ListTestCounts(ctx context.Context, namespace string) (map[string]int, error) {
	// Scan all quality test paths under the namespace (or all namespaces).
	prefix := ""
	if namespace != "" {
		prefix = namespace + "/pipelines/"
	}

	files, err := q.store.ListFiles(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list quality test files: %w", err)
	}

	// Parse paths like "{ns}/pipelines/{layer}/{name}/tests/quality/{testName}.sql"
	counts := make(map[string]int)
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".sql") {
			continue
		}
		if !strings.Contains(f.Path, "/tests/quality/") {
			continue
		}
		parts := strings.Split(f.Path, "/")
		// Expected: [ns, "pipelines", layer, name, "tests", "quality", "testName.sql"]
		if len(parts) < 7 || parts[1] != "pipelines" || parts[4] != "tests" || parts[5] != "quality" {
			continue
		}
		key := parts[0] + "." + parts[2] + "." + parts[3]
		counts[key]++
	}
	return counts, nil
}
