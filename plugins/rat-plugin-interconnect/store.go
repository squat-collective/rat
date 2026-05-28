package main

import (
	"sort"
	"sync"
	"time"
)

// Capability is a named service one plugin offers and others can invoke
// through the broker — without hardcoding the provider's name or routes.
type Capability struct {
	Name         string    `json:"name"`        // unique id, e.g. "data.analyze"
	Provider     string    `json:"provider"`    // the plugin that provides it
	Method       string    `json:"method"`      // HTTP method used to invoke it
	Path         string    `json:"path"`        // route on the provider, e.g. "/analyze"
	Description  string    `json:"description"`
	Consumers    []string  `json:"consumers"`   // plugins that declared they use it
	RegisteredAt time.Time `json:"registered_at"`
}

// store is the in-memory capability registry. Capabilities are keyed by name;
// re-registering a name replaces it. State is lost on restart — fine for an
// example plugin.
type store struct {
	mu   sync.RWMutex
	caps map[string]*Capability
}

func newStore() *store {
	return &store{caps: map[string]*Capability{}}
}

func (s *store) register(c *Capability) *Capability {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.RegisteredAt = time.Now().UTC()
	if c.Consumers == nil {
		c.Consumers = []string{}
	}
	s.caps[c.Name] = c
	return c
}

func (s *store) get(name string) (*Capability, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.caps[name]
	return c, ok
}

func (s *store) list() []*Capability {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Capability, 0, len(s.caps))
	for _, c := range s.caps {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *store) delete(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.caps[name]; !ok {
		return false
	}
	delete(s.caps, name)
	return true
}
