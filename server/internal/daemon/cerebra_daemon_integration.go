package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebra"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// detectMCPUsage inspects the task's runtime MCP overlay, connected apps,
// plugin hook tools, remote MCP connections, and agent skills to decide whether the task
// is expected to call MCP/tool chains. Used to populate TaskMeta.WillUseMCPTools before routing.
func detectMCPUsage(runtimeMCPOverlay []byte, connectedApps []string, pluginHooks int, remoteMCPs int, skillsCount int) bool {
	if len(runtimeMCPOverlay) > 2 { // non-empty JSON object
		return true
	}
	if pluginHooks > 0 || remoteMCPs > 0 || skillsCount > 0 {
		return true
	}
	for _, app := range connectedApps {
		if strings.TrimSpace(app) != "" {
			return true
		}
	}
	return false
}

type ollamaTagsResponse struct {
	Models []struct {
		Name         string   `json:"name"`
		Capabilities []string `json:"capabilities"`
	} `json:"models"`
}

func hasToolCapability(caps []string) bool {
	if len(caps) == 0 {
		return true // If capabilities not reported, assume compatible
	}
	for _, c := range caps {
		if strings.ToLower(c) == "tools" {
			return true
		}
	}
	return false
}

func autoSyncOpenCodeOllama(models []string) {
	home, err := os.UserHomeDir()
	if err != nil || len(models) == 0 {
		return
	}
	configDir := home + "/.config/opencode"
	_ = os.MkdirAll(configDir, 0755)
	configFile := configDir + "/opencode.jsonc"

	modelsMap := make(map[string]map[string]string)
	for _, m := range models {
		cleanName := strings.TrimPrefix(m, "ollama/")
		modelsMap[cleanName] = map[string]string{"name": cleanName}
	}

	cfg := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			"ollama": map[string]any{
				"npm": "@ai-sdk/openai-compatible",
				"options": map[string]string{
					"baseURL": "http://127.0.0.1:11434/v1",
					"apiKey":  "ollama",
				},
				"models": modelsMap,
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err == nil {
		_ = os.WriteFile(configFile, data, 0644)
	}
}

func fetchLocalOllamaModels(ctx context.Context) []string {
	reqCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://127.0.0.1:11434/api/tags", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var tagResp ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagResp); err != nil {
		return nil
	}
	var res []string
	for _, m := range tagResp.Models {
		if strings.TrimSpace(m.Name) != "" && hasToolCapability(m.Capabilities) {
			res = append(res, "ollama/"+strings.TrimSpace(m.Name))
		}
	}
	if len(res) > 0 {
		autoSyncOpenCodeOllama(res)
	}
	return res
}

// buildOpenclawTierMap builds a TierMap for openclaw runtimes.
//
// openclaw exposes agent IDs (e.g. "main"), not raw model names. The agent ID
// is what the CLI accepts via --agent, but it carries no tier signal on its own.
// Each agent entry also carries the underlying model name (e.g. "google/gemini-3.6-flash")
// which CAN be classified. This function:
//  1. Collects all (agentID, underlyingModel) pairs from discovery.
//  2. Classifies each entry by its underlying model name.
//  3. Builds the tier map keyed by tier → agentID (for dispatch), using the
//     underlying model classification to decide which agent goes to which tier.
//
// When multiple agents exist at the same tier, the best-matching one wins
// (frontier keywords, then param size). When only one agent exists (common
// self-hosted setups), it is assigned to all three tiers so routing never falls
// back to an empty candidate pool.
func buildOpenclawTierMap(ctx context.Context, runtimeCmd agent.Command) map[cerebra.Tier]string {
	if listModels == nil {
		return nil
	}
	cat, err := listModels(ctx, "openclaw", runtimeCmd)
	if err != nil || len(cat.Models) == 0 {
		return nil
	}

	// agentEntry holds both the agent ID (for dispatch) and the model used for
	// tier classification. The Model.Label is "<displayName> (<underlyingModel>)"
	// when an underlying model is known; fall back to the ID for classification
	// when the label does not contain a parenthesised model.
	type agentEntry struct {
		agentID         string
		classifyModel   string // underlying model name used only for ClassifyModelTier
	}

	var entries []agentEntry
	for _, m := range cat.Models {
		if m.ID == "" {
			continue
		}
		// Extract underlying model from label: "agentName (google/gemini-3.6-flash)"
		classifyAs := m.ID
		if label := m.Label; label != "" {
			if start := strings.LastIndex(label, "("); start != -1 {
				if end := strings.LastIndex(label, ")"); end > start {
					underlying := strings.TrimSpace(label[start+1 : end])
					if underlying != "" {
						classifyAs = underlying
					}
				}
			}
		}
		entries = append(entries, agentEntry{agentID: m.ID, classifyModel: classifyAs})
	}

	if len(entries) == 0 {
		return nil
	}

	// Classify each agent by its underlying model and bucket into tiers.
	buckets := map[cerebra.Tier][]agentEntry{}
	for _, e := range entries {
		tier := cerebra.ClassifyModelTier(e.classifyModel)
		buckets[tier] = append(buckets[tier], e)
	}

	// Pick the best agent per tier (first match wins; single-agent setups
	// fill all tiers with the same agent so routing always has a candidate).
	pickBest := func(tier cerebra.Tier) string {
		if es := buckets[tier]; len(es) > 0 {
			return es[0].agentID
		}
		return ""
	}

	result := make(map[cerebra.Tier]string)

	// Assign Simple tier — fall up to Standard/Heavy if no simple agent exists.
	if id := pickBest(cerebra.TierSimple); id != "" {
		result[cerebra.TierSimple] = id
	} else if id := pickBest(cerebra.TierStandard); id != "" {
		result[cerebra.TierSimple] = id
	} else if id := pickBest(cerebra.TierHeavy); id != "" {
		result[cerebra.TierSimple] = id
	}

	// Assign Standard tier — fall up to Heavy, down to Simple if needed.
	if id := pickBest(cerebra.TierStandard); id != "" {
		result[cerebra.TierStandard] = id
	} else if id := pickBest(cerebra.TierHeavy); id != "" {
		result[cerebra.TierStandard] = id
	} else if id := pickBest(cerebra.TierSimple); id != "" {
		result[cerebra.TierStandard] = id
	}

	// Assign Heavy tier — fall down to Standard/Simple if no heavy agent exists.
	if id := pickBest(cerebra.TierHeavy); id != "" {
		result[cerebra.TierHeavy] = id
	} else if id := pickBest(cerebra.TierStandard); id != "" {
		result[cerebra.TierHeavy] = id
	} else if id := pickBest(cerebra.TierSimple); id != "" {
		result[cerebra.TierHeavy] = id
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// deriveDynamicRuntimeTierMap automatically probes the local runtime machine's
// installed model catalog (using agent.ListModels and local Ollama APIs) and dynamically builds a
// machine-specific TierMap (Simple, Standard, Heavy).
// If dynamic discovery returns models, it derives the tiers directly from the live models.
// If discovery returns empty or errors, it falls back to known provider defaults.
func deriveDynamicRuntimeTierMap(ctx context.Context, provider string, runtimeCmd agent.Command, unavail *cerebra.UnavailabilityStore, runtimeID string) map[cerebra.Tier]string {
	// openclaw maps agent IDs → underlying models for tier classification.
	// Standard BuildTierMapFromCatalog can't handle agent IDs — use the
	// dedicated builder that looks through the agent to its model.
	if strings.ToLower(provider) == "openclaw" {
		if tierMap := buildOpenclawTierMap(ctx, runtimeCmd); len(tierMap) > 0 {
			return tierMap
		}
		return deriveRuntimeTierMap(provider)
	}

	var modelIDs []string

	// 1. Query local Ollama engine for providers that support local Ollama models (opencode, ollama)
	p := strings.ToLower(provider)
	if p == "opencode" || p == "ollama" || p == "" {
		ollamaModels := fetchLocalOllamaModels(ctx)
		modelIDs = append(modelIDs, ollamaModels...)
	}

	// 2. Discover provider models via agent.ListModels
	if listModels != nil {
		cat, err := listModels(ctx, provider, runtimeCmd)
		if err == nil && len(cat.Models) > 0 {
			for _, m := range cat.Models {
				if m.ID != "" {
					modelIDs = append(modelIDs, m.ID)
				}
			}
		}
	}

	// 3. If dynamic discovery found no models, populate from provider fallback catalog
	if len(modelIDs) == 0 {
		fallbackMap := deriveRuntimeTierMap(provider)
		for _, m := range fallbackMap {
			modelIDs = append(modelIDs, m)
		}
	}

	if len(modelIDs) > 0 {
		var available []string
		for _, m := range modelIDs {
			if unavail != nil && !unavail.IsAvailable(ctx, runtimeID, m) {
				continue
			}
			available = append(available, m)
		}
		if len(available) > 0 {
			tierMap := cerebra.BuildTierMapFromCatalog(available)
			if len(tierMap) > 0 {
				return map[cerebra.Tier]string(tierMap)
			}
		}
	}
	return deriveRuntimeTierMap(provider)
}

// deriveRuntimeTierMap provides static fallback catalogs for known providers
// when dynamic CLI model discovery is not supported or returns empty.
func deriveRuntimeTierMap(provider string) map[cerebra.Tier]string {
	switch strings.ToLower(provider) {
	case "kiro":
		// Full catalog of models available on the kiro runtime (free plan).
		// Ordered from lightest to heaviest so BuildTierMapFromCatalog assigns
		// tiers correctly. Includes all non-claude models as fallbacks so that
		// when claude quota is exhausted the router still has candidates.
		kiroCatalog := []string{
			"claude-haiku-4.5",   // simple
			"glm-5",              // simple
			"minimax-m2.1",       // simple
			"minimax-m2.5",       // standard
			"qwen3-coder-next",   // standard
			"deepseek-3.2",       // standard
			"claude-sonnet-4",    // standard
			"claude-sonnet-4.5",  // heavy
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(kiroCatalog))
	case "codex", "openai":
		codexCatalog := []string{
			"gpt-4o-mini",
			"gpt-4o",
			"o1",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(codexCatalog))
	case "claude", "anthropic":
		claudeCatalog := []string{
			"claude-3-5-haiku",
			"claude-3-5-sonnet",
			"claude-3-opus",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(claudeCatalog))
	case "gemini", "google":
		geminiCatalog := []string{
			"gemini-2.5-flash",
			"gemini-2.5-pro",
			"gemini-ultra",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(geminiCatalog))
	case "ollama", "qwen", "llama":
		localCatalog := []string{
			"llama3.2:3b",
			"qwen2.5-coder:7b",
			"deepseek-r1:14b",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(localCatalog))
	case "kimi":
		kimiCatalog := []string{
			"moonshot-v1-8k",
			"moonshot-v1-32k",
			"moonshot-v1-128k",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(kimiCatalog))
	case "hermes":
		hermesCatalog := []string{
			"hermes-3-llama-3.1-8b",
			"hermes-3-llama-3.1-70b",
			"hermes-3-llama-3.1-405b",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(hermesCatalog))
	case "openclaw":
		// OpenClaw exposes agent IDs (not raw model names) via `openclaw agents list`.
		// The static fallback covers the standard Gemini tier spread that openclaw
		// ships by default; dynamic discovery via discoverOpenclawAgents will
		// override this when the CLI is available and returns a real agent list.
		openclawCatalog := []string{
			"gemini-2.5-flash",   // Simple  — fast, low-cost
			"gemini-2.5-pro",     // Standard — balanced
			"gemini-2.5-pro-exp", // Heavy   — frontier reasoning
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(openclawCatalog))
	default:
		// OpenCode / Universal Runtime default catalog
		openCodeCatalog := []string{
			"opencode/mimo-v2.5-free",
			"opencode/hy3-free",
			"opencode/muse-spark-1.2-contributor-free",
			"opencode/x-preview-f-free",
			"opencode/nemotron-3.5-lightning-free",
			"opencode/nemotron-3-ultra-free",
			"opencode/big-pickle",
		}
		return map[cerebra.Tier]string(cerebra.BuildTierMapFromCatalog(openCodeCatalog))
	}
}

// routeBeforeDispatch calls the Cerebra router (if enabled) and returns the
// selected model. Falls back to agentDefaultModel when the router is nil or
// returns an error.
func routeBeforeDispatch(
	ctx context.Context,
	router *cerebra.Router,
	prompt string,
	meta cerebra.TaskMeta,
	runtimes []cerebra.RuntimeEntry,
	agentDefaultModel string,
) string {
	if router == nil {
		return agentDefaultModel
	}
	result := router.Route(ctx, prompt, meta, runtimes, agentDefaultModel)
	slog.Info("cerebra routed task model", "task_id", meta.TaskID, "tier", string(result.Tier), "model", result.Model, "matched_rule", result.MatchedRule)
	return result.Model
}

// isQuotaOrModelFailure reports whether an agent execution result or error indicates
// quota exhaustion, rate limiting (HTTP 429), or an unavailable/missing model.
func isQuotaOrModelFailure(result agent.Result, err error) (bool, string) {
	// SECURITY GUARD: A successfully completed task (status="completed") with zero
	// error must NEVER have its stdout parsed as a provider failure. This stops user
	// code printing "429" or rate limit text from causing a cascade Denial of Service.
	if (result.Status == "completed" || result.Status == "ok") && err == nil && result.Error == "" {
		return false, ""
	}

	if err != nil {
		errStr := err.Error()
		kind := cerebra.ParseFailure(errStr)
		if cerebra.ShouldMarkUnavailable(kind) {
			return true, errStr
		}
		reason := taskfailure.Classify(errStr)
		if reason == taskfailure.ReasonAgentProviderQuotaLimit || reason == taskfailure.ReasonAgentProviderCapacityOrRateLimit || reason == taskfailure.ReasonAgentModelNotFoundOrUnavailable {
			return true, errStr
		}
	}
	if result.Error != "" {
		kind := cerebra.ParseFailure(result.Error)
		if cerebra.ShouldMarkUnavailable(kind) {
			return true, result.Error
		}
		reason := taskfailure.Classify(result.Error)
		if reason == taskfailure.ReasonAgentProviderQuotaLimit || reason == taskfailure.ReasonAgentProviderCapacityOrRateLimit || reason == taskfailure.ReasonAgentModelNotFoundOrUnavailable {
			return true, result.Error
		}
	}
	if result.Output != "" {
		kind := cerebra.ParseFailure(result.Output)
		if cerebra.ShouldMarkUnavailable(kind) {
			return true, result.Output
		}
		reason := taskfailure.Classify(result.Output)
		if reason == taskfailure.ReasonAgentProviderQuotaLimit || reason == taskfailure.ReasonAgentProviderCapacityOrRateLimit || reason == taskfailure.ReasonAgentModelNotFoundOrUnavailable {
			return true, result.Output
		}
	}
	return false, ""
}
