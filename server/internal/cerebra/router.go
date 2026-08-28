package cerebra

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Router orchestrates the two-pass routing: semantic → tier → fallback
type Router struct {
	db                *sql.DB
	scorer            *Scorer
	unavailability    *UnavailabilityCache
	sessionAffinity   *SessionAffinity
}

// NewRouter creates a new Cerebra router
func NewRouter(db *sql.DB) *Router {
	return &Router{
		db:                db,
		scorer:            NewScorer(),
		unavailability:    NewUnavailabilityCache(db),
		sessionAffinity:   NewSessionAffinity(db),
	}
}

// RouteParams contains all parameters needed for routing
type RouteParams struct {
	Prompt       string
	RuntimeID    uuid.UUID
	WorkspaceID  uuid.UUID
	StaticModel  string // Fallback model from agent config
	IssueID      *uuid.UUID
	ChatSessionID *uuid.UUID
}

// RouteResult contains the routing decision
type RouteResult struct {
	Model          string
	RuntimeID      uuid.UUID
	RoutingMethod  string // "semantic", "tier", "session_affinity", "static", "fallback"
	Tier           Tier
	SemanticDomain Domain
	Confidence     float64
}

// Route performs the full two-pass routing with session affinity and fallback
func (r *Router) Route(ctx context.Context, params RouteParams) (*RouteResult, error) {
	result := &RouteResult{
		RuntimeID: params.RuntimeID,
	}

	// Check session affinity first - if a session already has a model, reuse it
	// unless the new prompt requires escalation
	if params.IssueID != nil || params.ChatSessionID != nil {
		sessionModel, escalate, err := r.sessionAffinity.CheckAffinity(ctx, params)
		if err != nil {
			// Log error but don't fail routing
			fmt.Printf("cerebra: session affinity check failed: %v\n", err)
		} else if sessionModel != "" && !escalate {
			result.Model = sessionModel
			result.RoutingMethod = "session_affinity"
			return result, nil
		}
	}

	// Fetch runtime's tier and semantic maps
	tierMap, semanticMap, err := r.getRuntimeModelMaps(ctx, params.RuntimeID)
	if err != nil {
		// Can't route without maps, fall back to static
		result.Model = params.StaticModel
		result.RoutingMethod = "static"
		return result, nil
	}

	// Pass 1: Semantic routing
	if len(semanticMap) > 0 {
		semanticRouter := NewSemanticRouter(semanticMap)
		matchResult := semanticRouter.Match(params.Prompt)
		if matchResult != nil && matchResult.Model != "" {
			// Check if model is available
			if r.unavailability.IsAvailable(ctx, params.RuntimeID, matchResult.Model) {
				result.Model = matchResult.Model
				result.RoutingMethod = "semantic"
				result.SemanticDomain = matchResult.Domain
				result.Confidence = matchResult.Confidence

				// Update session affinity if applicable
				r.updateSessionModel(ctx, params, matchResult.Model)

				return result, nil
			}
			// Model unavailable, try cross-runtime fallback
			fallbackModel, fallbackRuntimeID := r.findCrossRuntimeFallback(ctx, params.WorkspaceID, matchResult.Model, params.RuntimeID)
			if fallbackModel != "" {
				result.Model = fallbackModel
				result.RuntimeID = fallbackRuntimeID
				result.RoutingMethod = "semantic"
				result.SemanticDomain = matchResult.Domain
				result.Confidence = matchResult.Confidence
				return result, nil
			}
		}
	}

	// Pass 2: Tier routing
	tier := r.scorer.Score(params.Prompt)
	result.Tier = tier

	if len(tierMap) > 0 {
		tierModel, exists := tierMap[string(tier)]
		if exists && tierModel != "" {
			// Check if model is available
			if r.unavailability.IsAvailable(ctx, params.RuntimeID, tierModel) {
				result.Model = tierModel
				result.RoutingMethod = "tier"

				// Update session affinity if applicable
				r.updateSessionModel(ctx, params, tierModel)

				return result, nil
			}
			// Model unavailable, try cross-runtime fallback
			fallbackModel, fallbackRuntimeID := r.findCrossRuntimeFallback(ctx, params.WorkspaceID, tierModel, params.RuntimeID)
			if fallbackModel != "" {
				result.Model = fallbackModel
				result.RuntimeID = fallbackRuntimeID
				result.RoutingMethod = "tier"
				return result, nil
			}
		}

		// Try tier escalation if primary tier is unavailable
		escalatedModel := r.escalateTier(ctx, params.RuntimeID, tier, tierMap)
		if escalatedModel != "" {
			result.Model = escalatedModel
			result.RoutingMethod = "tier"
			return result, nil
		}
	}

	// Pass 3: Name-pattern inference fallback (best effort)
	if params.StaticModel != "" {
		inferredTier := r.inferTierFromModelName(params.StaticModel)
		result.Tier = inferredTier
	}

	// Pass 4: Static model fallback
	result.Model = params.StaticModel
	result.RoutingMethod = "static"
	return result, nil
}

// getRuntimeModelMaps fetches tier_model_map and semantic_model_map from DB
func (r *Router) getRuntimeModelMaps(ctx context.Context, runtimeID uuid.UUID) (map[string]string, map[string]string, error) {
	query := `SELECT tier_model_map, semantic_model_map FROM agent_runtime WHERE id = $1`
	
	var tierMapJSON, semanticMapJSON sql.NullString
	err := r.db.QueryRowContext(ctx, query, runtimeID).Scan(&tierMapJSON, &semanticMapJSON)
	if err != nil {
		return nil, nil, err
	}

	tierMap := make(map[string]string)
	semanticMap := make(map[string]string)

	if tierMapJSON.Valid && tierMapJSON.String != "" {
		if err := json.Unmarshal([]byte(tierMapJSON.String), &tierMap); err != nil {
			return nil, nil, fmt.Errorf("unmarshal tier_model_map: %w", err)
		}
	}

	if semanticMapJSON.Valid && semanticMapJSON.String != "" {
		if err := json.Unmarshal([]byte(semanticMapJSON.String), &semanticMap); err != nil {
			return nil, nil, fmt.Errorf("unmarshal semantic_model_map: %w", err)
		}
	}

	return tierMap, semanticMap, nil
}

// findCrossRuntimeFallback scans other runtimes in the workspace for the same model
func (r *Router) findCrossRuntimeFallback(ctx context.Context, workspaceID uuid.UUID, targetModel string, excludeRuntimeID uuid.UUID) (string, uuid.UUID) {
	query := `SELECT id, tier_model_map, semantic_model_map FROM agent_runtime WHERE workspace_id = $1 AND id != $2`
	
	rows, err := r.db.QueryContext(ctx, query, workspaceID, excludeRuntimeID)
	if err != nil {
		return "", uuid.Nil
	}
	defer rows.Close()

	for rows.Next() {
		var runtimeID uuid.UUID
		var tierMapJSON, semanticMapJSON sql.NullString
		
		if err := rows.Scan(&runtimeID, &tierMapJSON, &semanticMapJSON); err != nil {
			continue
		}

		// Check tier_model_map
		if tierMapJSON.Valid {
			var tierMap map[string]string
			if err := json.Unmarshal([]byte(tierMapJSON.String), &tierMap); err == nil {
				for _, model := range tierMap {
					if model == targetModel && r.unavailability.IsAvailable(ctx, runtimeID, model) {
						return model, runtimeID
					}
				}
			}
		}

		// Check semantic_model_map
		if semanticMapJSON.Valid {
			var semanticMap map[string]string
			if err := json.Unmarshal([]byte(semanticMapJSON.String), &semanticMap); err == nil {
				for _, model := range semanticMap {
					if model == targetModel && r.unavailability.IsAvailable(ctx, runtimeID, model) {
						return model, runtimeID
					}
				}
			}
		}
	}

	return "", uuid.Nil
}

// escalateTier tries to use a higher tier model if the target tier is unavailable
func (r *Router) escalateTier(ctx context.Context, runtimeID uuid.UUID, targetTier Tier, tierMap map[string]string) string {
	// Escalation order: simple → standard → heavy
	var tryTiers []Tier
	switch targetTier {
	case TierSimple:
		tryTiers = []Tier{TierStandard, TierHeavy}
	case TierStandard:
		tryTiers = []Tier{TierHeavy}
	case TierHeavy:
		return "" // Already at highest tier
	}

	for _, tier := range tryTiers {
		if model, exists := tierMap[string(tier)]; exists && model != "" {
			if r.unavailability.IsAvailable(ctx, runtimeID, model) {
				return model
			}
		}
	}

	return ""
}

// inferTierFromModelName uses name pattern matching to infer tier (fallback only)
func (r *Router) inferTierFromModelName(modelID string) Tier {
	return InferTierFromModelName(modelID)
}

// InferTierFromModelName classifies a model into a tier based on naming patterns.
// This is used for automatic tier assignment during model discovery.
//
// Tier Heavy: flagship models (opus, o1, o3, gpt-4, claude-3.5-sonnet, gemini-pro)
// Tier Standard: balanced models (gpt-4o, claude-3-sonnet, gemini-flash)  
// Tier Simple: fast/cheap models (mini, haiku, nano, 3.5-turbo)
func InferTierFromModelName(modelID string) Tier {
	lower := strings.ToLower(modelID)

	// Heavy tier: Most capable/expensive models
	heavyPatterns := []string{
		"opus",           // claude-3-opus, claude-3.5-opus
		"o1-preview",     // gpt-o1-preview
		"o1-mini",        // Actually more capable than gpt-4o-mini
		"o3",             // OpenAI o3 series
		"gpt-4-turbo",    // gpt-4-turbo-preview
		"gpt-4-32k",      // Large context GPT-4
		"claude-3.5-sonnet", // Claude 3.5 Sonnet (higher tier)
		"gemini-pro",     // Google Gemini Pro
		"gemini-ultra",   // Google Gemini Ultra
		"command-r-plus", // Cohere flagship
		"large",          // Generic large models
		"ultra",
		"max",
		"pro-",           // e.g., gemini-1.5-pro
	}
	for _, pattern := range heavyPatterns {
		if strings.Contains(lower, pattern) {
			return TierHeavy
		}
	}

	// Simple tier: Fast, cheap models
	simplePatterns := []string{
		"mini",           // gpt-4o-mini, gpt-3.5-turbo-mini
		"haiku",          // claude-3-haiku, claude-3.5-haiku
		"flash",          // gemini-flash
		"nano",           // gemini-nano
		"3.5-turbo",      // gpt-3.5-turbo
		"3.5",            // Generic 3.5 models
		"small",          // Generic small models
		"lite",           // Lightweight variants
		"turbo-instruct", // gpt-3.5-turbo-instruct
	}
	for _, pattern := range simplePatterns {
		if strings.Contains(lower, pattern) {
			return TierSimple
		}
	}

	// Standard tier: Everything else (balanced models)
	// Examples: gpt-4o, claude-3-sonnet, gemini-1.5-flash, command-r
	return TierStandard
}

// updateSessionModel updates the session_model for the issue or chat session
func (r *Router) updateSessionModel(ctx context.Context, params RouteParams, model string) {
	if params.IssueID != nil {
		_ = r.sessionAffinity.SetIssueSessionModel(ctx, *params.IssueID, model)
	} else if params.ChatSessionID != nil {
		_ = r.sessionAffinity.SetChatSessionModel(ctx, *params.ChatSessionID, model)
	}
}

// MarkModelUnavailable marks a model as unavailable due to quota/rate limit
func (r *Router) MarkModelUnavailable(ctx context.Context, runtimeID uuid.UUID, model string, ttlSeconds int) error {
	return r.unavailability.MarkUnavailable(ctx, runtimeID, model, ttlSeconds)
}
