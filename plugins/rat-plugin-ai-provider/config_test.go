package main

import (
	"encoding/json"
	"testing"
)

func TestConfigSchemaIsValidJSON(t *testing.T) {
	if !json.Valid([]byte(configSchemaJSON)) {
		t.Fatal("configSchemaJSON must be valid JSON for the portal to render it")
	}
}

func TestMergeOverlaysStoredOverDefaults(t *testing.T) {
	def := aiConfig{BaseURL: "http://default/v1", APIKey: "default-key", Model: "default-model"}
	stored := aiConfig{Model: "stored-model"} // only the model is set

	got := merge(def, stored)
	if got.Model != "stored-model" {
		t.Errorf("a set stored field should win: model = %q", got.Model)
	}
	if got.BaseURL != "http://default/v1" {
		t.Errorf("an unset stored field should fall back to the default: base_url = %q", got.BaseURL)
	}
	if got.APIKey != "default-key" {
		t.Errorf("api_key should fall back to the default, got %q", got.APIKey)
	}
}

func TestMergeIgnoresEmptyStored(t *testing.T) {
	def := aiConfig{BaseURL: "http://default/v1", Model: "m"}
	if got := merge(def, aiConfig{}); got != def {
		t.Errorf("an empty stored config should leave the defaults unchanged, got %+v", got)
	}
}

func TestMergeTrimsWhitespace(t *testing.T) {
	def := aiConfig{Model: "m"}
	got := merge(def, aiConfig{Model: "  spaced  "})
	if got.Model != "spaced" {
		t.Errorf("stored values should be trimmed, got %q", got.Model)
	}
}
