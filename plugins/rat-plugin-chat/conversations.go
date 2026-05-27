package main

// Conversation storage. Each conversation is one JSON file at
// /data/conversations/{id}.json — chosen over a database for two reasons:
//   1. zero schema migration; survives container restarts via a mounted
//      volume.
//   2. "give me access to the AI history" means Tom (or me) can just
//      `docker exec chat cat /data/conversations/*.json` to inspect any
//      session. No ratd round-trips, no SQL.
//
// Writes are atomic (write to .tmp + rename). The store is in-memory
// indexed at startup; subsequent reads/writes both keep the cache and
// the file in sync.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Conversation is one chat session. Messages is the full transcript in
// OpenAI message format — user / assistant / tool / system — which is
// exactly what the orchestrator hands to ai-provider for the next turn.
type Conversation struct {
	ID        string        `json:"id"`
	AgentID   string        `json:"agent_id"`
	Title     string        `json:"title"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Messages  []chatMessage `json:"messages"`
}

// ConversationSummary is what list endpoints return — same fields minus
// the heavyweight messages array.
type ConversationSummary struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	MessageCount int    `json:"message_count"`
}

type conversationStore struct {
	dir string

	mu    sync.RWMutex
	byID  map[string]*Conversation
}

func newConversationStore(dir string) (*conversationStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create conv dir: %w", err)
	}
	s := &conversationStore{dir: dir, byID: map[string]*Conversation{}}
	if err := s.loadAll(); err != nil {
		return nil, err
	}
	return s, nil
}

// loadAll scans the directory at startup so a container restart picks
// up everything previously persisted.
func (s *conversationStore) loadAll() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read conv dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var c Conversation
		if err := json.Unmarshal(raw, &c); err != nil {
			continue
		}
		if c.ID == "" {
			c.ID = strings.TrimSuffix(e.Name(), ".json")
		}
		s.byID[c.ID] = &c
	}
	return nil
}

// list returns all conversations sorted most-recent-first.
func (s *conversationStore) list() []ConversationSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ConversationSummary, 0, len(s.byID))
	for _, c := range s.byID {
		out = append(out, ConversationSummary{
			ID: c.ID, AgentID: c.AgentID, Title: c.Title,
			CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
			MessageCount: len(c.Messages),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *conversationStore) get(id string) (*Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	cp := *c
	cp.Messages = append([]chatMessage{}, c.Messages...)
	return &cp, true
}

// create makes a new empty conversation. title and agentID are optional —
// the chat handler usually derives the title from the first user message
// after the conversation is created.
func (s *conversationStore) create(agentID, title string) (*Conversation, error) {
	now := time.Now().UTC()
	c := &Conversation{
		ID:        "conv_" + randomID(8),
		AgentID:   agentID,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []chatMessage{},
	}
	s.mu.Lock()
	s.byID[c.ID] = c
	s.mu.Unlock()
	if err := s.persist(c); err != nil {
		return nil, err
	}
	return c, nil
}

// save persists the conversation back to disk and updates the in-memory
// cache. The caller hands us a fully-formed conversation (this is how
// the orchestrator commits its mutations after each turn).
func (s *conversationStore) save(c *Conversation) error {
	if c.ID == "" {
		return errors.New("save: conversation has no id")
	}
	c.UpdatedAt = time.Now().UTC()

	cp := *c
	cp.Messages = append([]chatMessage{}, c.Messages...)
	s.mu.Lock()
	s.byID[c.ID] = &cp
	s.mu.Unlock()

	return s.persist(&cp)
}

// rename updates the title only.
func (s *conversationStore) rename(id, title string) (*Conversation, error) {
	s.mu.Lock()
	c, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("conversation %q not found", id)
	}
	c.Title = title
	c.UpdatedAt = time.Now().UTC()
	cp := *c
	cp.Messages = append([]chatMessage{}, c.Messages...)
	s.mu.Unlock()
	if err := s.persist(&cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func (s *conversationStore) delete(id string) error {
	s.mu.Lock()
	_, ok := s.byID[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("conversation %q not found", id)
	}
	delete(s.byID, id)
	s.mu.Unlock()
	path := filepath.Join(s.dir, id+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove file: %w", err)
	}
	return nil
}

// persist writes the conversation to disk atomically (tmp + rename) so
// a concurrent reader never sees a half-written file.
func (s *conversationStore) persist(c *Conversation) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(s.dir, c.ID+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// deriveTitle takes a free-text message and produces a sidebar-friendly
// title (first line, trimmed, capped at 60 chars).
func deriveTitle(text string) string {
	t := strings.TrimSpace(text)
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimSpace(t)
	if t == "" {
		return "(empty)"
	}
	if len(t) > 60 {
		// Cut on a word boundary if we can.
		cut := strings.LastIndex(t[:60], " ")
		if cut < 30 {
			cut = 60
		}
		t = t[:cut] + "…"
	}
	return t
}

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
