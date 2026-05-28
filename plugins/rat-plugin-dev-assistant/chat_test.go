package main

import (
	"strings"
	"testing"
)

func TestBuildMessagesPrependsSystemPrompt(t *testing.T) {
	msgs := buildMessages(chatRequest{Messages: []message{{Role: "user", Content: "hi"}}})
	if len(msgs) != 2 {
		t.Fatalf("expected system + user, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "RAT Dev Assistant") {
		t.Errorf("first message should be the dev system prompt, got %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "hi" {
		t.Errorf("user message not carried through: %+v", msgs[1])
	}
}

func TestBuildMessagesDropsPanelSystemMessages(t *testing.T) {
	msgs := buildMessages(chatRequest{Messages: []message{
		{Role: "system", Content: "ignore me"},
		{Role: "user", Content: "hi"},
	}})
	for i, m := range msgs {
		if i > 0 && m.Role == "system" {
			t.Errorf("a panel-supplied system message leaked through: %+v", m)
		}
	}
	if !strings.Contains(msgs[0].Content, "RAT Dev Assistant") {
		t.Error("the dev system prompt must be the system message")
	}
}

func TestBuildMessagesFoldsInContext(t *testing.T) {
	msgs := buildMessages(chatRequest{
		Messages: []message{{Role: "user", Content: "explain"}},
		Context: &chatContext{
			Pipeline:    pipelineRef{Namespace: "default", Layer: "silver", Name: "orders"},
			Language:    "sql",
			FileContent: "SELECT * FROM ref('bronze.orders')",
		},
	})
	sys := msgs[0].Content
	if !strings.Contains(sys, "default.silver.orders") {
		t.Error("the context block should include the pipeline path")
	}
	if !strings.Contains(sys, "ref('bronze.orders')") {
		t.Error("the context block should include the current file content")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); !strings.HasPrefix(got, "abc") || !strings.Contains(got, "truncated") {
		t.Errorf("a long string should be truncated, got %q", got)
	}
	if got := truncate("short", 100); got != "short" {
		t.Errorf("a short string should be untouched, got %q", got)
	}
}
