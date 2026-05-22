package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// analyzeSystemPrompt steers the one-shot data-analysis endpoint used by the
// charts plugin's AI-analysis dashboard component.
const analyzeSystemPrompt = `You are a concise data analyst. Given a question and (optionally) a dataset,
write a short, insightful analysis in plain markdown — a few sentences or a
tight bullet list. Call out the notable patterns, comparisons and outliers. Do
not restate the raw data and do not add a preamble.`

type analyzeRequest struct {
	Prompt string `json:"prompt"`
	Data   string `json:"data"`
}

type analyzeResponse struct {
	Analysis string `json:"analysis,omitempty"`
	Error    string `json:"error,omitempty"`
}

// HandleAnalyze is a one-shot LLM call — a prompt plus optional data in, a
// markdown analysis out. It is proxied at /api/v1/x/ai/analyze and powers the
// charts plugin's AI-analysis dashboard component. Unlike /chat it has no
// tools and no session: just a single completion.
func (s *chatService) HandleAnalyze(w http.ResponseWriter, r *http.Request) {
	var body analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, analyzeResponse{Error: "invalid request body"})
		return
	}
	if body.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, analyzeResponse{Error: "prompt is required"})
		return
	}

	user := body.Prompt
	if body.Data != "" {
		user += "\n\nDataset (JSON rows):\n" + truncateStr(body.Data, 6000)
	}

	// Stay under ratd's 120s HTTP WriteTimeout so the response always lands.
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()

	msg, err := s.ai.complete(ctx, []chatMessage{
		{Role: "system", Content: analyzeSystemPrompt},
		{Role: "user", Content: user},
	}, nil)
	if err != nil {
		errMsg := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = "the analysis took too long — try again or simplify the prompt"
		}
		writeJSON(w, http.StatusOK, analyzeResponse{Error: errMsg})
		return
	}
	writeJSON(w, http.StatusOK, analyzeResponse{Analysis: msg.Content})
}
