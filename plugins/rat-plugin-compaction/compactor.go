package main

// compactor.go is the bridge from Go to compact.py. We run the helper
// per-table as a one-shot subprocess so a single-table OOM can't take
// the plugin down — and so we never hold pyiceberg state in-process.
//
// The script's contract: a single JSON line on stdout (the result), and
// a non-zero exit code on failure. We forward the JSON straight into the
// detector's status field for the UI to render.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const compactScript = "/compact.py"

type compactor struct {
	det      *detector
	pyEnv    []string
	timeout  time.Duration
	autoRun  bool
	interval time.Duration

	// table-level lock — two concurrent compactions of the same table
	// would race on Iceberg snapshot commits. Different tables can run
	// in parallel, so this is per-key not a single mutex.
	locks sync.Map // map[string]*sync.Mutex
}

func newCompactor(det *detector, pyEnv []string, timeout time.Duration, autoRun bool, interval time.Duration) *compactor {
	return &compactor{det: det, pyEnv: pyEnv, timeout: timeout, autoRun: autoRun, interval: interval}
}

// CompactResult mirrors the JSON shape emitted by compact.py.
type CompactResult struct {
	OK             bool   `json:"ok"`
	Skipped        bool   `json:"skipped,omitempty"`
	Reason         string `json:"reason,omitempty"`
	FilesBefore    int    `json:"files_before"`
	FilesAfter     int    `json:"files_after"`
	SizeBefore     int64  `json:"size_before"`
	SizeAfter      int64  `json:"size_after"`
	Rows           int64  `json:"rows,omitempty"`
	DurationMS     int    `json:"duration_ms"`
	Error          string `json:"error,omitempty"`
	Trace          string `json:"trace,omitempty"`
}

// Compact runs compact.py once for the table identified by ns/layer/name.
// The detector's status is flipped to "compacting" for the duration so
// the UI shows progress; on completion it returns to "ok" or "error".
func (c *compactor) Compact(ctx context.Context, ns, layer, name string) (*CompactResult, error) {
	key := ns + "/" + layer + "/" + name
	lk, _ := c.locks.LoadOrStore(key, &sync.Mutex{})
	mu := lk.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	c.det.markStatus(ns, layer, name, "compacting", "", "")

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", compactScript, ns, layer, name)
	cmd.Env = c.pyEnv

	out, err := cmd.Output()
	if err != nil {
		errMsg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, string(ee.Stderr))
		}
		c.det.markStatus(ns, layer, name, "error", "", errMsg)
		return nil, fmt.Errorf("compact.py: %s", errMsg)
	}

	// compact.py emits exactly one JSON line. Strip trailing newline first
	// so json.Unmarshal doesn't fail on the otherwise-unrelated trailing
	// whitespace.
	line := strings.TrimSpace(string(out))
	var result CompactResult
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		c.det.markStatus(ns, layer, name, "error", "", fmt.Sprintf("decode compact output: %v: %q", err, line))
		return nil, fmt.Errorf("decode compact output: %w", err)
	}

	if !result.OK {
		c.det.markStatus(ns, layer, name, "error", "", result.Error)
		return &result, fmt.Errorf("compact failed: %s", result.Error)
	}

	stats := fmt.Sprintf("%d files (%.1f KiB) → %d files (%.1f KiB) in %dms",
		result.FilesBefore, float64(result.SizeBefore)/1024,
		result.FilesAfter, float64(result.SizeAfter)/1024,
		result.DurationMS)
	c.det.markStatus(ns, layer, name, "ok", stats, "")
	return &result, nil
}

// Loop runs the detection sweep, then if autoRun is on compacts every
// table the detector flagged as a candidate. One sweep per `interval`.
// Errors are logged but never abort the loop.
func (c *compactor) Loop(ctx context.Context) {
	// First sweep happens immediately so the UI has data on first load.
	c.tick(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *compactor) tick(ctx context.Context) {
	if err := c.det.scan(ctx); err != nil {
		slog.Warn("compaction: detection sweep failed", "error", err)
		return
	}
	if !c.autoRun {
		return
	}
	for _, t := range c.det.candidates() {
		slog.Info("compaction: auto-compacting candidate",
			"table", t.Namespace+"."+t.Layer+"."+t.Name,
			"files", t.FileCount, "ratio", t.SmallFileRatio)
		if _, err := c.Compact(ctx, t.Namespace, t.Layer, t.Name); err != nil {
			slog.Warn("compaction: auto-compact failed",
				"table", t.Namespace+"."+t.Layer+"."+t.Name, "error", err)
		}
	}
}
