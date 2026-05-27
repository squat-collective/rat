package main

// nessie.go is a tiny Nessie v2 REST client — just enough to walk the
// commit history for a single Iceberg table. Each commit gives us the
// metadata.json URL + snapshot id that the table had AT that commit;
// that's the per-version state we feed iceberg_scan() with for the
// row-diff drill-in.
//
// We need this because PyIceberg (as RAT uses it) writes a fresh
// metadata.json per commit instead of chaining snapshots inside a
// single metadata file, so DuckDB's iceberg_snapshots() against the
// current metadata file returns only ONE snapshot. Walking Nessie's
// commit log is the canonical way to see the full history.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type nessieClient struct {
	baseURL string
	http    *http.Client
}

func newNessieClient(baseURL string) *nessieClient {
	return &nessieClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// TableVersion captures one historical state of an Iceberg table —
// derived from one Nessie commit that PUT a new content for the table.
//
// SnapshotID is string-encoded in JSON because Iceberg snapshot ids are
// int64 values that routinely exceed JavaScript's Number.MAX_SAFE_INTEGER
// (2^53 - 1). A bare JSON number is silently rounded by the browser's
// JSON.parse — past that boundary, the value the UI echoes back doesn't
// match any real snapshot and iceberg_scan() errors with
// "Could not find snapshot with id …". Encoding as a string preserves
// every bit losslessly. Go's `,string` tag handles both directions.
type TableVersion struct {
	Hash             string    `json:"hash"`
	Message          string    `json:"message"`
	CommittedAt      time.Time `json:"committed_at"`
	MetadataLocation string    `json:"metadata_location"`
	SnapshotID       int64     `json:"snapshot_id,string"`
}

// WalkTableHistory walks the commit log of the given branch (newest
// first) and returns the entries that PUT an ICEBERG_TABLE matching
// the given key. Capped at maxCommits scanned + maxResults returned to
// avoid runaway loops on a noisy branch.
func (n *nessieClient) WalkTableHistory(
	ctx context.Context, branch string, tableKey []string,
	maxCommits, maxResults int,
) ([]TableVersion, error) {
	if maxCommits <= 0 {
		maxCommits = 500
	}
	if maxResults <= 0 {
		maxResults = 50
	}
	wantKey := strings.Join(tableKey, ".")

	versions := make([]TableVersion, 0, maxResults)
	scanned := 0
	pageToken := ""

	for scanned < maxCommits && len(versions) < maxResults {
		u := fmt.Sprintf("%s/api/v2/trees/%s/history?max-records=50&fetch=ALL",
			n.baseURL, branch)
		if pageToken != "" {
			u += "&page-token=" + pageToken
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return versions, err
		}
		resp, err := n.http.Do(req)
		if err != nil {
			return versions, fmt.Errorf("nessie unreachable: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return versions, fmt.Errorf("nessie history: HTTP %d", resp.StatusCode)
		}

		var page struct {
			Token      string `json:"token"`
			LogEntries []struct {
				CommitMeta struct {
					Hash       string    `json:"hash"`
					Message    string    `json:"message"`
					CommitTime time.Time `json:"commitTime"`
				} `json:"commitMeta"`
				Operations []struct {
					Type string `json:"type"`
					Key  struct {
						Elements []string `json:"elements"`
					} `json:"key"`
					Content struct {
						Type             string `json:"type"`
						MetadataLocation string `json:"metadataLocation"`
						SnapshotID       int64  `json:"snapshotId"`
					} `json:"content"`
				} `json:"operations"`
			} `json:"logEntries"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return versions, fmt.Errorf("decode nessie page: %w", err)
		}
		if len(page.LogEntries) == 0 {
			break
		}

		for _, le := range page.LogEntries {
			scanned++
			for _, op := range le.Operations {
				if op.Type != "PUT" || op.Content.Type != "ICEBERG_TABLE" {
					continue
				}
				if strings.Join(op.Key.Elements, ".") != wantKey {
					continue
				}
				// Skip commits that registered the table without writing
				// data — Nessie records snapshotId = -1 for those. They
				// have no rows to scan, so iceberg_scan() would fail.
				if op.Content.SnapshotID <= 0 {
					continue
				}
				versions = append(versions, TableVersion{
					Hash:             le.CommitMeta.Hash,
					Message:          le.CommitMeta.Message,
					CommittedAt:      le.CommitMeta.CommitTime,
					MetadataLocation: op.Content.MetadataLocation,
					SnapshotID:       op.Content.SnapshotID,
				})
				if len(versions) >= maxResults {
					return versions, nil
				}
			}
			if scanned >= maxCommits {
				break
			}
		}
		if page.Token == "" {
			break
		}
		pageToken = page.Token
	}
	return versions, nil
}
