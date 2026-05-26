package main

// diff_iceberg.go implements the drill-in: for a given table, list its
// versioned metadata files (via Nessie's commit log) and compute a
// row-level diff between any two by running iceberg_scan against each.
//
// Why Nessie instead of iceberg_snapshots(): PyIceberg as configured in
// RAT writes a FRESH metadata.json per commit rather than chaining
// snapshots inside one file, so iceberg_snapshots() against the latest
// metadata returns a single entry. The full history lives in Nessie's
// commit log, where each commit that touched the table carries the
// metadataLocation + snapshotId it produced.

import (
	"context"
	"fmt"
	"strings"
)

type icebergDiffer struct {
	c      *ratdClient
	nessie *nessieClient
}

func newIcebergDiffer(c *ratdClient, n *nessieClient) *icebergDiffer {
	return &icebergDiffer{c: c, nessie: n}
}

// ListSnapshots returns the per-commit history of the table, newest
// first. Each entry is one Nessie commit that wrote a new state for
// the table and carries the metadata URL + snapshot id we need to
// time-travel into it.
func (d *icebergDiffer) ListSnapshots(ctx context.Context, ns, layer, name string) ([]TableVersion, error) {
	return d.nessie.WalkTableHistory(ctx, "main", []string{ns, layer, name}, 500, 100)
}

type rowDiff struct {
	Added   []map[string]any `json:"added"`
	Removed []map[string]any `json:"removed"`
	Columns []string         `json:"columns"`
	Stats   struct {
		AddedCount   int  `json:"added_count"`
		RemovedCount int  `json:"removed_count"`
		Truncated    bool `json:"truncated"`
	} `json:"stats"`
}

// Diff returns the rows added (in B but not A) and removed (in A but
// not B) between two versions. Each version is a (metadataURL,
// snapshotID) pair as returned by ListSnapshots.
func (d *icebergDiffer) Diff(
	ctx context.Context,
	metadataA string, snapshotA int64,
	metadataB string, snapshotB int64,
	limit int,
) (*rowDiff, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	// Discover the schema at snapshot B (the "new" side). If the two
	// snapshots have different schemas, EXCEPT will fail anyway and the
	// error message is informative enough.
	colsRes, err := d.c.Query(ctx,
		fmt.Sprintf(`SELECT * FROM iceberg_scan(%s, snapshot_from_id => %d) LIMIT 0`,
			sqlString(metadataB), snapshotB))
	if err != nil {
		return nil, fmt.Errorf("probe schema at B: %w", err)
	}
	cols := make([]string, 0, len(colsRes.Columns))
	for _, c := range colsRes.Columns {
		cols = append(cols, c.Name)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("no columns at B")
	}
	colList := joinIdents(cols)

	added, err := d.runDiffSide(ctx, "B EXCEPT A", colList, metadataA, snapshotA, metadataB, snapshotB, true, limit)
	if err != nil {
		return nil, fmt.Errorf("diff added: %w", err)
	}
	removed, err := d.runDiffSide(ctx, "A EXCEPT B", colList, metadataA, snapshotA, metadataB, snapshotB, false, limit)
	if err != nil {
		return nil, fmt.Errorf("diff removed: %w", err)
	}

	rd := &rowDiff{Columns: cols, Added: added, Removed: removed}
	if len(rd.Added) > limit {
		rd.Added = rd.Added[:limit]
		rd.Stats.Truncated = true
	}
	if len(rd.Removed) > limit {
		rd.Removed = rd.Removed[:limit]
		rd.Stats.Truncated = true
	}
	rd.Stats.AddedCount = len(rd.Added)
	rd.Stats.RemovedCount = len(rd.Removed)
	return rd, nil
}

// runDiffSide executes one side of the symmetric diff. The asAdded flag
// just picks the direction (B-A for added, A-B for removed) — the rest
// of the query is identical.
func (d *icebergDiffer) runDiffSide(
	ctx context.Context, _ string, colList string,
	metaA string, snapA int64, metaB string, snapB int64,
	asAdded bool, limit int,
) ([]map[string]any, error) {
	var first, second struct {
		meta string
		snap int64
	}
	if asAdded {
		first = struct{ meta string; snap int64 }{metaB, snapB}
		second = struct{ meta string; snap int64 }{metaA, snapA}
	} else {
		first = struct{ meta string; snap int64 }{metaA, snapA}
		second = struct{ meta string; snap int64 }{metaB, snapB}
	}
	sql := fmt.Sprintf(
		`SELECT * FROM (
		   SELECT %s FROM iceberg_scan(%s, snapshot_from_id => %d)
		   EXCEPT
		   SELECT %s FROM iceberg_scan(%s, snapshot_from_id => %d)
		 ) LIMIT %d`,
		colList, sqlString(first.meta), first.snap,
		colList, sqlString(second.meta), second.snap,
		limit+1,
	)
	res, err := d.c.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	return res.Rows, nil
}

// sqlString quotes a string for inclusion in a SQL literal — basic but
// safe for our small set of caller-controlled values (metadata URLs
// come from Nessie, not user input).
func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func joinIdents(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
	}
	return strings.Join(out, ", ")
}
