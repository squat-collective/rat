package main

// mcpClient is a minimal JSON-RPC 2.0 client for the MCP servers this
// plugin orchestrates. We always reach those servers *through the
// interconnect broker* (POST /api/v1/x/interconnect/invoke with capability +
// payload) so we never hardcode plugin URLs — the chat plugin discovers
// servers by capability name and routes through the broker.
//
// The MCP slice we use is just three calls: initialize (handshake),
// tools/list (catalog), and tools/call (invoke). Same shape as the servers
// in rat-plugin-mcp-docs and rat-plugin-mcp-sql.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const mcpProtocolVersion = "2025-03-26"

// MCPTool is one tool exposed by an MCP server. We carry the server name so
// the orchestrator knows which server to route a call back to when the LLM
// picks a tool by its namespaced name (e.g. "docs__list_tables").
type MCPTool struct {
	Server      string         `json:"server"`       // e.g. "mcp-docs"
	Name        string         `json:"name"`         // e.g. "list_tables"
	NSName      string         `json:"namespaced"`   // e.g. "docs__list_tables"
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// mcpClient talks to MCP servers via the interconnect broker.
type mcpClient struct {
	ratdURL string
	http    *http.Client
	id      atomic.Int64 // monotonically-increasing JSON-RPC id
}

func newMCPClient(ratdURL string) *mcpClient {
	return &mcpClient{
		ratdURL: strings.TrimRight(ratdURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// invoke posts a JSON-RPC message to the named capability through the
// interconnect broker and returns the inner JSON-RPC result.
func (c *mcpClient) invoke(ctx context.Context, capability, method string, params any) (json.RawMessage, error) {
	id := c.id.Add(1)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	body, _ := json.Marshal(map[string]any{
		"capability": capability,
		"payload":    payload,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.ratdURL+"/api/v1/x/interconnect/invoke", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("broker invoke: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("broker invoke: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	// The broker envelope wraps the MCP server's response in {body: ...}.
	var envelope struct {
		Error string          `json:"error,omitempty"`
		Body  json.RawMessage `json:"body,omitempty"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode broker envelope: %w", err)
	}
	if envelope.Error != "" {
		return nil, fmt.Errorf("broker: %s", envelope.Error)
	}
	// Unwrap the JSON-RPC layer to return just the result.
	var rpc struct {
		Result json.RawMessage `json:"result,omitempty"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(envelope.Body, &rpc); err != nil {
		return nil, fmt.Errorf("decode JSON-RPC: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	return rpc.Result, nil
}

// initialize completes the MCP handshake. We do not actually use anything
// from the response yet — we just need to make sure the server speaks the
// version we expect.
func (c *mcpClient) initialize(ctx context.Context, capability string) error {
	_, err := c.invoke(ctx, capability, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "rat-plugin-chat", "version": pluginVersion},
	})
	return err
}

// listTools returns the tools the named server exposes.
func (c *mcpClient) listTools(ctx context.Context, capability, serverName string) ([]MCPTool, error) {
	raw, err := c.invoke(ctx, capability, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode tools/list: %w", err)
	}
	out := make([]MCPTool, 0, len(resp.Tools))
	for _, t := range resp.Tools {
		out = append(out, MCPTool{
			Server:      serverName,
			Name:        t.Name,
			NSName:      namespaceToolName(serverName, t.Name),
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return out, nil
}

// callTool invokes a tool on the named MCP server. Returns the text payload
// and a boolean for whether the server flagged it as an error result (in
// which case we still feed the text back to the LLM so it can recover).
func (c *mcpClient) callTool(ctx context.Context, capability, toolName string, args map[string]any) (string, bool, error) {
	raw, err := c.invoke(ctx, capability, "tools/call", map[string]any{
		"name": toolName, "arguments": args,
	})
	if err != nil {
		return "", false, err
	}
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", false, fmt.Errorf("decode tools/call: %w", err)
	}
	var b strings.Builder
	for _, c := range resp.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String(), resp.IsError, nil
}

// namespaceToolName creates the public tool name we expose to the LLM.
// Strips the "mcp-" prefix from server names so the model sees concise
// names like "docs__list_tables" rather than "mcp-docs__list_tables".
func namespaceToolName(server, name string) string {
	s := strings.TrimPrefix(server, "mcp-")
	return s + "__" + name
}

// splitNamespacedName reverses the namespacing — used to find which server
// the LLM's tool_call belongs to.
func splitNamespacedName(ns string) (server, name string, ok bool) {
	i := strings.Index(ns, "__")
	if i < 0 {
		return "", "", false
	}
	return ns[:i], ns[i+2:], true
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
