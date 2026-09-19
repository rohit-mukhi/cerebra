package cerebra

import (
	"os"
	"testing"
)

// TestParseOpenClawModels_RealConfig tests parsing with an actual OpenClaw config
// This test will be skipped if ~/.openclaw/openclaw.json doesn't exist
func TestParseOpenClawModels_RealConfig(t *testing.T) {
	configPath := os.ExpandEnv("$HOME/.openclaw/openclaw.json")
	
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Skipf("Skipping test: OpenClaw config not found at %s", configPath)
		return
	}
	
	models, err := ParseOpenClawModels(configData)
	if err != nil {
		t.Fatalf("Failed to parse OpenClaw config: %v", err)
	}
	
	if len(models) == 0 {
		t.Error("Expected to discover at least one model, got zero")
	}
	
	t.Logf("Discovered %d models from OpenClaw config:", len(models))
	for _, model := range models {
		t.Logf("  - %s (%s) | Provider: %s | Type: %s | Cost: %s | Tier: %s",
			model.ID, model.Name, model.Provider, model.ModelType, model.CostTier, model.InferredTier)
	}
	
	// Verify we have at least primary and some fallbacks
	hasPrimary := false
	hasFallback := false
	
	for _, model := range models {
		if model.ModelType == "primary" {
			hasPrimary = true
		}
		if model.ModelType == "fallback" {
			hasFallback = true
		}
	}
	
	if !hasPrimary {
		t.Error("Expected to find at least one primary model")
	}
	if !hasFallback {
		t.Log("Warning: No fallback models found (this might be normal if not configured)")
	}
}

// TestParseOpenClawModels_SampleConfig tests parsing with a known sample config
func TestParseOpenClawModels_SampleConfig(t *testing.T) {
	sampleConfig := []byte(`{
		"agents": {
			"defaults": {
				"model": {
					"primary": "groq/llama-3.3-70b-versatile",
					"fallbacks": [
						"groq/llama-3.1-8b-instant",
						"openrouter/free"
					]
				}
			}
		}
	}`)
	
	models, err := ParseOpenClawModels(sampleConfig)
	if err != nil {
		t.Fatalf("Failed to parse sample config: %v", err)
	}
	
	if len(models) != 3 {
		t.Errorf("Expected 3 models (1 primary + 2 fallbacks), got %d", len(models))
	}
	
	// Verify primary model
	if models[0].ID != "groq/llama-3.3-70b-versatile" {
		t.Errorf("Expected primary model to be groq/llama-3.3-70b-versatile, got %s", models[0].ID)
	}
	if models[0].ModelType != "primary" {
		t.Errorf("Expected first model to be primary, got %s", models[0].ModelType)
	}
	if models[0].Provider != "groq" {
		t.Errorf("Expected provider to be groq, got %s", models[0].Provider)
	}
	if models[0].CostTier != "premium" {
		t.Errorf("Expected 70b model to be premium tier, got %s", models[0].CostTier)
	}
	
	// Verify first fallback
	if models[1].ID != "groq/llama-3.1-8b-instant" {
		t.Errorf("Expected first fallback to be groq/llama-3.1-8b-instant, got %s", models[1].ID)
	}
	if models[1].ModelType != "fallback" {
		t.Errorf("Expected model to be fallback type, got %s", models[1].ModelType)
	}
	if models[1].CostTier != "free" {
		t.Errorf("Expected 8b-instant to be free tier, got %s", models[1].CostTier)
	}
	
	// Verify second fallback (openrouter/free)
	if models[2].ID != "openrouter/free" {
		t.Errorf("Expected second fallback to be openrouter/free, got %s", models[2].ID)
	}
	if models[2].Provider != "openrouter" {
		t.Errorf("Expected provider to be openrouter, got %s", models[2].Provider)
	}
	if models[2].CostTier != "free" {
		t.Errorf("Expected openrouter/free to be free tier, got %s", models[2].CostTier)
	}
}

// TestExtractProvider tests the provider extraction logic
func TestExtractProvider(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"groq/llama-3.3-70b-versatile", "groq"},
		{"openrouter/free", "openrouter"},
		{"anthropic/claude-3-5-sonnet", "anthropic"},
		{"just-a-name", "just-a-name"},
		{"", "unknown"},
	}
	
	for _, tt := range tests {
		got := extractProvider(tt.modelID)
		if got != tt.expected {
			t.Errorf("extractProvider(%q) = %q, want %q", tt.modelID, got, tt.expected)
		}
	}
}

// TestInferCostTier tests cost tier classification
func TestInferCostTier(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"openrouter/free", "free"},
		{"groq/llama-3.1-8b-instant", "free"},
		{"gpt-4o-mini", "free"},
		{"claude-3-haiku", "free"},
		{"groq/llama-3.3-70b-versatile", "premium"},
		{"claude-opus-4-5", "premium"},
		{"gpt-o1-preview", "premium"},
		{"gpt-4o", "paid"},
		{"claude-3-sonnet", "paid"},
	}
	
	for _, tt := range tests {
		got := inferCostTier(tt.modelID)
		if got != tt.expected {
			t.Errorf("inferCostTier(%q) = %q, want %q", tt.modelID, got, tt.expected)
		}
	}
}

// TestDeduplicateModels tests deduplication logic
func TestDeduplicateModels(t *testing.T) {
	models := []DiscoveredModel{
		{ID: "model1", Provider: "groq", CostTier: "free"},
		{ID: "model1", Provider: "", CostTier: ""}, // Duplicate with less info
		{ID: "model2", Provider: "openrouter"},
		{ID: "model2", Provider: "openrouter", CostTier: "free"}, // Duplicate with more info
	}
	
	result := deduplicateModels(models)
	
	if len(result) != 2 {
		t.Errorf("Expected 2 unique models after deduplication, got %d", len(result))
	}
	
	// Verify model1 kept the entry with provider info
	var model1 *DiscoveredModel
	for i := range result {
		if result[i].ID == "model1" {
			model1 = &result[i]
			break
		}
	}
	if model1 == nil {
		t.Fatal("model1 not found after deduplication")
	}
	if model1.Provider != "groq" {
		t.Errorf("Expected model1 to keep provider=groq, got %s", model1.Provider)
	}
	
	// Verify model2 kept the entry with cost tier info
	var model2 *DiscoveredModel
	for i := range result {
		if result[i].ID == "model2" {
			model2 = &result[i]
			break
		}
	}
	if model2 == nil {
		t.Fatal("model2 not found after deduplication")
	}
	if model2.CostTier != "free" {
		t.Errorf("Expected model2 to keep cost_tier=free, got %s", model2.CostTier)
	}
}
