package main

// tools wires this plugin's MCP tools to ratd's catalog/metadata API. The
// tools are deliberately read-only and small: an LLM should be able to learn
// the warehouse by asking "what tables do you have?" → "describe X" without
// ever needing to write SQL.

import (
	"context"
	"fmt"
	"strings"
)

func registerTools(s *Server, r *ratdClient) {
	s.Add(Tool{
		Name:        "list_namespaces",
		Description: "List the namespaces (data domains) available in RAT. Use first if you don't know what to query.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(ctx context.Context, _ map[string]any) (string, error) {
			ns, err := r.listNamespaces(ctx)
			if err != nil {
				return "", err
			}
			return jsonString(map[string]any{"namespaces": ns}), nil
		},
	})

	s.Add(Tool{
		Name: "list_tables",
		Description: "List tables in the catalog, optionally filtered by namespace. " +
			"Each entry has namespace, layer (bronze/silver/gold) and name.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{
					"type":        "string",
					"description": "Optional namespace filter, e.g. 'shop'.",
				},
			},
		},
		Handler: func(ctx context.Context, args map[string]any) (string, error) {
			ns := argStringOpt(args, "namespace", "")
			tables, err := r.listTables(ctx, ns)
			if err != nil {
				return "", err
			}
			// Trim noisy fields the model doesn't need — keeps the response
			// small enough to be useful inside a model context.
			trimmed := make([]map[string]any, 0, len(tables))
			for _, t := range tables {
				row := map[string]any{}
				for _, k := range []string{"namespace", "layer", "name", "row_count"} {
					if v, ok := t[k]; ok {
						row[k] = v
					}
				}
				trimmed = append(trimmed, row)
			}
			return jsonString(map[string]any{"tables": trimmed}), nil
		},
	})

	s.Add(Tool{
		Name: "get_table_schema",
		Description: "Return the columns (name + type) of a single table. " +
			"Use before run_query so you cite real column names.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string"},
				"layer":     map[string]any{"type": "string", "enum": []string{"bronze", "silver", "gold"}},
				"name":      map[string]any{"type": "string"},
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
			t, err := r.getTable(ctx, ns, layer, name)
			if err != nil {
				return "", err
			}
			schema := map[string]any{
				"namespace": ns, "layer": layer, "name": name,
				"columns": t["columns"],
			}
			return jsonString(schema), nil
		},
	})

	s.Add(Tool{
		Name: "get_table_description",
		Description: "Return the human-authored description and per-column descriptions for a table. " +
			"This is the canonical place to learn what a table means.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string"},
				"layer":     map[string]any{"type": "string", "enum": []string{"bronze", "silver", "gold"}},
				"name":      map[string]any{"type": "string"},
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
			meta, err := r.getTableMetadata(ctx, ns, layer, name)
			if err != nil {
				// 404 is meaningful here — surface it as a soft message
				// instead of an error so the model can still answer.
				if strings.Contains(err.Error(), "not found") {
					return jsonString(map[string]any{
						"namespace": ns, "layer": layer, "name": name,
						"description":         "",
						"column_descriptions": map[string]string{},
						"note":                "no description authored for this table yet",
					}), nil
				}
				return "", err
			}
			out := map[string]any{"namespace": ns, "layer": layer, "name": name}
			for _, k := range []string{"description", "column_descriptions"} {
				if v, ok := meta[k]; ok {
					out[k] = v
				}
			}
			return jsonString(out), nil
		},
	})

	s.Add(Tool{
		Name:        "describe_warehouse",
		Description: "One-shot orientation: returns all namespaces + every table grouped by namespace and layer. Useful when the user asks an open-ended question.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(ctx context.Context, _ map[string]any) (string, error) {
			ns, err := r.listNamespaces(ctx)
			if err != nil {
				return "", err
			}
			tables, err := r.listTables(ctx, "")
			if err != nil {
				return "", err
			}
			// Bucket per namespace → layer → []name. Cheap to compute, very
			// LLM-friendly to read.
			byNS := map[string]map[string][]string{}
			for _, t := range tables {
				nsv, _ := t["namespace"].(string)
				lyv, _ := t["layer"].(string)
				nm, _ := t["name"].(string)
				if nsv == "" || nm == "" {
					continue
				}
				if _, ok := byNS[nsv]; !ok {
					byNS[nsv] = map[string][]string{}
				}
				byNS[nsv][lyv] = append(byNS[nsv][lyv], nm)
			}
			return jsonString(map[string]any{
				"namespaces":           ns,
				"tables_by_namespace":  byNS,
				"total_namespace_count": len(ns),
				"total_table_count":    fmt.Sprintf("%d", len(tables)),
			}), nil
		},
	})
}
