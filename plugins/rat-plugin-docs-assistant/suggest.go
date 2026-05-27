package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// docsSystemPrompt instructs the model to behave as a documentation writer
// and return STRICT JSON. The /suggest handler then parses that JSON.
const docsSystemPrompt = `You are a data documentation writer. Given a table's identifier, its columns and (optionally) a sample of its rows, you produce concise documentation.

You ALWAYS reply with a single JSON object — and NOTHING else. No prose, no preamble, no markdown, no code fences.

Shape:
{
  "description": "<one short sentence (10-20 words) describing what the table contains>",
  "column_descriptions": { "<column_name>": "<one short sentence>", ... }
}

Rules:
- Keys in column_descriptions MUST match the supplied column names exactly.
- Ground descriptions in the data sample when provided. If a column's meaning is genuinely unclear, write "(no description)" rather than inventing.
- Tense: present. Style: concrete and factual. Avoid filler like "This column contains" or "The id of".
- Do not include columns the user did not supply.`

// message is one OpenAI-format chat message — shared with ratd.go.
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type tableRef struct {
	Namespace string `json:"namespace"`
	Layer     string `json:"layer"`
	Name      string `json:"name"`
}

type columnRef struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// suggestRequest is the panel → plugin call.
type suggestRequest struct {
	Table                     tableRef          `json:"table"`
	Columns                   []columnRef       `json:"columns"`
	CurrentDescription        string            `json:"current_description"`
	CurrentColumnDescriptions map[string]string `json:"current_column_descriptions"`
	DataSample                string            `json:"data_sample"`
}

// suggestResponse is what the plugin returns. The panel renders these as
// editable suggestions the user reviews and saves.
type suggestResponse struct {
	Description        string            `json:"description"`
	ColumnDescriptions map[string]string `json:"column_descriptions"`
	Model              string            `json:"model,omitempty"`
	Error              string            `json:"error,omitempty"`
}

type suggestAPI struct {
	ratd *ratdClient
}

func (a *suggestAPI) handle(w http.ResponseWriter, r *http.Request) {
	var req suggestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Columns) == 0 {
		writeErr(w, http.StatusBadRequest, "columns is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
	defer cancel()

	msgs := []message{
		{Role: "system", Content: docsSystemPrompt},
		{Role: "user", Content: buildUserPrompt(req)},
	}
	reply, model, err := a.ratd.chat(ctx, msgs)
	if err != nil {
		writeJSON(w, http.StatusOK, suggestResponse{Error: err.Error()})
		return
	}
	parsed, perr := parseSuggestion(reply, req.Columns)
	if perr != nil {
		writeJSON(w, http.StatusOK, suggestResponse{Error: perr.Error(), Model: model})
		return
	}
	parsed.Model = model
	writeJSON(w, http.StatusOK, parsed)
}

func buildUserPrompt(req suggestRequest) string {
	var b strings.Builder
	b.WriteString("Table: ")
	b.WriteString(req.Table.Namespace)
	b.WriteString(".")
	b.WriteString(req.Table.Layer)
	b.WriteString(".")
	b.WriteString(req.Table.Name)
	b.WriteString("\nLayer: ")
	b.WriteString(req.Table.Layer)
	b.WriteString("\nColumns:")
	for _, c := range req.Columns {
		b.WriteString("\n- ")
		b.WriteString(c.Name)
		if c.Type != "" {
			b.WriteString(" (")
			b.WriteString(c.Type)
			b.WriteString(")")
		}
	}
	if cur := strings.TrimSpace(req.CurrentDescription); cur != "" {
		b.WriteString("\n\nCurrent table description:\n")
		b.WriteString(cur)
	}
	if len(req.CurrentColumnDescriptions) > 0 {
		var lines []string
		for k, v := range req.CurrentColumnDescriptions {
			if s := strings.TrimSpace(v); s != "" {
				lines = append(lines, "- "+k+": "+s)
			}
		}
		if len(lines) > 0 {
			b.WriteString("\n\nCurrent column descriptions:\n")
			b.WriteString(strings.Join(lines, "\n"))
		}
	}
	if s := strings.TrimSpace(req.DataSample); s != "" {
		b.WriteString("\n\nData sample:\n")
		if len(s) > 3000 {
			s = s[:3000] + "\n…(truncated)"
		}
		b.WriteString(s)
	}
	return b.String()
}

var errInvalidJSON = errors.New("the AI did not return valid JSON — try again")

// parseSuggestion extracts the JSON object the model produced. Strips a code
// fence if the model wrapped its reply, isolates the first {...} block, and
// keeps only the columns we asked for.
func parseSuggestion(text string, cols []columnRef) (suggestResponse, error) {
	clean := stripCodeFence(text)
	clean = strings.TrimSpace(clean)
	if i := strings.Index(clean, "{"); i > 0 {
		clean = clean[i:]
	}
	if j := strings.LastIndex(clean, "}"); j >= 0 && j < len(clean)-1 {
		clean = clean[:j+1]
	}
	var s suggestResponse
	if err := json.Unmarshal([]byte(clean), &s); err != nil {
		return suggestResponse{}, errInvalidJSON
	}
	if len(s.ColumnDescriptions) > 0 {
		want := make(map[string]bool, len(cols))
		for _, c := range cols {
			want[c.Name] = true
		}
		filtered := make(map[string]string, len(cols))
		for k, v := range s.ColumnDescriptions {
			if want[k] {
				filtered[k] = strings.TrimSpace(v)
			}
		}
		s.ColumnDescriptions = filtered
	}
	return s, nil
}

func stripCodeFence(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "```") {
		if i := strings.Index(t, "\n"); i >= 0 {
			t = t[i+1:]
		}
		if j := strings.LastIndex(t, "```"); j >= 0 {
			t = t[:j]
		}
	}
	return strings.TrimSpace(t)
}
