package main

// subagentRunStore captures the FULL event stream of every subagent
// invocation — tool_calls + tool_results + assistant deltas + final
// content — and persists them to /data/subagent_runs/. The parent
// conversation only sees the subagent's final text, so without this
// store there is no way to debug "what did the subagent actually do?"
//
// Naming: {parent_conv_id}__{parent_tool_call_id}.json — one file per
// subagent invocation, keyed by the tool_call that triggered it. That
// scheme is also recursive: when a subagent calls a sub-subagent, the
// nested run is stored under {parent_tool_call_id}__{nested_tool_call_id}
// so the chain is reconstructable from filenames alone.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TraceEvent is one orchestrator event seen during a subagent run. We
// keep raw JSON for the payload so the event shape stays in sync with
// the SSE stream without a parallel struct to maintain.
type TraceEvent struct {
	At      time.Time       `json:"at"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

// SubagentRun is one subagent invocation. ParentConvID + ParentToolCallID
// link it back to where it was called from; FinalContent is what was
// handed back to the parent as the tool result.
type SubagentRun struct {
	ID               string       `json:"id"`
	ParentConvID     string       `json:"parent_conv_id"`
	ParentToolCallID string       `json:"parent_tool_call_id"`
	SubagentID       string       `json:"subagent_id"`
	Task             string       `json:"task"`
	StartedAt        time.Time    `json:"started_at"`
	FinishedAt       time.Time    `json:"finished_at"`
	FinalContent     string       `json:"final_content"`
	Error            string       `json:"error,omitempty"`
	Events           []TraceEvent `json:"events"`
}

type subagentRunStore struct {
	dir string

	mu  sync.RWMutex
	all map[string]*SubagentRun
}

func newSubagentRunStore(dir string) (*subagentRunStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create subagent_runs dir: %w", err)
	}
	s := &subagentRunStore{dir: dir, all: map[string]*SubagentRun{}}
	if err := s.loadAll(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *subagentRunStore) loadAll() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read subagent_runs dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var r SubagentRun
		if err := json.Unmarshal(raw, &r); err != nil {
			continue
		}
		if r.ID == "" {
			r.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		s.all[r.ID] = &r
	}
	return nil
}

// save persists a SubagentRun atomically.
func (s *subagentRunStore) save(r *SubagentRun) error {
	if r.ID == "" {
		return fmt.Errorf("subagent run id required")
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(s.dir, r.ID+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.mu.Lock()
	cp := *r
	cp.Events = append([]TraceEvent{}, r.Events...)
	s.all[r.ID] = &cp
	s.mu.Unlock()
	return nil
}

func (s *subagentRunStore) get(id string) (*SubagentRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.all[id]
	if !ok {
		return nil, false
	}
	cp := *r
	cp.Events = append([]TraceEvent{}, r.Events...)
	return &cp, true
}

// listForConversation returns all subagent runs that belong to a
// particular conversation, oldest first. Useful for the "show me what
// the subagents in conv X did" debug view.
func (s *subagentRunStore) listForConversation(convID string) []SubagentRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []SubagentRun
	for _, r := range s.all {
		if r.ParentConvID == convID {
			cp := *r
			cp.Events = nil // summary view — caller fetches details with get()
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// traceSink wraps the captureSink to ALSO record every orchestrator
// event verbatim. This is what gets used inside runSubagentCall so we
// have a permanent record of what the subagent did.
type traceSink struct {
	captured *captureSink   // delegate for the "final content" hand-back
	run      *SubagentRun   // accumulating record
}

func newTraceSink(run *SubagentRun) *traceSink {
	return &traceSink{captured: &captureSink{}, run: run}
}

func (t *traceSink) emit(event string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		// don't drop the event entirely — record an empty payload
		raw = []byte("null")
	}
	t.run.Events = append(t.run.Events, TraceEvent{
		At: time.Now().UTC(), Event: event, Payload: raw,
	})
	return t.captured.emit(event, payload)
}

func (t *traceSink) finalContent() string { return t.captured.lastContent }
