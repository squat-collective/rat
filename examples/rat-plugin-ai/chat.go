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
	maxToolRounds = 6
	turnTimeout   = 5 * time.Minute

	systemPrompt = `You are the RAT Data Navigator, an AI assistant embedded in the RAT data platform.
You help users explore and analyse their data through conversation.

You have tools to list tables, inspect table schemas, and run read-only SQL
(DuckDB) queries. When a question is about the data, USE THE TOOLS — inspect
schemas and run queries rather than guessing. Tables are referenced as
namespace.layer.name (for example default.bronze.orders); the layers are
bronze, silver and gold.

For any question about row counts, sums, averages, or specific data values,
you MUST use run_query with a SQL query (e.g. SELECT count(*) FROM
default.bronze.orders). list_tables only lists table names — it does not
contain row counts or data.

A run_query result has the form {"columns": [...], "rows": [...]}. The answer
to a "how many" or aggregate question is the numeric VALUE inside the first
row — NOT how many rows the result has. Example: a result of
{"rows": [{"count_star()": 42}]} means the count is 42 (not 1).

Keep answers concise and grounded in the results you actually get back.`
)

var errToolBudget = errors.New("the assistant exceeded its tool-call budget — try a more specific question")

// session is one continuable conversation. Its mutex serialises turns so a
// session is never mutated by two requests at once.
type session struct {
	mu       sync.Mutex
	messages []chatMessage
}

// chatService holds all sessions and drives the chat + tool-calling loop.
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

// chatStep records one tool call the assistant made — surfaced to the UI so the
// conversation is transparent rather than a black box.
type chatStep struct {
	Tool string `json:"tool"`
	Args string `json:"args"`
}

type chatResponseBody struct {
	SessionID string     `json:"session_id"`
	Reply     string     `json:"reply,omitempty"`
	Steps     []chatStep `json:"steps,omitempty"`
	Error     string     `json:"error,omitempty"`
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

	// Serialise turns within a session.
	sess.mu.Lock()
	defer sess.mu.Unlock()

	sess.messages = append(sess.messages, chatMessage{Role: "user", Content: body.Message})

	ctx, cancel := context.WithTimeout(r.Context(), turnTimeout)
	defer cancel()

	reply, steps, err := s.runTurn(ctx, sess)
	if err != nil {
		slog.Warn("chat turn failed", "session", id, "error", err)
		writeJSON(w, http.StatusOK, chatResponseBody{SessionID: id, Steps: steps, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, chatResponseBody{SessionID: id, Reply: reply, Steps: steps})
}

// runTurn drives the tool-calling loop: ask the model, run any tools it
// requests, feed the results back, and repeat until it returns a text answer.
func (s *chatService) runTurn(ctx context.Context, sess *session) (string, []chatStep, error) {
	var steps []chatStep
	for round := 0; round < maxToolRounds; round++ {
		msg, err := s.ai.complete(ctx, s.withSystem(sess.messages), toolSpecs)
		if err != nil {
			return "", steps, err
		}
		sess.messages = append(sess.messages, msg)

		if len(msg.ToolCalls) == 0 {
			return msg.Content, steps, nil
		}

		for _, tc := range msg.ToolCalls {
			slog.Info("tool call", "tool", tc.Function.Name, "args", tc.Function.Arguments)
			steps = append(steps, chatStep{Tool: tc.Function.Name, Args: tc.Function.Arguments})
			result := s.tools.execute(ctx, tc.Function.Name, tc.Function.Arguments)
			sess.messages = append(sess.messages, chatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			})
		}
	}
	return "", steps, errToolBudget
}

// withSystem prepends the system prompt to the conversation for each request.
func (s *chatService) withSystem(msgs []chatMessage) []chatMessage {
	out := make([]chatMessage, 0, len(msgs)+1)
	out = append(out, chatMessage{Role: "system", Content: systemPrompt})
	return append(out, msgs...)
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
