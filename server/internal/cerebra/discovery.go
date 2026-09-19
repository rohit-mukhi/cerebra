package cerebra

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DiscoveredModel represents a model discovered from a runtime
type DiscoveredModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	InferredTier Tier   `json:"tier"`

	// Provider is the model provider (e.g., "groq", "openrouter", "anthropic")
	Provider string `json:"provider,omitempty"`

	// ModelType indicates if this is a "primary", "fallback", or "agent" model
	ModelType string `json:"model_type,omitempty"`

	// SourceAgent is the agent ID this model came from (for OpenClaw agents like "nova", "iris")
	SourceAgent string `json:"source_agent,omitempty"`

	// CostTier classifies the model by cost: "free", "paid", "premium"
	CostTier string `json:"cost_tier,omitempty"`
}

// ModelDiscovery handles automatic model discovery and tier assignment
type ModelDiscovery struct {
	queries *db.Queries
}

// NewModelDiscovery creates a new model discovery handler
func NewModelDiscovery(queries *db.Queries) *ModelDiscovery {
	return &ModelDiscovery{
		queries: queries,
	}
}

// DiscoverAndAssignTiers discovers available models from a runtime and auto-populates tier_model_map
//
// Process:
// 1. Query runtime for available models (via model list request)
// 2. Classify each model into a tier using InferTierFromModelName
// 3. Build tier_model_map by selecting best model per tier
// 4. Cache discovered models in agent_runtime.discovered_models
// 5. Update agent_runtime.tier_model_map with auto-assigned tiers
//
// Returns the number of models discovered and any error
func (md *ModelDiscovery) DiscoverAndAssignTiers(ctx context.Context, runtimeID uuid.UUID, models []DiscoveredModel) (int, error) {
	if len(models) == 0 {
		return 0, fmt.Errorf("no models provided for discovery")
	}

	// Classify models into tiers
	for i := range models {
		models[i].InferredTier = InferTierFromModelName(models[i].ID)
	}

	// Build tier_model_map by selecting the best model for each tier
	tierMap := md.buildTierMap(models)

	// Marshal to JSON for storage
	tierMapJSON, err := json.Marshal(tierMap)
	if err != nil {
		return 0, fmt.Errorf("marshal tier_model_map: %w", err)
	}

	modelsJSON, err := json.Marshal(models)
	if err != nil {
		return 0, fmt.Errorf("marshal discovered_models: %w", err)
	}

	// Update agent_runtime with discovered models and tier map
	err = md.queries.UpdateRuntimeDiscoveredModels(ctx, db.UpdateRuntimeDiscoveredModelsParams{
		ID:               pgtype.UUID{Bytes: runtimeID, Valid: true},
		DiscoveredModels: modelsJSON,
		TierModelMap:     tierMapJSON,
	})
	if err != nil {
		return 0, fmt.Errorf("update agent_runtime: %w", err)
	}

	slog.Info("cerebra: model discovery complete",
		"runtime_id", runtimeID.String(),
		"models_discovered", len(models),
		"tier_map", tierMap,
	)

	return len(models), nil
}

// buildTierMap selects the best model for each tier from discovered models
//
// Strategy:
// - Group models by tier AND cost (free, paid, premium)
// - For simple tier: prefer free models (cost optimization)
// - For standard tier: prefer paid models (balanced performance)
// - For heavy tier: prefer premium models (maximum capability)
// - Use preference-based selection within each cost category
func (md *ModelDiscovery) buildTierMap(models []DiscoveredModel) map[string]string {
	tierMap := make(map[string]string)

	// Group models by tier AND cost
	type TierModels struct {
		Free    []DiscoveredModel
		Paid    []DiscoveredModel
		Premium []DiscoveredModel
	}
	modelsByTier := make(map[Tier]TierModels)

	for _, model := range models {
		tm := modelsByTier[model.InferredTier]
		switch model.CostTier {
		case "free":
			tm.Free = append(tm.Free, model)
		case "premium":
			tm.Premium = append(tm.Premium, model)
		default: // "paid" or empty (treat as paid)
			tm.Paid = append(tm.Paid, model)
		}
		modelsByTier[model.InferredTier] = tm
	}

	// Select best model for each tier with cost-aware preferences
	for tier, models := range modelsByTier {
		var selected DiscoveredModel

		switch tier {
		case TierSimple:
			// Simple tier: prefer free models for cost optimization
			// Fallback: paid > premium (use cheapest available)
			if len(models.Free) > 0 {
				selected = md.selectBestModel(models.Free, tier)
			} else if len(models.Paid) > 0 {
				selected = md.selectBestModel(models.Paid, tier)
			} else if len(models.Premium) > 0 {
				selected = md.selectBestModel(models.Premium, tier)
			}

		case TierStandard:
			// Standard tier: prefer paid models for balanced performance
			// Fallback: premium > free (prioritize quality over cost)
			if len(models.Paid) > 0 {
				selected = md.selectBestModel(models.Paid, tier)
			} else if len(models.Premium) > 0 {
				selected = md.selectBestModel(models.Premium, tier)
			} else if len(models.Free) > 0 {
				selected = md.selectBestModel(models.Free, tier)
			}

		case TierHeavy:
			// Heavy tier: prefer premium models for maximum capability
			// Fallback: paid > free (prioritize performance)
			if len(models.Premium) > 0 {
				selected = md.selectBestModel(models.Premium, tier)
			} else if len(models.Paid) > 0 {
				selected = md.selectBestModel(models.Paid, tier)
			} else if len(models.Free) > 0 {
				selected = md.selectBestModel(models.Free, tier)
			}
		}

		if selected.ID != "" {
			tierMap[string(tier)] = selected.ID
		}
	}

	return tierMap
}

// selectBestModel chooses the best model from a list based on preference patterns
func (md *ModelDiscovery) selectBestModel(models []DiscoveredModel, tier Tier) DiscoveredModel {
	if len(models) == 1 {
		return models[0]
	}

	// Define preference patterns for each tier
	var preferencePatterns []string

	switch tier {
	case TierHeavy:
		// Prefer: opus > o1 > gpt-4-turbo > sonnet-3.5 > others
		preferencePatterns = []string{
			"opus",
			"o1-preview",
			"o3",
			"gpt-4-turbo",
			"claude-3.5-sonnet",
			"gemini-ultra",
			"gemini-pro",
		}
	case TierStandard:
		// Prefer: gpt-4o > sonnet > gemini-flash > command-r > others
		preferencePatterns = []string{
			"gpt-4o",
			"claude-3-sonnet",
			"claude-3.5-sonnet", // If not classified as heavy
			"gemini-1.5-flash",
			"command-r",
		}
	case TierSimple:
		// Prefer: gpt-4o-mini > haiku > flash > 3.5-turbo > others
		preferencePatterns = []string{
			"gpt-4o-mini",
			"haiku",
			"gemini-flash",
			"gpt-3.5-turbo",
		}
	}

	// Try to find a model matching preference patterns
	for _, pattern := range preferencePatterns {
		for _, model := range models {
			if contains(model.ID, pattern) {
				return model
			}
		}
	}

	// Fallback: return first model
	return models[0]
}

// RefreshDiscoveredModels re-discovers models for a runtime (e.g., when runtime capabilities change)
func (md *ModelDiscovery) RefreshDiscoveredModels(ctx context.Context, runtimeID uuid.UUID, models []DiscoveredModel) error {
	_, err := md.DiscoverAndAssignTiers(ctx, runtimeID, models)
	return err
}

// GetDiscoveredModels retrieves cached discovered models from the database
func (md *ModelDiscovery) GetDiscoveredModels(ctx context.Context, runtimeID uuid.UUID) ([]DiscoveredModel, error) {
	modelsJSON, err := md.queries.GetRuntimeDiscoveredModels(ctx, pgtype.UUID{Bytes: runtimeID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("query discovered_models: %w", err)
	}

	if len(modelsJSON) == 0 {
		return nil, nil // Not yet discovered
	}

	var models []DiscoveredModel
	if err := json.Unmarshal(modelsJSON, &models); err != nil {
		return nil, fmt.Errorf("unmarshal discovered_models: %w", err)
	}

	return models, nil
}

// ScheduleRediscovery schedules a background rediscovery for runtimes that need it
//
// Triggers rediscovery when:
// - discovered_models is NULL (never discovered)
// - discovered_models is older than 7 days (stale)
// - Runtime has been offline and just came back online
func (md *ModelDiscovery) ScheduleRediscovery(ctx context.Context, runtimeID uuid.UUID) error {
	status, err := md.queries.CheckRuntimeDiscoveryStatus(ctx, pgtype.UUID{Bytes: runtimeID, Valid: true})
	if err != nil {
		return fmt.Errorf("check discovery status: %w", err)
	}

	needsDiscovery, _ := status.NeedsDiscovery.(bool)
	isStale := status.IsStale

	if needsDiscovery || isStale {
		slog.Info("cerebra: runtime needs model rediscovery",
			"runtime_id", runtimeID.String(),
			"reason", map[bool]string{true: "never_discovered", false: "stale"}[needsDiscovery],
		)
		// Trigger async discovery (implementation depends on task queue system)
		// For now, just log the need
		return fmt.Errorf("rediscovery needed but not yet implemented")
	}

	return nil
}

// ModelListResult represents the response from a runtime's model list endpoint
type ModelListResult struct {
	Models    []ModelInfo `json:"models"`
	Timestamp time.Time   `json:"timestamp"`
}

// ModelInfo represents a single model from the runtime
type ModelInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// ParseModelListResponse converts a runtime's model list response into DiscoveredModels
func ParseModelListResponse(response ModelListResult) []DiscoveredModel {
	discovered := make([]DiscoveredModel, 0, len(response.Models))

	for _, model := range response.Models {
		discovered = append(discovered, DiscoveredModel{
			ID:           model.ID,
			Name:         model.Name,
			InferredTier: InferTierFromModelName(model.ID),
		})
	}

	return discovered
}
