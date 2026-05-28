package main

import (
	"strings"
	"testing"
)

func TestBuildUserPromptIncludesColumnsAndSample(t *testing.T) {
	prompt := buildUserPrompt(suggestRequest{
		Table:      tableRef{Namespace: "default", Layer: "bronze", Name: "orders"},
		Columns:    []columnRef{{Name: "id", Type: "BIGINT"}, {Name: "amount", Type: "DOUBLE"}},
		DataSample: "id=1 amount=99",
	})
	for _, want := range []string{
		"default.bronze.orders",
		"id (BIGINT)",
		"amount (DOUBLE)",
		"id=1 amount=99",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q in:\n%s", want, prompt)
		}
	}
}

func TestParseSuggestionStrictJSON(t *testing.T) {
	got, err := parseSuggestion(
		`{"description":"orders","column_descriptions":{"id":"primary key"}}`,
		[]columnRef{{Name: "id"}},
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Description != "orders" {
		t.Errorf("description = %q", got.Description)
	}
	if got.ColumnDescriptions["id"] != "primary key" {
		t.Errorf("column id description = %q", got.ColumnDescriptions["id"])
	}
}

func TestParseSuggestionStripsCodeFence(t *testing.T) {
	in := "```json\n{\"description\":\"x\",\"column_descriptions\":{\"a\":\"b\"}}\n```"
	got, err := parseSuggestion(in, []columnRef{{Name: "a"}})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Description != "x" || got.ColumnDescriptions["a"] != "b" {
		t.Errorf("unexpected parse: %+v", got)
	}
}

func TestParseSuggestionHandlesPreamble(t *testing.T) {
	in := "Sure, here you go:\n{\"description\":\"x\",\"column_descriptions\":{\"a\":\"b\"}}"
	got, err := parseSuggestion(in, []columnRef{{Name: "a"}})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Description != "x" {
		t.Errorf("description = %q", got.Description)
	}
}

func TestParseSuggestionFiltersUnknownColumns(t *testing.T) {
	got, err := parseSuggestion(
		`{"description":"x","column_descriptions":{"a":"keep","b":"keep","made_up":"drop"}}`,
		[]columnRef{{Name: "a"}, {Name: "b"}},
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := got.ColumnDescriptions["made_up"]; ok {
		t.Error("unknown column should be filtered out")
	}
	if got.ColumnDescriptions["a"] != "keep" || got.ColumnDescriptions["b"] != "keep" {
		t.Errorf("known columns missing: %+v", got.ColumnDescriptions)
	}
}

func TestParseSuggestionRejectsNonJSON(t *testing.T) {
	if _, err := parseSuggestion("totally not json", []columnRef{{Name: "x"}}); err == nil {
		t.Error("expected an error for non-JSON output")
	}
}
