package main

// lineage.go — the DAG builder. Ported from
// platform/internal/api/lineage.go, adapted to read its source data
// from ratd's HTTP API rather than in-process stores.
//
// Same shape and semantics as the original — a node per pipeline + a
// node per producible table + a node per landing zone, with edges:
//   pipeline → table          (produces)
//   table    → pipeline       (ref — read dependency)
//   landing  → pipeline       (landing_input)
//
// Dependencies are extracted from pipeline source files (.sql / .py)
// with the same regexes the runner uses for template substitution.

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// LineageNode is one node in the graph (pipeline, table, or landing zone).
type LineageNode struct {
	ID          string             `json:"id"`
	Type        string             `json:"type"`
	Namespace   string             `json:"namespace"`
	Layer       string             `json:"layer,omitempty"`
	Name        string             `json:"name"`
	LatestRun   *RunSummary        `json:"latest_run,omitempty"`
	Quality     *QualitySummary    `json:"quality,omitempty"`
	TableStats  *LineageTableStats `json:"table_stats,omitempty"`
	LandingInfo *LandingInfo       `json:"landing_info,omitempty"`
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

// LineageEdge is a directed edge in the graph.
type LineageEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// LineageGraph is the full response.
type LineageGraph struct {
	Nodes []LineageNode `json:"nodes"`
	Edges []LineageEdge `json:"edges"`
}

// Regexes match the runner's templating.py for dependency extraction.
var (
	refRe         = regexp.MustCompile(`ref\(\s*['"]([^'"]+)['"]\s*\)`)
	landingZoneRe = regexp.MustCompile(`landing_zone\(\s*['"]([^'"]+)['"]\s*\)`)
)

// Per-pipeline parsed dependencies.
type pipelineDeps struct {
	refs         []string
	landingZones []string
}

// maxConcurrentReads bounds how many pipeline files we read in
// parallel. Same value as the original.
const maxConcurrentReads = 20

// buildGraph is the orchestrator — fetches everything in parallel
// from ratd, parses pipeline files for deps, assembles the graph.
func (l *lineageService) buildGraph(ctx context.Context, nsFilter string) (LineageGraph, error) {
	pipelines, err := l.ratd.listPipelines(ctx, nsFilter)
	if err != nil {
		return LineageGraph{}, err
	}

	// Read pipeline files in parallel (bounded).
	deps := make([]pipelineDeps, len(pipelines))
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
			content, err := l.ratd.readFile(ctx, filePath)
			if err != nil || content == "" {
				return
			}
			deps[idx] = extractDeps(content)
		}(i, p.S3Path, p.Type)
	}
	wg.Wait()

	// Batch-fetch runs, quality counts, tables, landing zones in parallel.
	var (
		runMap     = map[string]*RunSummary{}
		qualityMap = map[string]*QualitySummary{}
		tables     []tableInfo
		landings   []landingZone
		mu         sync.Mutex
		bg         sync.WaitGroup
	)

	bg.Add(1)
	go func() {
		defer bg.Done()
		runs, err := l.ratd.listLatestRunPerPipeline(ctx, pipelines)
		if err != nil {
			return
		}
		mu.Lock()
		for k, r := range runs {
			runMap[k] = r
		}
		mu.Unlock()
	}()

	bg.Add(1)
	go func() {
		defer bg.Done()
		counts, err := l.ratd.qualityTestCounts(ctx, nsFilter)
		if err != nil {
			return
		}
		mu.Lock()
		for k, c := range counts {
			if c > 0 {
				qualityMap[k] = &QualitySummary{Total: c}
			}
		}
		mu.Unlock()
	}()

	bg.Add(1)
	go func() {
		defer bg.Done()
		ts, err := l.ratd.listTables(ctx, nsFilter)
		if err != nil {
			return
		}
		mu.Lock()
		tables = ts
		mu.Unlock()
	}()

	bg.Add(1)
	go func() {
		defer bg.Done()
		lz, err := l.ratd.listLandingZones(ctx, nsFilter)
		if err != nil {
			return
		}
		mu.Lock()
		landings = lz
		mu.Unlock()
	}()

	bg.Wait()

	return assembleGraph(pipelines, deps, runMap, qualityMap, tables, landings), nil
}

// extractDeps parses pipeline source for ref() and landing_zone() calls.
func extractDeps(code string) pipelineDeps {
	var d pipelineDeps
	for _, m := range refRe.FindAllStringSubmatch(code, -1) {
		if len(m) > 1 {
			d.refs = append(d.refs, m[1])
		}
	}
	for _, m := range landingZoneRe.FindAllStringSubmatch(code, -1) {
		if len(m) > 1 {
			d.landingZones = append(d.landingZones, m[1])
		}
	}
	return d
}

// resolveRef interprets a ref() argument: 2-part = same namespace,
// 3-part = explicit namespace.
func resolveRef(ref, pipelineNS string) (ns, layer, name string, ok bool) {
	parts := strings.Split(ref, ".")
	switch len(parts) {
	case 2:
		return pipelineNS, parts[0], parts[1], true
	case 3:
		return parts[0], parts[1], parts[2], true
	default:
		return "", "", "", false
	}
}

// assembleGraph builds the final nodes + edges from the parts we
// fetched. Same logic as the original buildLineageGraph.
func assembleGraph(
	pipelines []pipeline,
	deps []pipelineDeps,
	runMap map[string]*RunSummary,
	qualityMap map[string]*QualitySummary,
	tables []tableInfo,
	landings []landingZone,
) LineageGraph {
	edgeCap := len(pipelines)
	for _, d := range deps {
		edgeCap += len(d.refs) + len(d.landingZones)
	}
	nodeSeen := map[string]bool{}
	var nodes []LineageNode
	edges := make([]LineageEdge, 0, edgeCap)

	addNode := func(n LineageNode) {
		if !nodeSeen[n.ID] {
			nodeSeen[n.ID] = true
			nodes = append(nodes, n)
		}
	}

	tableMap := map[string]*tableInfo{}
	for i := range tables {
		t := &tables[i]
		tableMap[t.Namespace+"."+t.Layer+"."+t.Name] = t
	}
	lzMap := map[string]*landingZone{}
	for i := range landings {
		lz := &landings[i]
		lzMap[lz.Namespace+"."+lz.Name] = lz
	}

	produced := map[string]bool{}

	for i, p := range pipelines {
		key := p.Namespace + "." + p.Layer + "." + p.Name
		pID := "pipeline:" + key

		addNode(LineageNode{
			ID: pID, Type: "pipeline",
			Namespace: p.Namespace, Layer: p.Layer, Name: p.Name,
			LatestRun: runMap[key],
			Quality:   qualityMap[key],
		})

		// Target table node — produced by this pipeline.
		tID := "table:" + key
		produced[key] = true
		if t, ok := tableMap[key]; ok {
			addNode(LineageNode{
				ID: tID, Type: "table",
				Namespace: t.Namespace, Layer: t.Layer, Name: t.Name,
				TableStats: &LineageTableStats{RowCount: t.RowCount, SizeBytes: t.SizeBytes},
			})
		} else {
			// Table may not exist yet (pipeline hasn't run).
			addNode(LineageNode{
				ID: tID, Type: "table",
				Namespace: p.Namespace, Layer: p.Layer, Name: p.Name,
			})
		}

		edges = append(edges, LineageEdge{Source: pID, Target: tID, Type: "produces"})

		if i >= len(deps) {
			continue
		}
		d := deps[i]
		for _, r := range d.refs {
			ns, layer, name, ok := resolveRef(r, p.Namespace)
			if !ok {
				continue
			}
			rk := ns + "." + layer + "." + name
			rID := "table:" + rk
			if t, ok := tableMap[rk]; ok {
				addNode(LineageNode{
					ID: rID, Type: "table",
					Namespace: t.Namespace, Layer: t.Layer, Name: t.Name,
					TableStats: &LineageTableStats{RowCount: t.RowCount, SizeBytes: t.SizeBytes},
				})
			} else {
				addNode(LineageNode{
					ID: rID, Type: "table",
					Namespace: ns, Layer: layer, Name: name,
				})
			}
			edges = append(edges, LineageEdge{Source: rID, Target: pID, Type: "ref"})
		}

		for _, lzName := range d.landingZones {
			lzk := p.Namespace + "." + lzName
			lzID := "landing:" + lzk
			if lz, ok := lzMap[lzk]; ok {
				addNode(LineageNode{
					ID: lzID, Type: "landing_zone",
					Namespace: lz.Namespace, Name: lz.Name,
					LandingInfo: &LandingInfo{FileCount: lz.FileCount},
				})
			} else {
				addNode(LineageNode{
					ID: lzID, Type: "landing_zone",
					Namespace: p.Namespace, Name: lzName,
				})
			}
			edges = append(edges, LineageEdge{Source: lzID, Target: pID, Type: "landing_input"})
		}
	}

	// Orphan tables (not produced by any pipeline).
	for _, t := range tables {
		key := t.Namespace + "." + t.Layer + "." + t.Name
		if !produced[key] {
			addNode(LineageNode{
				ID: "table:" + key, Type: "table",
				Namespace: t.Namespace, Layer: t.Layer, Name: t.Name,
				TableStats: &LineageTableStats{RowCount: t.RowCount, SizeBytes: t.SizeBytes},
			})
		}
	}
	for _, lz := range landings {
		lzk := lz.Namespace + "." + lz.Name
		addNode(LineageNode{
			ID: "landing:" + lzk, Type: "landing_zone",
			Namespace: lz.Namespace, Name: lz.Name,
			LandingInfo: &LandingInfo{FileCount: lz.FileCount},
		})
	}

	if nodes == nil {
		nodes = []LineageNode{}
	}
	if edges == nil {
		edges = []LineageEdge{}
	}
	return LineageGraph{Nodes: nodes, Edges: edges}
}
