package modelrouter

import "strings"

type ModelTier string

const (
	TierSimple   ModelTier = "simple"
	TierStandard ModelTier = "standard"
	TierHeavy    ModelTier = "heavy"
)

func ClassifyTask(title string) (ModelTier, string) {
	t := strings.ToLower(title)

	heavyKeywords := []string{"refactor", "architecture", "design", "migrate", "complex", "debug", "optimize"}
	simpleKeywords := []string{"typo", "rename", "docs", "readme", "comment", "format"}

	for _, kw := range heavyKeywords {
		if strings.Contains(t, kw) {
			return TierHeavy, "keyword:" + kw
		}
	}
	for _, kw := range simpleKeywords {
		if strings.Contains(t, kw) {
			return TierSimple, "keyword:" + kw
		}
	}
	if len(t) > 80 {
		return TierStandard, "length>80"
	}
	return TierStandard, "default"
}

var modelRegistry = map[string]map[ModelTier]string{
	"claude": {
		TierSimple:   "claude-haiku-4-5",
		TierStandard: "claude-sonnet-5",
		TierHeavy:    "claude-opus-4-8",
	},
	"openai": {
		TierSimple:   "gpt-5-mini",
		TierStandard: "gpt-5",
		TierHeavy:    "gpt-5-pro",
	},
}

func SelectModel(provider, issueTitle, fallbackModel string) (model, reason string) {
	tier, why := ClassifyTask(issueTitle)
	if providerModels, ok := modelRegistry[provider]; ok {
		if m, ok := providerModels[tier]; ok && m != "" {
			return m, "tier=" + string(tier) + " " + why
		}
	}
	return fallbackModel, "fallback:no_registry_entry"
}
