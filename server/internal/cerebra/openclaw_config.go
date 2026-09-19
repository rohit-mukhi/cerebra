package cerebra

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OpenClawConfig represents the structure of OpenClaw's agent configuration
type OpenClawConfig struct {
	Agents struct {
		Defaults struct {
			Model struct {
				Primary   string   `json:"primary"`
				Fallbacks []string `json:"fallbacks"`
			} `json:"model"`
			Models map[string]interface{} `json:"models"`
		} `json:"defaults"`
		List []struct {
			ID        string `json:"id"`
			Workspace string `json:"workspace,omitempty"`
		} `json:"list"`
	} `json:"agents"`
}

// ParseOpenClawModels extracts all models from OpenClaw configuration JSON
// and returns them as DiscoveredModel instances with full metadata.
//
// For a typical OpenClaw config with primary + fallback models, this will
// return multiple models (e.g., groq/llama-3.3-70b-versatile, groq/llama-3.1-8b-instant,
// openrouter/free) instead of just agent IDs (nova, iris, rex, sage).
func ParseOpenClawModels(configJSON []byte) ([]DiscoveredModel, error) {
	var config OpenClawConfig
	if err := json.Unmarshal(configJSON, &config); err != nil {
		return nil, fmt.Errorf("failed to parse OpenClaw config: %w", err)
	}

	models := []DiscoveredModel{}

	// Add primary model
	if config.Agents.Defaults.Model.Primary != "" {
		primary := config.Agents.Defaults.Model.Primary
		models = append(models, DiscoveredModel{
			ID:           primary,
			Name:         formatModelName(primary),
			Provider:     extractProvider(primary),
			ModelType:    "primary",
			CostTier:     inferCostTier(primary),
			InferredTier: InferTierFromModelName(primary),
		})
	}

	// Add fallback models
	for i, fallback := range config.Agents.Defaults.Model.Fallbacks {
		if fallback == "" {
			continue
		}
		models = append(models, DiscoveredModel{
			ID:           fallback,
			Name:         formatModelName(fallback) + fmt.Sprintf(" (fallback %d)", i+1),
			Provider:     extractProvider(fallback),
			ModelType:    "fallback",
			CostTier:     inferCostTier(fallback),
			InferredTier: InferTierFromModelName(fallback),
		})
	}

	return models, nil
}

// extractProvider extracts the provider name from a model ID
// Examples:
//   - "groq/llama-3.3-70b-versatile" → "groq"
//   - "openrouter/free" → "openrouter"
//   - "anthropic/claude-3-5-sonnet" → "anthropic"
func extractProvider(modelID string) string {
	if modelID == "" {
		return "unknown"
	}
	parts := strings.SplitN(modelID, "/", 2)
	if len(parts) >= 1 && parts[0] != "" {
		return parts[0]
	}
	return "unknown"
}

// inferCostTier classifies a model into free, paid, or premium cost tiers
// based on naming patterns and known model characteristics.
//
// Classification rules:
// - "free" tier: Models with "free" in name, small models (8b, instant), or known free providers
// - "premium" tier: Large models (70b+), flagship models (opus, o1, o3)
// - "paid" tier: Everything else (balanced performance models)
func inferCostTier(modelID string) string {
	lower := strings.ToLower(modelID)

	// Explicit free models
	if strings.Contains(lower, "free") {
		return "free"
	}

	// Small/fast models are typically free or very cheap
	if strings.Contains(lower, "8b") ||
		strings.Contains(lower, "instant") ||
		strings.Contains(lower, "mini") ||
		strings.Contains(lower, "haiku") {
		return "free"
	}

	// Premium/flagship models
	if strings.Contains(lower, "70b") ||
		strings.Contains(lower, "opus") ||
		strings.Contains(lower, "o1") ||
		strings.Contains(lower, "o3") ||
		strings.Contains(lower, "ultra") ||
		strings.Contains(lower, "pro") && !strings.Contains(lower, "pro-") {
		return "premium"
	}

	// Default to paid tier for balanced models
	return "paid"
}

// formatModelName creates a human-readable name from a model ID
// Example: "groq/llama-3.3-70b-versatile" → "llama-3.3-70b-versatile (groq)"
func formatModelName(modelID string) string {
	parts := strings.SplitN(modelID, "/", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("%s (%s)", parts[1], parts[0])
	}
	return modelID
}

// deduplicateModels removes duplicate models, preferring detailed entries
// over simple agent IDs. If multiple entries exist for the same model ID,
// we keep the one with the most metadata (provider, cost tier, etc.).
func deduplicateModels(models []DiscoveredModel) []DiscoveredModel {
	seen := make(map[string]DiscoveredModel)

	for _, model := range models {
		existing, exists := seen[model.ID]

		if !exists {
			// First time seeing this model
			seen[model.ID] = model
			continue
		}

		// Model already exists, keep the one with more metadata
		// Prefer models with provider info over those without
		if model.Provider != "" && existing.Provider == "" {
			seen[model.ID] = model
			continue
		}

		// Prefer models with cost tier info
		if model.CostTier != "" && existing.CostTier == "" {
			seen[model.ID] = model
			continue
		}

		// Prefer primary/fallback over agent
		if (model.ModelType == "primary" || model.ModelType == "fallback") &&
			existing.ModelType == "agent" {
			seen[model.ID] = model
			continue
		}
	}

	// Convert map back to slice
	result := make([]DiscoveredModel, 0, len(seen))
	for _, model := range seen {
		result = append(result, model)
	}

	return result
}

// ReadOpenClawConfig reads and parses OpenClaw configuration from file system
// Default location: ~/.openclaw/openclaw.json
//
// This function is used during model discovery to extract actual model configurations
// (primary + fallbacks) instead of just discovering agent IDs.
func ReadOpenClawConfig(configPath string) ([]DiscoveredModel, error) {
	// Expand home directory if path starts with ~
	if strings.HasPrefix(configPath, "~/") {
		// In Go, we need to use os.UserHomeDir() or environment variable
		// For now, let's handle it in the caller
		return nil, fmt.Errorf("path expansion should be handled by caller: %s", configPath)
	}

	// Note: This function is designed to be called from the daemon side
	// where file system access is available. The server side will need
	// to request this information via RPC/HTTP from the daemon.
	return nil, fmt.Errorf("ReadOpenClawConfig should be called from daemon context")
}
