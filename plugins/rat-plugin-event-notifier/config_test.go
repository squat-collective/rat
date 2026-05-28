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
	def := notifierConfig{WebhookURL: "http://default", MaxEvents: 50}
	got := merge(def, notifierConfig{
		WebhookURL: "http://stored", MaxEvents: 10, ForwardOnlyFailures: true,
	})
	if got.WebhookURL != "http://stored" {
		t.Errorf("a set webhook should win, got %q", got.WebhookURL)
	}
	if got.MaxEvents != 10 {
		t.Errorf("a set max_events should win, got %d", got.MaxEvents)
	}
	if !got.ForwardOnlyFailures {
		t.Error("forward_only_failures should follow the stored value")
	}
}

func TestMergeKeepsDefaultsWhenStoredEmpty(t *testing.T) {
	def := notifierConfig{WebhookURL: "http://default", MaxEvents: 50}
	if got := merge(def, notifierConfig{}); got.WebhookURL != "http://default" || got.MaxEvents != 50 {
		t.Errorf("an empty stored config should keep the defaults, got %+v", got)
	}
}

func TestMergeClampsMaxEvents(t *testing.T) {
	got := merge(notifierConfig{MaxEvents: 50}, notifierConfig{MaxEvents: 99999})
	if got.MaxEvents != 1000 {
		t.Errorf("max_events should clamp to 1000, got %d", got.MaxEvents)
	}
}

func TestIsFailureEvent(t *testing.T) {
	if !isFailureEvent(event{Type: "quality_failed"}) {
		t.Error("quality_failed should be a failure")
	}
	if !isFailureEvent(event{Type: "run_completed", Payload: []byte(`{"status":"failed"}`)}) {
		t.Error("a run_completed with a failed status should be a failure")
	}
	if isFailureEvent(event{Type: "run_completed", Payload: []byte(`{"status":"success"}`)}) {
		t.Error("a successful run should not be a failure")
	}
	if isFailureEvent(event{Type: "run_completed", Payload: []byte(`{}`)}) {
		t.Error("a run with no status should not be treated as a failure")
	}
}
