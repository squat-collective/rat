package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// devSystemPrompt makes the model a RAT pipeline-development expert and a
// code generator. It is prepended to every conversation.
const devSystemPrompt = `You are the RAT Dev Assistant — an expert at building data pipelines on RAT, a self-hostable data platform.

RAT pipelines:
- A pipeline is a SQL file (DuckDB SQL, Jinja-templated) or a Python file.
- ref('layer.name') resolves another pipeline's output table; ref('namespace.layer.name') works cross-namespace.
- landing_zone('name') resolves the S3 path for uploaded files.
- Layers: bronze (raw ingest), silver (cleaned and joined), gold (aggregated, serving).
- Pipeline config lives in leading SQL comments: -- @merge_strategy: incremental, plus @unique_key, @watermark_column, @partition_column. Merge strategies: full_refresh, incremental, append_only, delete_insert, scd2, snapshot.
- Jinja helpers: is_incremental(), is_scd2(), {{ this }} (the output table).
- A Python pipeline assigns a PyArrow table to the variable "result"; "duckdb_conn" is a pre-configured DuckDB connection.

How to answer:
- Be concise and practical. Briefly explain your reasoning.
- When asked to write or change a pipeline, output the COMPLETE file in ONE fenced code block (sql or python). It must be the whole runnable file — the user applies it to the editor in one click.
- Ground your SQL in the columns shown in the provided file and data sample. Never invent columns.`

// message is one OpenAI-format chat message.
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatContext is the editing context the panel sends with each request.
type chatContext struct {
	Pipeline    pipelineRef `json:"pipeline"`
	Language    string      `json:"language"`
	FileContent string      `json:"file_content"`
	DataSample  string      `json:"data_sample"`
}

type pipelineRef struct {
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Name      string `json:"name"`
}

type chatRequest struct {
	Messages []message    `json:"messages"`
	Context  *chatContext `json:"context"`
}

// chatAPI serves POST /chat — ratd proxies it at /api/v1/x/dev-assistant/chat.
type chatAPI struct {
	ratd *ratdClient
}

func (a *chatAPI) handle(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "messages is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
	defer cancel()

	reply, model, err := a.ratd.chat(ctx, buildMessages(req))
	if err != nil {
		// A soft error — the panel shows it in the conversation.
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reply": reply, "model": model})
}

// buildMessages assembles the full prompt: the dev system prompt (with the
// editing context folded in) followed by the conversation.
func buildMessages(req chatRequest) []message {
	system := devSystemPrompt
	if req.Context != nil {
		if block := contextBlock(req.Context); block != "" {
			system += "\n\n" + block
		}
	}
	msgs := []message{{Role: "system", Content: system}}
	for _, m := range req.Messages {
		if m.Role == "system" {
			continue // the panel must not inject its own system messages
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// contextBlock renders the current pipeline, file and data sample as text the
// model can ground its answers in.
func contextBlock(c *chatContext) string {
	var b strings.Builder
	b.WriteString("--- Current editing context ---")
	if c.Pipeline.Name != "" {
		b.WriteString("\nPipeline: " + c.Pipeline.Namespace + "." +
			c.Pipeline.Layer + "." + c.Pipeline.Name)
	}
	if c.Language != "" {
		b.WriteString("\nLanguage: " + c.Language)
	}
	if s := strings.TrimSpace(c.FileContent); s != "" {
		b.WriteString("\n\nCurrent file:\n" + truncate(s, 6000))
	}
	if s := strings.TrimSpace(c.DataSample); s != "" {
		b.WriteString("\n\nData sample:\n" + truncate(s, 3000))
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n…(truncated)"
	}
	return s
}
