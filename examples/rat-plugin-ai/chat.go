package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// maxToolRounds bounds one agent's tool-calling loop.
	maxToolRounds = 14
	// turnTimeout must stay under ratd's 120s HTTP WriteTimeout so the plugin
	// always finishes and sends a response before the proxy cuts the
	// connection — otherwise the answer never reaches the chat.
	turnTimeout = 100 * time.Second
)

// ── Agent prompts ─────────────────────────────────────────────────
//
// The AI is a small team of agents. Each has a narrow job and a small tool
// set — focused agents are far more reliable on a small model than one agent
// juggling everything. The orchestrator only routes; the specialists do the
// work.

const orchestratorPrompt = `You are the RAT Data Navigator, an AI assistant in the RAT data platform.
You COORDINATE a small team of specialists — you do not query data or draw
charts yourself.

- For ANY question about the data (tables, schemas, row counts, values,
  comparisons, analysis), call query_data with the user's question in plain
  English.
- For ANY request to chart, plot, graph or visualise data, call create_chart
  describing what to draw in plain English.
- For greetings or general questions, just reply directly.

If the user wants both an answer and a chart, call query_data first, then
create_chart. Relay the specialists' results to the user clearly and concisely.
Never put images or base64 data in your reply.`

const sqlAgentPrompt = `You are a DuckDB SQL specialist for the RAT data platform. Given one data
question, use your tools to answer it precisely, then reply with the answer.

Tables are namespace.layer.name (e.g. default.bronze.orders); the layers are
bronze, silver and gold. Use list_tables to discover tables and describe_table
for schemas — never guess table or column names. Use run_query for all data.

To count rows in a table: SELECT count(*) FROM namespace.layer.name. For counts
across several tables, call list_tables first, then run ONE query that
UNION ALLs a per-table SELECT — using each table's REAL name as the label
literal, e.g. SELECT 'default.bronze.fr_orders' AS table_name, count(*) AS rows
FROM default.bronze.fr_orders. Never use placeholders like 'Table1'.

A run_query result is {"columns":[...],"rows":[...]}. The answer to a "how
many" question is the VALUE inside the first row, not the number of rows.
Answer concisely with the actual numbers you got back.`

const chartAgentPrompt = `You are a data-visualisation specialist for the RAT data platform. You draw
charts to answer the user's request.

Work through the request:
1. Decide exactly what to plot — which column gives the labels, which gives the
   numeric values. Match the user's intent precisely: "amount by name" means
   select the amount column (or SUM it per name); only COUNT rows when the user
   explicitly asks for counts or frequencies.
2. Use list_tables and describe_table for the real table and column names —
   never guess them. Inspect only the tables you will actually chart.
3. If the values need aggregating, use run_query to confirm the SQL works.
4. Call render_chart to draw the chart — it is shown to the user automatically.

render_chart takes: chart_type (bar, line, area, pie, donut or radar), a title,
a SQL query, label_column, and value_columns — one or more numeric columns;
list several to plot multiple series. It also takes an optional "options"
object to style the chart: a palette (rat, vivid, ocean, sunset, mono) or
explicit colors (hex per series), plus stacked, curve (smooth/linear/step),
dots, horizontal, bar_radius, inner_radius, show_labels, hide_grid,
hide_legend. Pick the chart type and styling that best suit the data — e.g. a
donut for shares of a whole, line or area for trends over time, radar to
compare entities across several metrics, stacked bars for parts of a total.
Tables are namespace.layer.name (layers: bronze, silver, gold).

To chart row counts across all tables: list_tables first, then UNION ALL — per
table — a SELECT '<real table name>' AS table_name, count(*) AS rows FROM
<that table>; use real names, never placeholders.

Charts show automatically — never put images or base64 in your reply. Finish
with a one or two sentence summary of what the chart shows.`

var errToolBudget = errors.New("an agent exceeded its tool-call budget — try a more specific question")

// session is one continuable conversation — the orchestrator's message
// history. Its mutex serialises turns so it is never mutated by two requests
// at once.
type session struct {
	mu       sync.Mutex
	messages []chatMessage
}

// chatService runs the orchestrator and its specialist sub-agents.
type chatService struct {
	ai    *aiClient
	tools *dataTools

	mu       sync.Mutex
	sessions map[string]*session
}

func newChatService(ai *aiClient, tools *dataTools) *chatService {
	return &chatService{ai: ai, tools: tools, sessions: map[string]*session{}}
}

type chatRequestBody struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// chatStep records one delegation the orchestrator made — shown in the UI so
// the conversation is transparent.
type chatStep struct {
	Tool string `json:"tool"`
	Args string `json:"args"`
}

type chatResponseBody struct {
	SessionID string      `json:"session_id"`
	Reply     string      `json:"reply,omitempty"`
	Steps     []chatStep  `json:"steps,omitempty"`
	Charts    []chartSpec `json:"charts,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// HandleChat is the HTTP handler for POST /chat (proxied at /api/v1/x/ai/chat).
func (s *chatService) HandleChat(w http.ResponseWriter, r *http.Request) {
	var body chatRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, chatResponseBody{Error: "invalid request body"})
		return
	}
	if body.Message == "" {
		writeJSON(w, http.StatusBadRequest, chatResponseBody{Error: "message is required"})
		return
	}

	sess, id := s.session(body.SessionID)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	sess.messages = append(sess.messages, chatMessage{Role: "user", Content: body.Message})

	ctx, cancel := context.WithTimeout(r.Context(), turnTimeout)
	defer cancel()

	// The orchestrator agent runs on the session history.
	reply, steps, charts, err := s.runAgent(ctx, orchestratorPrompt, orchestratorTools, &sess.messages)
	if err != nil {
		slog.Warn("chat turn failed", "session", id, "error", err)
		msg := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			msg = "That took too long to answer — try a more specific question, " +
				"or break it into smaller steps."
		}
		writeJSON(w, http.StatusOK, chatResponseBody{
			SessionID: id, Steps: steps, Charts: charts, Error: msg,
		})
		return
	}
	writeJSON(w, http.StatusOK, chatResponseBody{
		SessionID: id, Reply: reply, Steps: steps, Charts: charts,
	})
}

// runAgent drives one agent's tool-calling loop. It appends the agent's
// assistant + tool messages to conv — so the orchestrator's conv is the
// persistent session, while a sub-agent's conv is a throwaway slice. Returns
// the agent's final text, the tool calls it made, and any charts produced.
func (s *chatService) runAgent(
	ctx context.Context, systemPrompt string, tools []toolDef, conv *[]chatMessage,
) (string, []chatStep, []chartSpec, error) {
	var steps []chatStep
	var charts []chartSpec

	for round := 0; round < maxToolRounds; round++ {
		req := make([]chatMessage, 0, len(*conv)+1)
		req = append(req, chatMessage{Role: "system", Content: systemPrompt})
		req = append(req, (*conv)...)

		msg, err := s.ai.complete(ctx, req, tools)
		if err != nil {
			return "", steps, charts, err
		}
		*conv = append(*conv, msg)

		if len(msg.ToolCalls) == 0 {
			return msg.Content, steps, charts, nil
		}

		for _, tc := range msg.ToolCalls {
			slog.Info("tool call", "tool", tc.Function.Name, "args", tc.Function.Arguments)
			steps = append(steps, chatStep{Tool: tc.Function.Name, Args: tc.Function.Arguments})
			result, tcCharts := s.execTool(ctx, tc.Function.Name, tc.Function.Arguments)
			charts = append(charts, tcCharts...)
			*conv = append(*conv, chatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			})
		}
	}
	return "", steps, charts, errToolBudget
}

// execTool runs one tool call. Delegation tools (query_data, create_chart)
// spawn a focused specialist sub-agent; leaf tools run directly against ratd.
func (s *chatService) execTool(ctx context.Context, name, args string) (string, []chartSpec) {
	switch name {
	case "query_data":
		var a struct {
			Question string `json:"question"`
		}
		if err := json.Unmarshal([]byte(args), &a); err != nil || a.Question == "" {
			return `{"error":"query_data requires a question"}`, nil
		}
		conv := []chatMessage{{Role: "user", Content: a.Question}}
		reply, _, _, err := s.runAgent(ctx, sqlAgentPrompt, sqlAgentTools, &conv)
		if err != nil {
			return toolError(err), nil
		}
		return reply, nil

	case "create_chart":
		var a struct {
			Request string `json:"request"`
		}
		if err := json.Unmarshal([]byte(args), &a); err != nil || a.Request == "" {
			return `{"error":"create_chart requires a request"}`, nil
		}
		conv := []chatMessage{{Role: "user", Content: a.Request}}
		reply, _, charts, err := s.runAgent(ctx, chartAgentPrompt, chartAgentTools, &conv)
		if err != nil {
			return toolError(err), charts
		}
		return reply, charts

	default:
		// Leaf tool — run it directly against ratd.
		result, chart := s.tools.execute(ctx, name, args)
		if chart != nil {
			return result, []chartSpec{*chart}
		}
		return result, nil
	}
}

// session returns the session for id, creating it (and an id) when needed.
func (s *chatService) session(id string) (*session, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		id = "sess-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	sess, ok := s.sessions[id]
	if !ok {
		sess = &session{}
		s.sessions[id] = sess
	}
	return sess, id
}
