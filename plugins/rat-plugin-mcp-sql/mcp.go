package main

// Minimal Model Context Protocol (MCP) server, JSON-RPC 2.0 over HTTP+SSE
// (the "Streamable HTTP" transport). We implement the slice of the spec a
// well-behaved MCP client needs to discover and call tools:
//
//   initialize                — handshake (returns server name + capabilities)
//   notifications/initialized — client says "ready"; no response
//   tools/list                — return our tool catalog
//   tools/call                — invoke a tool, return its result
//
// One small file so the whole server fits in your head — both this and
// rat-plugin-mcp-sql use the same pattern.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Protocol version we advertise on initialize. MCP versions are
// negotiated, but for an in-org server fixing it is fine.
const mcpProtocolVersion = "2025-03-26"

// ── JSON-RPC plumbing ──────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // null for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

// ── Tool registry ──────────────────────────────────────────────────

// Tool is one callable function the server exposes. Handler returns the
// content to put in the MCP tools/call response (free-form text is the
// most portable representation).
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]any         `json:"inputSchema"`
	Handler     func(ctx context.Context, args map[string]any) (string, error) `json:"-"`
}

// Server is a tiny MCP server. Register tools with Add, then mount
// ServeHTTP at the /mcp path on whatever mux you like.
type Server struct {
	name    string
	version string
	tools   []Tool
}

func NewServer(name, version string) *Server {
	return &Server{name: name, version: version}
}

func (s *Server) Add(t Tool) {
	s.tools = append(s.tools, t)
}

// ServeHTTP handles a single JSON-RPC request and writes the response.
// Notifications (no id) get an empty 204.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
	if err != nil {
		writeRPCError(w, nil, codeParseError, "read body: "+err.Error())
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeRPCError(w, nil, codeParseError, "invalid JSON: "+err.Error())
		return
	}
	if req.JSONRPC != "2.0" {
		writeRPCError(w, req.ID, codeInvalidRequest, "jsonrpc must be 2.0")
		return
	}

	// Notifications carry no id and expect no response.
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		writeRPCResult(w, req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
		})
	case "notifications/initialized", "notifications/cancelled":
		w.WriteHeader(http.StatusNoContent)
	case "tools/list":
		writeRPCResult(w, req.ID, map[string]any{"tools": s.toolDescriptors()})
	case "tools/call":
		s.handleToolCall(r.Context(), w, req)
	case "ping":
		writeRPCResult(w, req.ID, map[string]any{})
	default:
		if isNotification {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeRPCError(w, req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

func (s *Server) toolDescriptors() []map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return out
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (s *Server) handleToolCall(ctx context.Context, w http.ResponseWriter, req rpcRequest) {
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeRPCError(w, req.ID, codeInvalidRequest, "invalid tool call params: "+err.Error())
		return
	}
	var found *Tool
	for i := range s.tools {
		if s.tools[i].Name == p.Name {
			found = &s.tools[i]
			break
		}
	}
	if found == nil {
		writeRPCResult(w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "unknown tool: " + p.Name}},
			"isError": true,
		})
		return
	}
	text, err := found.Handler(ctx, p.Arguments)
	if err != nil {
		// Tool errors are returned as an MCP isError result, not a JSON-RPC
		// error — that's what the spec says, and it lets the model see what
		// went wrong and retry.
		writeRPCResult(w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		})
		return
	}
	writeRPCResult(w, req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

// ── Response helpers ───────────────────────────────────────────────

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	resp := rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	resp := rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ── Convenience for handler implementations ─────────────────────────

// argString reads a required string arg and returns a clear error if missing.
func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %s must be a string", key)
	}
	return s, nil
}

// argStringOpt reads an optional string, returning fallback if absent.
func argStringOpt(args map[string]any, key, fallback string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

// argIntOpt reads an optional integer (JSON numbers are float64).
func argIntOpt(args map[string]any, key string, fallback int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return fallback
}

// jsonString marshals v to a JSON string for returning as MCP text content.
// Pretty-printed because LLMs read it.
func jsonString(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "marshal error: " + err.Error()
	}
	return string(b)
}

// Sentinel so handlers can return a "no data" hint without it looking like
// a real error to the model.
var errNoData = errors.New("no data found")
