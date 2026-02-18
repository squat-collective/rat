package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rat-data/rat/platform/internal/domain"
)

// LineageNode represents a node in the lineage graph (pipeline, table, or landing zone).
type LineageNode struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"` // "pipeline" | "table" | "landing_zone"
	Namespace   string          `json:"namespace"`
	Layer       string          `json:"layer,omitempty"`
	Name        string          `json:"name"`
	LatestRun   *RunSummary     `json:"latest_run,omitempty"`
	Quality     *QualitySummary `json:"quality,omitempty"`
	TableStats  *LineageTableStats `json:"table_stats,omitempty"`
	LandingInfo *LandingInfo    `json:"landing_info,omitempty"`
}

// RunSummary is a compact representation of the latest pipeline run.
type RunSummary struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// QualitySummary is a compact representation of quality test results.
type QualitySummary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Warned int `json:"warned"`
}

// LineageTableStats holds row count and size for a table node.
type LineageTableStats struct {
	RowCount  int64 `json:"row_count"`
	SizeBytes int64 `json:"size_bytes"`
}

// LandingInfo holds file count for a landing zone node.
type LandingInfo struct {
	FileCount int `json:"file_count"`
}

// LineageEdge represents a directed edge in the lineage graph.
type LineageEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"` // "ref" | "produces" | "landing_input"
}

// LineageGraph is the complete lineage response.
type LineageGraph struct {
	Nodes []LineageNode `json:"nodes"`
	Edges []LineageEdge `json:"edges"`
}

// Regex patterns matching the runner's templating.py for dependency extraction.
var (
	refRe         = regexp.MustCompile(`ref\(\s*['"]([^'"]+)['"]\s*\)`)
	landingZoneRe = regexp.MustCompile(`landing_zone\(\s*['"]([^'"]+)['"]\s*\)`)
)

// maxConcurrentReads bounds the number of concurrent S3 reads for pipeline SQL.
const maxConcurrentReads = 20

// pipelineDeps holds parsed dependencies for a single pipeline.
type pipelineDeps struct {
	refs         []string // ref('layer.name') or ref('ns.layer.name')
	landingZones []string // landing_zone('name')
}

// MountLineageRoutes registers the lineage endpoint on the router.
func MountLineageRoutes(r chi.Router, srv *Server) {
	r.Get("/lineage", srv.HandleGetLineage)
}

// HandleGetLineage builds and returns the lineage DAG for the given namespace (or all namespaces).
//
// Goroutines use a derived context with a 60-second timeout rather than the raw
// request context. This prevents goroutines from holding a reference to the
// request context after the HTTP handler returns, which could lead to cancelled
// operations or data races on the context values.
func (s *Server) HandleGetLineage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nsFilter := r.URL.Query().Get("namespace")

	// Derive a context for concurrent goroutines — decouples from the HTTP
	// request lifecycle while still respecting upstream cancellation.
	fetchCtx, fetchCancel := context.WithTimeout(ctx, 60*time.Second)
	defer fetchCancel()

	// 1. List all pipelines
	pipelines, err := s.Pipelines.ListPipelines(ctx, PipelineFilter{Namespace: nsFilter})
	if err != nil {
		internalError(w, "failed to list pipelines", err)
		return
	}

	// 2. Read pipeline SQL files in parallel (bounded concurrency)
	results := make([]pipelineDeps, len(pipelines))
	sem := make(chan struct{}, maxConcurrentReads)
	var wg sync.WaitGroup

	for i, p := range pipelines {
		wg.Add(1)
		go func(idx int, s3Path, pType string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ext := "pipeline.sql"
			if pType == "python" {
				ext = "pipeline.py"
			}
			filePath := strings.TrimRight(s3Path, "/") + "/" + ext

			content, readErr := s.Storage.ReadFile(fetchCtx, filePath)
			if readErr != nil || content == nil {
				return
			}

			results[idx] = extractDependencies(content.Content)
		}(i, p.S3Path, p.Type)
	}
	wg.Wait()

	// 3. Batch-fetch latest runs, quality test counts, tables, and landing zones in parallel.
	// Uses batch methods to avoid N+1 queries per pipeline.
	runMap := make(map[string]*RunSummary)
	qualityMap := make(map[string]*QualitySummary)
	var tableInfos []TableInfo
	var landingZoneItems []LandingZoneListItem
	var mu sync.Mutex
	var fetchWg sync.WaitGroup

	// Batch-fetch latest run per pipeline (single query instead of N)
	fetchWg.Add(1)
	go func() {
		defer fetchWg.Done()
		pipelineIDs := make([]uuid.UUID, len(pipelines))
		pipelineKeyByID := make(map[uuid.UUID]string, len(pipelines))
		for i, p := range pipelines {
			pipelineIDs[i] = p.ID
			pipelineKeyByID[p.ID] = p.Namespace + "." + string(p.Layer) + "." + p.Name
		}
		latestRuns, batchErr := s.Runs.LatestRunPerPipeline(fetchCtx, pipelineIDs)
		if batchErr != nil {
			return
		}
		mu.Lock()
		for pid, run := range latestRuns {
			key, ok := pipelineKeyByID[pid]
			if !ok {
				continue
			}
			rs := &RunSummary{
				ID:     run.ID.String(),
				Status: string(run.Status),
			}
			if run.StartedAt != nil {
				rs.StartedAt = run.StartedAt.Format("2006-01-02T15:04:05Z")
			}
			if run.DurationMs != nil {
				rs.DurationMs = int64(*run.DurationMs)
			}
			runMap[key] = rs
		}
		mu.Unlock()
	}()

	// Batch-fetch quality test counts (single query instead of N)
	fetchWg.Add(1)
	go func() {
		defer fetchWg.Done()
		counts, batchErr := s.Quality.ListTestCounts(fetchCtx, nsFilter)
		if batchErr != nil {
			return
		}
		mu.Lock()
		for key, count := range counts {
			if count > 0 {
				qualityMap[key] = &QualitySummary{Total: count}
			}
		}
		mu.Unlock()
	}()

	// Fetch all tables
	fetchWg.Add(1)
	go func() {
		defer fetchWg.Done()
		tables, listErr := s.Query.ListTables(fetchCtx, nsFilter, "")
		if listErr != nil {
			return
		}
		mu.Lock()
		tableInfos = tables
		mu.Unlock()
	}()

	// Fetch landing zones
	fetchWg.Add(1)
	go func() {
		defer fetchWg.Done()
		zones, listErr := s.LandingZones.ListZones(fetchCtx, LandingZoneFilter{Namespace: nsFilter})
		if listErr != nil {
			return
		}
		mu.Lock()
		landingZoneItems = zones
		mu.Unlock()
	}()

	fetchWg.Wait()

	// 4. Build graph
	graph := buildLineageGraph(pipelines, results, runMap, qualityMap, tableInfos, landingZoneItems)

	writeJSON(w, http.StatusOK, graph)
}

// extractDependencies parses SQL/Python code for ref() and landing_zone() calls.
func extractDependencies(code string) pipelineDeps {
	var deps pipelineDeps

	for _, m := range refRe.FindAllStringSubmatch(code, -1) {
		if len(m) > 1 {
			deps.refs = append(deps.refs, m[1])
		}
	}

	for _, m := range landingZoneRe.FindAllStringSubmatch(code, -1) {
		if len(m) > 1 {
			deps.landingZones = append(deps.landingZones, m[1])
		}
	}

	return deps
}

// resolveRef resolves a ref string to (namespace, layer, name).
// 2-part: "layer.name" uses the pipeline's namespace.
// 3-part: "ns.layer.name" uses the explicit namespace.
func resolveRef(ref, pipelineNamespace string) (ns, layer, name string, ok bool) {
	parts := strings.Split(ref, ".")
	switch len(parts) {
	case 2:
		return pipelineNamespace, parts[0], parts[1], true
	case 3:
		return parts[0], parts[1], parts[2], true
	default:
		return "", "", "", false
	}
}

// buildLineageGraph constructs the full lineage graph from all fetched data.
func buildLineageGraph(
	pipelines []domain.Pipeline,
	deps []pipelineDeps,
	runMap map[string]*RunSummary,
	qualityMap map[string]*QualitySummary,
	tables []TableInfo,
	landingZones []LandingZoneListItem,
) LineageGraph {
	// Pre-compute edge capacity: each pipeline produces at least 1 edge,
	// plus one per ref and one per landing zone dependency.
	edgeCap := len(pipelines)
	for _, d := range deps {
		edgeCap += len(d.refs) + len(d.landingZones)
	}

	nodeMap := make(map[string]bool)
	var nodes []LineageNode
	edges := make([]LineageEdge, 0, edgeCap)

	addNode := func(n LineageNode) {
		if !nodeMap[n.ID] {
			nodeMap[n.ID] = true
			nodes = append(nodes, n)
		}
	}

	// Tables indexed by ns.layer.name for lookup
	tableMap := make(map[string]*TableInfo)
	for i := range tables {
		t := &tables[i]
		key := t.Namespace + "." + t.Layer + "." + t.Name
		tableMap[key] = t
	}

	// Landing zones indexed by ns.name
	lzMap := make(map[string]*LandingZoneListItem)
	for i := range landingZones {
		lz := &landingZones[i]
		key := lz.Namespace + "." + lz.Name
		lzMap[key] = lz
	}

	// Track which tables are produced by a pipeline
	producedTables := make(map[string]bool)

	// Process each pipeline
	for i, p := range pipelines {
		pipelineKey := p.Namespace + "." + string(p.Layer) + "." + p.Name
		pipelineID := "pipeline:" + pipelineKey

		// Pipeline node
		pNode := LineageNode{
			ID:        pipelineID,
			Type:      "pipeline",
			Namespace: p.Namespace,
			Layer:     string(p.Layer),
			Name:      p.Name,
			LatestRun: runMap[pipelineKey],
			Quality:   qualityMap[pipelineKey],
		}
		addNode(pNode)

		// Target table = pipeline name (convention: same ns.layer.name)
		targetTableKey := pipelineKey
		targetTableID := "table:" + targetTableKey
		producedTables[targetTableKey] = true

		if t, ok := tableMap[targetTableKey]; ok {
			addNode(LineageNode{
				ID:        targetTableID,
				Type:      "table",
				Namespace: t.Namespace,
				Layer:     t.Layer,
				Name:      t.Name,
				TableStats: &LineageTableStats{
					RowCount:  t.RowCount,
					SizeBytes: t.SizeBytes,
				},
			})
		} else {
			// Table may not exist yet (pipeline hasn't run)
			addNode(LineageNode{
				ID:        targetTableID,
				Type:      "table",
				Namespace: p.Namespace,
				Layer:     string(p.Layer),
				Name:      p.Name,
			})
		}

		// Edge: pipeline → target table (produces)
		edges = append(edges, LineageEdge{
			Source: pipelineID,
			Target: targetTableID,
			Type:   "produces",
		})

		// Process dependencies
		if i < len(deps) {
			d := deps[i]

			// ref() dependencies: table → pipeline
			for _, ref := range d.refs {
				refNS, refLayer, refName, ok := resolveRef(ref, p.Namespace)
				if !ok {
					continue
				}
				refKey := refNS + "." + refLayer + "." + refName
				refTableID := "table:" + refKey

				if t, exists := tableMap[refKey]; exists {
					addNode(LineageNode{
						ID:        refTableID,
						Type:      "table",
						Namespace: t.Namespace,
						Layer:     t.Layer,
						Name:      t.Name,
						TableStats: &LineageTableStats{
							RowCount:  t.RowCount,
							SizeBytes: t.SizeBytes,
						},
					})
				} else {
					addNode(LineageNode{
						ID:        refTableID,
						Type:      "table",
						Namespace: refNS,
						Layer:     refLayer,
						Name:      refName,
					})
				}

				edges = append(edges, LineageEdge{
					Source: refTableID,
					Target: pipelineID,
					Type:   "ref",
				})
			}

			// landing_zone() dependencies: landing zone → pipeline
			for _, lzName := range d.landingZones {
				lzKey := p.Namespace + "." + lzName
				lzID := "landing:" + lzKey

				if lz, exists := lzMap[lzKey]; exists {
					addNode(LineageNode{
						ID:        lzID,
						Type:      "landing_zone",
						Namespace: lz.Namespace,
						Name:      lz.Name,
						LandingInfo: &LandingInfo{
							FileCount: lz.FileCount,
						},
					})
				} else {
					addNode(LineageNode{
						ID:        lzID,
						Type:      "landing_zone",
						Namespace: p.Namespace,
						Name:      lzName,
					})
				}

				edges = append(edges, LineageEdge{
					Source: lzID,
					Target: pipelineID,
					Type:   "landing_input",
				})
			}
		}
	}

	// Add orphan tables (tables not produced by any pipeline)
	for _, t := range tables {
		key := t.Namespace + "." + t.Layer + "." + t.Name
		if !producedTables[key] {
			addNode(LineageNode{
				ID:        "table:" + key,
				Type:      "table",
				Namespace: t.Namespace,
				Layer:     t.Layer,
				Name:      t.Name,
				TableStats: &LineageTableStats{
					RowCount:  t.RowCount,
					SizeBytes: t.SizeBytes,
				},
			})
		}
	}

	// Add orphan landing zones (not referenced by any pipeline)
	for _, lz := range landingZones {
		lzKey := lz.Namespace + "." + lz.Name
		lzID := "landing:" + lzKey
		addNode(LineageNode{
			ID:        lzID,
			Type:      "landing_zone",
			Namespace: lz.Namespace,
			Name:      lz.Name,
			LandingInfo: &LandingInfo{
				FileCount: lz.FileCount,
			},
		})
	}

	if nodes == nil {
		nodes = []LineageNode{}
	}
	if edges == nil {
		edges = []LineageEdge{}
	}

	return LineageGraph{
		Nodes: nodes,
		Edges: edges,
	}
}
