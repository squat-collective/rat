package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// QualityTest represents a data quality test definition.
type QualityTest struct {
	Name        string   `json:"name"`
	SQL         string   `json:"sql"`
	Severity    string   `json:"severity"`     // error, warn
	Description string   `json:"description"`
	Published   bool     `json:"published"`
	Tags        []string `json:"tags"`
	Remediation string   `json:"remediation"`
}

// QualityTestResult represents a single test execution result.
type QualityTestResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // passed, failed, warned, error
	Severity   string `json:"severity"`
	Value      int    `json:"value"`
	Expected   int    `json:"expected"`
	DurationMs int64  `json:"duration_ms"`
}

// QualityStore defines the persistence interface for quality tests.
// Tests are SQL files stored in S3 under pipelines/{layer}/{name}/tests/quality/.
type QualityStore interface {
	ListTests(ctx context.Context, namespace, layer, pipeline string) ([]QualityTest, error)
	CreateTest(ctx context.Context, namespace, layer, pipeline string, test QualityTest) error
	DeleteTest(ctx context.Context, namespace, layer, pipeline, testName string) error
	RunTests(ctx context.Context, namespace, layer, pipeline string) ([]QualityTestResult, error)

	// ListTestCounts returns the count of quality tests per pipeline key ("ns.layer.name")
	// in a single batch operation, avoiding N+1 queries when building the lineage graph.
	ListTestCounts(ctx context.Context, namespace string) (map[string]int, error)
}

// CreateQualityTestRequest is the JSON body for POST /api/v1/pipelines/{namespace}/{layer}/{name}/tests.
type CreateQualityTestRequest struct {
	Name        string `json:"name"`
	SQL         string `json:"sql"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
}

// MountQualityRoutes registers quality test endpoints nested under pipelines.
// Quality tests are a sub-resource of pipelines, so they live under
// /pipelines/{namespace}/{layer}/{name}/tests rather than a top-level /tests path.
func MountQualityRoutes(r chi.Router, srv *Server) {
	r.Get("/pipelines/{namespace}/{layer}/{name}/tests", srv.HandleListQualityTests)
	r.Post("/pipelines/{namespace}/{layer}/{name}/tests", srv.HandleCreateQualityTest)
	r.Delete("/pipelines/{namespace}/{layer}/{name}/tests/{testName}", srv.HandleDeleteQualityTest)
	r.Post("/pipelines/{namespace}/{layer}/{name}/tests/run", srv.HandleRunQualityTests)
}

// HandleListQualityTests lists quality tests for a pipeline.
func (s *Server) HandleListQualityTests(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	tests, err := s.Quality.ListTests(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if tests == nil {
		tests = []QualityTest{}
	}

	// Annotate each test with published status based on pipeline's PublishedVersions.
	p, pErr := s.Pipelines.GetPipeline(r.Context(), namespace, layer, name)
	if pErr == nil && p != nil && p.PublishedVersions != nil {
		qualityPrefix := namespace + "/pipelines/" + layer + "/" + name + "/tests/quality/"
		for i := range tests {
			key := qualityPrefix + tests[i].Name + ".sql"
			_, tests[i].Published = p.PublishedVersions[key]
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tests": tests,
		"total": len(tests),
	})
}

// HandleCreateQualityTest creates a quality test SQL file.
func (s *Server) HandleCreateQualityTest(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	var req CreateQualityTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorJSON(w, "invalid request body", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.SQL == "" {
		errorJSON(w, "name and sql are required", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if !validName(req.Name) {
		errorJSON(w, "test name must be a lowercase slug (a-z, 0-9, hyphens, underscores; must start with a letter)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if len(req.SQL) > maxSQLLength {
		errorJSON(w, "sql too long (max 500KB)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if len(req.Description) > maxDescriptionLength {
		errorJSON(w, "description too long (max 5000 chars)", "INVALID_ARGUMENT", http.StatusBadRequest)
		return
	}
	if req.Severity == "" {
		req.Severity = "error"
	}

	test := QualityTest{
		Name:        req.Name,
		SQL:         req.SQL,
		Severity:    req.Severity,
		Description: req.Description,
	}

	if err := s.Quality.CreateTest(r.Context(), namespace, layer, name, test); err != nil {
		errorJSON(w, err.Error(), "ALREADY_EXISTS", http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"name":     test.Name,
		"severity": test.Severity,
		"path":     namespace + "/pipelines/" + layer + "/" + name + "/tests/quality/" + test.Name + ".sql",
	})
}

// HandleDeleteQualityTest deletes a quality test.
func (s *Server) HandleDeleteQualityTest(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")
	testName := chi.URLParam(r, "testName")

	if err := s.Quality.DeleteTest(r.Context(), namespace, layer, name, testName); err != nil {
		internalError(w, "internal error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleRunQualityTests executes all quality tests for a pipeline.
func (s *Server) HandleRunQualityTests(w http.ResponseWriter, r *http.Request) {
	namespace := chi.URLParam(r, "namespace")
	layer := chi.URLParam(r, "layer")
	name := chi.URLParam(r, "name")

	results, err := s.Quality.RunTests(r.Context(), namespace, layer, name)
	if err != nil {
		internalError(w, "internal error", err)
		return
	}
	if results == nil {
		results = []QualityTestResult{}
	}

	passed := 0
	failed := 0
	for _, r := range results {
		if r.Status == "passed" {
			passed++
		} else {
			failed++
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"passed":  passed,
		"failed":  failed,
		"total":   len(results),
	})
}
