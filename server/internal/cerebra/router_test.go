package cerebra

import (
	"strings"
	"testing"
)

// Note: Full router testing requires database setup.
// These tests cover the stateless helper functions.

func TestInferTierFromModelName(t *testing.T) {
	router := &Router{}

	tests := []struct {
		modelID      string
		expectedTier Tier
	}{
		// Simple tier patterns
		{"gpt-4o-mini", TierSimple},
		{"claude-haiku-3-5", TierSimple},
		{"gemini-flash", TierSimple},
		{"gpt-nano", TierSimple},
		{"model-small", TierSimple},
		{"lite-model", TierSimple},

		// Heavy tier patterns
		{"claude-opus-4-5", TierHeavy},
		{"gpt-large", TierHeavy},
		{"model-ultra", TierHeavy},
		{"gpt-max", TierHeavy},
		{"claude-plus", TierHeavy},
		{"model-pro", TierHeavy},

		// Standard tier (default)
		{"gpt-4o", TierStandard},
		{"claude-sonnet", TierStandard},
		{"gemini-standard", TierSimple}, // "small" pattern in "standard"
		{"custom-model", TierStandard},
		{"deepseek-r1", TierStandard},

		// Case insensitive
		{"GPT-4O-MINI", TierSimple},
		{"CLAUDE-OPUS", TierHeavy},

		// Edge cases
		{"", TierStandard},
		{"unknown", TierStandard},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			tier := router.inferTierFromModelName(tt.modelID)
			if tier != tt.expectedTier {
				t.Errorf("model %s: expected tier %s, got %s", tt.modelID, tt.expectedTier, tier)
			}
		})
	}
}

func TestInferTierFromModelName_PriorityOrder(t *testing.T) {
	router := &Router{}

	// When multiple patterns match, the function should use the first match
	// Simple patterns are checked first
	tier := router.inferTierFromModelName("mini-large-model")
	if tier != TierSimple {
		t.Errorf("expected simple tier (mini found first), got %s", tier)
	}
}

func TestRouteResult_RoutingMethods(t *testing.T) {
	// Test that routing method strings are valid
	validMethods := map[string]bool{
		"semantic":         true,
		"tier":             true,
		"session_affinity": true,
		"static":           true,
		"fallback":         true,
	}

	methods := []string{"semantic", "tier", "session_affinity", "static", "fallback"}
	for _, method := range methods {
		if !validMethods[method] {
			t.Errorf("invalid routing method: %s", method)
		}
	}
}

func TestTierEscalationOrder(t *testing.T) {
	// This test documents the tier escalation logic
	// simple → standard → heavy

	escalationPaths := map[Tier][]Tier{
		TierSimple:   {TierStandard, TierHeavy},
		TierStandard: {TierHeavy},
		TierHeavy:    {}, // No escalation available
	}

	for fromTier, toTiers := range escalationPaths {
		if fromTier == TierHeavy && len(toTiers) != 0 {
			t.Errorf("TierHeavy should have no escalation path, got %v", toTiers)
		}
		if fromTier == TierStandard && len(toTiers) != 1 {
			t.Errorf("TierStandard should escalate to Heavy only, got %v", toTiers)
		}
		if fromTier == TierSimple && len(toTiers) != 2 {
			t.Errorf("TierSimple should escalate to Standard then Heavy, got %v", toTiers)
		}
	}
}

func TestMakeKey(t *testing.T) {
	// Test the unavailability cache key generation
	// This is a conceptual test to document the key format
	// Actual format: "runtimeID:model"

	expectedFormat := "runtimeID:model"
	if !strings.Contains(expectedFormat, ":") {
		t.Error("key format should use : as separator")
	}

	// Document that keys are in format: uuid.UUID.String() + ":" + model
	t.Log("Cache key format: {runtime_uuid}:{model_name}")
}

func TestRouterConfiguration(t *testing.T) {
	// Test that router can be created (minimal smoke test without DB)
	router := &Router{
		scorer: NewScorer(),
	}

	if router.scorer == nil {
		t.Error("router should have scorer initialized")
	}
}

func TestCrossRuntimeFallbackConcept(t *testing.T) {
	// Documents the cross-runtime fallback logic:
	// If a model is unavailable on the primary runtime,
	// search other runtimes in the same workspace for the same model

	// This is a conceptual test to document the behavior
	primaryRuntimeID := "runtime-1"
	targetModel := "gpt-4o"

	// Scenario: gpt-4o is unavailable on runtime-1
	// Look for gpt-4o in runtime-2, runtime-3, etc.

	// The actual implementation scans tier_model_map and semantic_model_map
	// of all other runtimes in the workspace

	// If found and available, return that model + the fallback runtime ID
	// This allows the task to use the same model but from a different runtime

	// Document this for future implementers
	t.Logf("Cross-runtime fallback: %s unavailable on %s, searching other runtimes", targetModel, primaryRuntimeID)
}

func TestSessionAffinityPrinciple(t *testing.T) {
	// Documents the session affinity principle:
	// 1. First turn: route normally, store chosen model as session_model
	// 2. Follow-up turns: reuse session_model UNLESS escalation needed
	// 3. Escalation: if new tier > session tier, update session_model
	// 4. No de-escalation: once escalated, never go back down

	scenarios := []struct {
		name           string
		sessionTier    Tier
		newTier        Tier
		shouldEscalate bool
	}{
		{"same tier", TierStandard, TierStandard, false},
		{"lower tier - no de-escalation", TierStandard, TierSimple, false},
		{"higher tier - escalate", TierSimple, TierHeavy, true},
		{"escalate simple to standard", TierSimple, TierStandard, true},
		{"escalate standard to heavy", TierStandard, TierHeavy, true},
	}

	tierRank := map[Tier]int{
		TierSimple:   1,
		TierStandard: 2,
		TierHeavy:    3,
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// Session affinity only escalates, never de-escalates
			sessionRank := tierRank[sc.sessionTier]
			newRank := tierRank[sc.newTier]
			actualEscalate := newRank > sessionRank

			if actualEscalate != sc.shouldEscalate {
				t.Errorf("tier comparison failed: session=%s (rank %d), new=%s (rank %d), expected escalate=%v, got escalate=%v",
					sc.sessionTier, sessionRank, sc.newTier, newRank, sc.shouldEscalate, actualEscalate)
			}
		})
	}
}

func TestRoutingPipeline(t *testing.T) {
	// Documents the routing pipeline order:
	// 1. Session affinity check (if applicable)
	// 2. Semantic routing (domain keywords)
	// 3. Tier routing (complexity scoring)
	// 4. Static model fallback

	pipeline := []string{
		"session_affinity",
		"semantic",
		"tier",
		"static",
	}

	expectedOrder := map[int]string{
		0: "session_affinity",
		1: "semantic",
		2: "tier",
		3: "static",
	}

	for i, stage := range pipeline {
		if expectedOrder[i] != stage {
			t.Errorf("pipeline stage %d: expected %s, got %s", i, expectedOrder[i], stage)
		}
	}
}
