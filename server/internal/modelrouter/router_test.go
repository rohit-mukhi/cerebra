package modelrouter

import "testing"

func TestSelectModel(t *testing.T) {
	model, reason := SelectModel("claude", "Refactor the authentication module", "claude-sonnet-5")
	t.Logf("model=%s reason=%s", model, reason)
	if model != "claude-opus-4-8" {
		t.Errorf("expected heavy tier model, got %s", model)
	}

	model, reason = SelectModel("claude", "Fix typo in readme", "claude-sonnet-5")
	t.Logf("model=%s reason=%s", model, reason)
	if model != "claude-haiku-4-5" {
		t.Errorf("expected simple tier model, got %s", model)
	}

	model, reason = SelectModel("unknown-provider", "Some task", "fallback-model")
	t.Logf("model=%s reason=%s", model, reason)
	if model != "fallback-model" {
		t.Errorf("expected fallback, got %s", model)
	}
}
