package main

// detector.go walks Nessie's REST API to enumerate every Iceberg table,
// then counts data files per table via MinIO's HTTP list API. We avoid
// pulling in the MinIO Go SDK (one less dep): the Nessie + S3 list calls
// are plain HTTP/JSON and we only need the prefix + size info.
//
// The detector keeps its results in an in-memory snapshot keyed by
// (namespace, layer, name). The api.go layer reads from this snapshot;
// the compactor's auto-loop also consumes it to pick rewrite candidates.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// TableHealth is one row in the detector's in-memory snapshot — the API
// + UI flatten this into JSON without further transformation.
//
// We report counts from the CURRENT Iceberg snapshot (read out of the
// table's metadata.json summary), not from a raw S3 directory listing.
// The raw listing would include data files left behind by past snapshots
// that haven't been physically deleted yet — counting those would loop
// the auto-compactor forever (compaction succeeds → stats unchanged →
// next tick still sees a candidate).
type TableHealth struct {
	Namespace        string    `json:"namespace"`
	Layer            string    `json:"layer"`
	Name             string    `json:"name"`
	FileCount        int       `json:"file_count"`
	TotalBytes       int64     `json:"total_bytes"`
	AvgFileBytes     int64     `json:"avg_file_bytes"`
	SmallFileRatio   float64   `json:"small_file_ratio"` // 1 - (avg / target_size), clamped to [0,1]
	Status           string    `json:"status"` // ok | candidate | compacting | error
	LastCheckedAt    time.Time `json:"last_checked_at"`
	LastCompactedAt  time.Time `json:"last_compacted_at,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
	LastCompactStats string    `json:"last_compact_stats,omitempty"`
}

type detector struct {
	nessieURL string
	s3        s3Client

	targetFileBytes int64 // files this big or larger are "well-sized"
	minFileCount    int   // tables below this are never compaction candidates
	threshold       float64

	mu     sync.RWMutex
	tables map[string]*TableHealth // key = "ns/layer/name"
}

func newDetector(nessieURL string, s3 s3Client, targetFileBytes int64, minFileCount int, threshold float64) *detector {
	return &detector{
		nessieURL:       strings.TrimRight(nessieURL, "/"),
		s3:              s3,
		targetFileBytes: targetFileBytes,
		minFileCount:    minFileCount,
		threshold:       threshold,
		tables:          make(map[string]*TableHealth),
	}
}

// snapshot returns a stable copy of the current health map for the API.
func (d *detector) snapshot() []TableHealth {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]TableHealth, 0, len(d.tables))
	for _, t := range d.tables {
		out = append(out, *t)
	}
	return out
}

// candidates returns tables that exceed the small-file-ratio threshold
// and have enough files to be worth compacting.
func (d *detector) candidates() []TableHealth {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]TableHealth, 0)
	for _, t := range d.tables {
		if t.Status == "candidate" {
			out = append(out, *t)
		}
	}
	return out
}

// markStatus is used by the compactor to flip status before/after a run
// so the UI shows "compacting…" while the python helper is in flight.
func (d *detector) markStatus(ns, layer, name, status, statsLine, errMsg string) {
	key := ns + "/" + layer + "/" + name
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.tables[key]
	if !ok {
		return
	}
	t.Status = status
	t.LastError = errMsg
	if statsLine != "" {
		t.LastCompactStats = statsLine
	}
	if status == "ok" && errMsg == "" {
		t.LastCompactedAt = time.Now().UTC()
	}
}

// scan runs one full sweep: discover namespaces → tables → file counts.
// Errors per table are recorded in TableHealth.LastError; one bad table
// doesn't abort the sweep.
func (d *detector) scan(ctx context.Context) error {
	namespaces, err := d.listNessieNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("list namespaces: %w", err)
	}

	seen := make(map[string]bool)
	for _, ns := range namespaces {
		entries, err := d.listNessieEntries(ctx, ns)
		if err != nil {
			slog.Warn("compaction: list entries failed", "namespace", ns, "error", err)
			continue
		}
		for _, entry := range entries {
			// Nessie stores tables as nested namespaces — for RAT the convention
			// is "namespace.layer.table" (3 segments). We skip anything else.
			if len(entry.Name.Elements) != 3 {
				continue
			}
			n, layer, name := entry.Name.Elements[0], entry.Name.Elements[1], entry.Name.Elements[2]
			key := n + "/" + layer + "/" + name
			seen[key] = true

			// metadataLocation tells us where to read the current snapshot
			// summary from. The summary carries the canonical file counts —
			// only-current-snapshot view, not the raw S3 leftovers.
			count, bytes, err := d.s3.readSnapshotStats(ctx, entry.Metadata.MetadataLocation)
			now := time.Now().UTC()
			d.mu.Lock()
			t, ok := d.tables[key]
			if !ok {
				t = &TableHealth{Namespace: n, Layer: layer, Name: name}
				d.tables[key] = t
			}
			if err != nil {
				t.LastError = err.Error()
				t.Status = "error"
				t.LastCheckedAt = now
				d.mu.Unlock()
				continue
			}
			t.FileCount = count
			t.TotalBytes = bytes
			if count > 0 {
				t.AvgFileBytes = bytes / int64(count)
				// SmallFileRatio is a 0..1 score of how much smaller than the
				// target average the files are. 1 = files are absurdly small,
				// 0 = avg meets-or-exceeds target. Used only as a UI signal;
				// the trigger logic below compares avg directly.
				if t.AvgFileBytes >= d.targetFileBytes {
					t.SmallFileRatio = 0
				} else {
					t.SmallFileRatio = 1 - float64(t.AvgFileBytes)/float64(d.targetFileBytes)
				}
			} else {
				t.AvgFileBytes = 0
				t.SmallFileRatio = 0
			}
			// Don't clobber a "compacting" status mid-flight.
			if t.Status != "compacting" {
				if count >= d.minFileCount && t.SmallFileRatio >= d.threshold {
					t.Status = "candidate"
				} else {
					t.Status = "ok"
				}
			}
			t.LastCheckedAt = now
			t.LastError = ""
			d.mu.Unlock()
		}
	}

	// Drop entries no longer in Nessie so the UI doesn't show ghost rows
	// for tables the user deleted between sweeps.
	d.mu.Lock()
	for key := range d.tables {
		if !seen[key] {
			delete(d.tables, key)
		}
	}
	d.mu.Unlock()
	return nil
}

// dataPrefixFromMetadata strips "s3://<bucket>/" + the trailing
// "metadata/<file>.json" segment, leaving the table's "<ns>/<layer>/<name_uuid>/data/" prefix.
func dataPrefixFromMetadata(metadataLocation string) string {
	const scheme = "s3://"
	if !strings.HasPrefix(metadataLocation, scheme) {
		return ""
	}
	rest := metadataLocation[len(scheme):]
	// rest = "<bucket>/<...>/metadata/<file>.json"
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return ""
	}
	rest = rest[slash+1:]
	idx := strings.LastIndex(rest, "/metadata/")
	if idx < 0 {
		return ""
	}
	return rest[:idx] + "/data/"
}

// ── Nessie REST ────────────────────────────────────────────────────────

// Nessie's content APIs return namespace names as path-segment-encoded
// dotted strings, e.g. "shop.bronze" — we need the parts as a slice to
// list children. The wrapping types follow Nessie's v2 contract; see
// https://projectnessie.org/docs/iceberg/spec/.

type nessieEntry struct {
	Name     nessieKey      `json:"name"`
	Metadata nessieMetadata `json:"-"` // populated by a follow-up GET (see listNessieEntries)
}

type nessieMetadata struct {
	MetadataLocation string `json:"metadataLocation"`
}

type nessieKey struct {
	Elements []string `json:"elements"`
}

func (d *detector) listNessieNamespaces(ctx context.Context) ([]string, error) {
	// We list every entry on main branch with no path filter, then group by
	// the first-segment of the table name. Nessie's namespace-listing API
	// is more direct, but the entries endpoint already gives us the table
	// triples we need next so we'd be paying twice. Defer optimisation.
	entries, err := d.listAllEntries(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, e := range entries {
		if len(e.Name.Elements) >= 1 {
			seen[e.Name.Elements[0]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	return out, nil
}

func (d *detector) listNessieEntries(ctx context.Context, namespace string) ([]nessieEntry, error) {
	all, err := d.listAllEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]nessieEntry, 0)
	for _, e := range all {
		if len(e.Name.Elements) >= 1 && e.Name.Elements[0] == namespace {
			out = append(out, e)
		}
	}
	return out, nil
}

// listAllEntries hits Nessie's v2 entries API for the main branch and
// follows pagination. The shape is { entries: [...], token } where token
// is empty when the listing is complete.
func (d *detector) listAllEntries(ctx context.Context) ([]nessieEntry, error) {
	u := d.nessieURL + "/api/v2/trees/main/entries?content=true&max-records=200"
	out := make([]nessieEntry, 0)
	token := ""
	for {
		url := u
		if token != "" {
			url += "&page-token=" + queryEscape(token)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("nessie entries: HTTP %d: %s", resp.StatusCode, string(body))
		}
		var page struct {
			Entries []struct {
				Name    nessieKey `json:"name"`
				Content struct {
					MetadataLocation string `json:"metadataLocation"`
				} `json:"content"`
			} `json:"entries"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode nessie entries: %w", err)
		}
		for _, e := range page.Entries {
			out = append(out, nessieEntry{
				Name:     e.Name,
				Metadata: nessieMetadata{MetadataLocation: e.Content.MetadataLocation},
			})
		}
		if page.Token == "" {
			break
		}
		token = page.Token
	}
	return out, nil
}

func queryEscape(s string) string {
	// minimal escaper — avoids pulling in net/url just for this
	return strings.ReplaceAll(strings.ReplaceAll(s, "+", "%2B"), " ", "%20")
}

// ── S3 list (MinIO Go SDK — handles AWS V4 signing) ────────────────────

type s3Client struct {
	mc     *minio.Client
	bucket string
}

func newS3Client(endpoint, accessKey, secretKey, bucket, region string) s3Client {
	// minio.New needs a hostname[:port] — strip scheme and pick useSSL based
	// on what was passed.
	useSSL := strings.HasPrefix(endpoint, "https://")
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	host = strings.TrimRight(host, "/")
	mc, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region,
	})
	if err != nil {
		// Construction failures here are programmer errors (bad endpoint
		// shape) — surface them with a panic so they're caught in startup
		// rather than silently producing a non-functional plugin.
		panic(fmt.Sprintf("init MinIO client: %v", err))
	}
	return s3Client{mc: mc, bucket: bucket}
}

// readSnapshotStats fetches the table's current metadata.json from S3,
// finds the current snapshot, and returns (total_data_files, total_bytes).
// The metadata.json is small (1-10 KiB typically), so this is cheap to do
// per-table per-sweep — no need to cache between ticks.
//
// metadataLocation comes from Nessie's content listing as a full s3://
// URL. We strip the scheme + bucket prefix to get the object key, then
// GET it via the MinIO SDK (which V4-signs the request).
func (c s3Client) readSnapshotStats(ctx context.Context, metadataLocation string) (int, int64, error) {
	const scheme = "s3://"
	if !strings.HasPrefix(metadataLocation, scheme) {
		return 0, 0, fmt.Errorf("metadata location missing s3:// scheme: %q", metadataLocation)
	}
	rest := metadataLocation[len(scheme):]
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return 0, 0, fmt.Errorf("metadata location has no key: %q", metadataLocation)
	}
	// rest = "<bucket>/<key>". We ignore the bucket from the URL and use
	// the configured bucket — they're always the same in our setup and
	// keeping a single bucket simplifies the IAM scope.
	key := rest[slash+1:]

	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("get metadata.json: %w", err)
	}
	defer obj.Close()

	body, err := io.ReadAll(io.LimitReader(obj, 8*1024*1024))
	if err != nil {
		return 0, 0, fmt.Errorf("read metadata.json: %w", err)
	}

	var meta struct {
		CurrentSnapshotID int64 `json:"current-snapshot-id"`
		Snapshots         []struct {
			SnapshotID int64             `json:"snapshot-id"`
			Summary    map[string]string `json:"summary"`
		} `json:"snapshots"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return 0, 0, fmt.Errorf("decode metadata.json: %w", err)
	}

	for _, s := range meta.Snapshots {
		if s.SnapshotID != meta.CurrentSnapshotID {
			continue
		}
		fc, _ := strconv.Atoi(s.Summary["total-data-files"])
		fs, _ := strconv.ParseInt(s.Summary["total-files-size"], 10, 64)
		return fc, fs, nil
	}
	// Fresh table with no snapshots yet — report zeros, status will end
	// up "ok" (below min_file_count).
	return 0, 0, nil
}
