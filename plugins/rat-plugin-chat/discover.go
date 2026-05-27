package main

// discover finds MCP servers by polling the interconnect plugin for
// capabilities whose name starts with "mcp.server." — that's the convention
// the chat plugin and the MCP-server plugins both agree on.
//
// Once an MCP server is discovered, we initialize a session and cache its
// tool list. The cache is refreshed on a fixed interval so newly-deployed
// MCP servers show up live, without restarting chat.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const mcpCapabilityPrefix = "mcp.server."

// MCPServer is one MCP server the chat plugin knows about — its
// interconnect capability name + the human server name we surface to the UI.
type MCPServer struct {
	Capability  string    `json:"capability"`  // e.g. "mcp.server.docs"
	Name        string    `json:"name"`        // e.g. "mcp-docs" (provider)
	Description string    `json:"description"`
	Tools       []MCPTool `json:"tools"`
	LastError   string    `json:"last_error,omitempty"`
	LastSeen    time.Time `json:"last_seen"`
}

// discoverer keeps a live view of MCP servers reachable through the broker.
type discoverer struct {
	ratdURL string
	mcp     *mcpClient
	http    *http.Client

	mu      sync.RWMutex
	servers map[string]*MCPServer // keyed by capability
}

func newDiscoverer(ratdURL string, mcp *mcpClient) *discoverer {
	return &discoverer{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		mcp:     mcp,
		http:    &http.Client{Timeout: 10 * time.Second},
		servers: map[string]*MCPServer{},
	}
}

// list returns a snapshot of the discovered servers.
func (d *discoverer) list() []*MCPServer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*MCPServer, 0, len(d.servers))
	for _, s := range d.servers {
		copy := *s
		out = append(out, &copy)
	}
	return out
}

// findByCapability returns one server descriptor, or nil if absent.
func (d *discoverer) findByCapability(cap string) *MCPServer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if s, ok := d.servers[cap]; ok {
		c := *s
		return &c
	}
	return nil
}

// findToolByName looks up a tool by its namespaced name (e.g. "docs__list_tables")
// across all discovered servers. Returns the server + the tool's *original*
// name so we can call it.
func (d *discoverer) findToolByName(nsName string) (*MCPServer, string, bool) {
	server, originalName, ok := splitNamespacedName(nsName)
	if !ok {
		return nil, "", false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	// server here is the short form (e.g. "docs"); match by suffix of name.
	for _, s := range d.servers {
		if s.Name == "mcp-"+server || s.Name == server {
			for _, t := range s.Tools {
				if t.Name == originalName {
					sc := *s
					return &sc, originalName, true
				}
			}
		}
	}
	return nil, "", false
}

// allTools flattens the tool list across all discovered servers, with the
// namespaced names — this is what the chat orchestrator hands to the LLM.
func (d *discoverer) allTools() []MCPTool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var out []MCPTool
	for _, s := range d.servers {
		out = append(out, s.Tools...)
	}
	return out
}

// poll loops, refreshing the registry on each tick.
func (d *discoverer) poll(ctx context.Context, interval time.Duration) {
	d.refresh(ctx) // immediate first pass
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.refresh(ctx)
		}
	}
}

// refresh re-reads the interconnect's capability list, then for any
// capability under "mcp.server." re-runs initialize + tools/list. We don't
// bother diffing — listing/initializing is cheap and the broker handles
// liveness already.
func (d *discoverer) refresh(ctx context.Context) {
	caps, err := d.listCapabilities(ctx)
	if err != nil {
		slog.Warn("discover: failed to list capabilities", "error", err)
		return
	}
	seen := map[string]bool{}
	for _, c := range caps {
		if !strings.HasPrefix(c.Name, mcpCapabilityPrefix) {
			continue
		}
		seen[c.Name] = true
		srv := &MCPServer{
			Capability:  c.Name,
			Name:        c.Provider,
			Description: c.Description,
			LastSeen:    time.Now(),
		}
		// Short timeouts so a slow MCP server doesn't stall the whole poll.
		initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := d.mcp.initialize(initCtx, c.Name); err != nil {
			srv.LastError = "initialize: " + err.Error()
			cancel()
			d.commit(srv)
			continue
		}
		cancel()
		toolCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		tools, err := d.mcp.listTools(toolCtx, c.Name, c.Provider)
		cancel()
		if err != nil {
			srv.LastError = "tools/list: " + err.Error()
			d.commit(srv)
			continue
		}
		srv.Tools = tools
		d.commit(srv)
	}
	// Drop any server that vanished from the broker.
	d.mu.Lock()
	for k := range d.servers {
		if !seen[k] {
			delete(d.servers, k)
		}
	}
	d.mu.Unlock()
}

func (d *discoverer) commit(s *MCPServer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers[s.Capability] = s
}

// brokerCapability is what GET /api/v1/x/interconnect/capabilities returns.
type brokerCapability struct {
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// listCapabilities hits the interconnect plugin to get all known capabilities.
func (d *discoverer) listCapabilities(ctx context.Context) ([]brokerCapability, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		d.ratdURL+"/api/v1/x/interconnect/capabilities", nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	var out []brokerCapability
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
