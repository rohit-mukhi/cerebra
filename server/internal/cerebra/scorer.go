package cerebra

import (
	"strings"
)

// Tier represents a complexity tier for model selection
type Tier string

const (
	TierSimple   Tier = "simple"
	TierStandard Tier = "standard"
	TierHeavy    Tier = "heavy"
)

// Scorer evaluates prompt complexity and assigns a tier
type Scorer struct{}

// NewScorer creates a new complexity scorer
func NewScorer() *Scorer {
	return &Scorer{}
}

// Score analyzes the prompt and returns the appropriate complexity tier
func (s *Scorer) Score(prompt string) Tier {
	// Tokenize for word count
	words := strings.Fields(prompt)
	wordCount := len(words)

	// Check for heavy complexity keywords
	heavyKeywords := []string{
		"refactor", "architect", "architecture", "design", "migrate",
		"redesign", "restructure", "overhaul", "rebuild", "rewrite",
	}
	if containsAnyKeyword(prompt, heavyKeywords) {
		return TierHeavy
	}

	// Word count thresholds
	if wordCount > 100 {
		return TierHeavy
	}

	// Check for standard complexity keywords
	standardKeywords := []string{
		"fix", "add", "update", "test", "debug", "implement",
		"create", "build", "develop", "modify", "change",
	}
	if containsAnyKeyword(prompt, standardKeywords) {
		return TierStandard
	}

	// Medium length prompts default to standard
	if wordCount > 30 {
		return TierStandard
	}

	// Short, simple prompts
	return TierSimple
}

// containsAnyKeyword checks if the text contains any of the keywords (case-insensitive, whole word)
func containsAnyKeyword(text string, keywords []string) bool {
	lowerText := strings.ToLower(text)
	words := tokenize(lowerText)
	wordSet := make(map[string]bool)
	for _, word := range words {
		wordSet[word] = true
	}

	for _, keyword := range keywords {
		if wordSet[strings.ToLower(keyword)] {
			return true
		}
	}

	return false
}

// ScoreWithDetails returns tier along with scoring details for debugging
type ScoreDetails struct {
	Tier         Tier
	WordCount    int
	HeavySignals []string
	StdSignals   []string
}

// ScoreWithDetails provides detailed information about the scoring decision
func (s *Scorer) ScoreWithDetails(prompt string) ScoreDetails {
	words := strings.Fields(prompt)
	wordCount := len(words)

	details := ScoreDetails{
		WordCount:    wordCount,
		HeavySignals: []string{},
		StdSignals:   []string{},
	}

	// Check for heavy keywords
	heavyKeywords := []string{
		"refactor", "architect", "architecture", "design", "migrate",
		"redesign", "restructure", "overhaul", "rebuild", "rewrite",
	}
	lowerPrompt := strings.ToLower(prompt)
	promptWords := tokenize(lowerPrompt)
	wordSet := make(map[string]bool)
	for _, word := range promptWords {
		wordSet[word] = true
	}

	for _, kw := range heavyKeywords {
		if wordSet[strings.ToLower(kw)] {
			details.HeavySignals = append(details.HeavySignals, kw)
		}
	}

	if len(details.HeavySignals) > 0 {
		details.Tier = TierHeavy
		return details
	}

	if wordCount > 100 {
		details.HeavySignals = append(details.HeavySignals, "long_prompt")
		details.Tier = TierHeavy
		return details
	}

	// Check for standard keywords
	standardKeywords := []string{
		"fix", "add", "update", "test", "debug", "implement",
		"create", "build", "develop", "modify", "change",
	}
	for _, kw := range standardKeywords {
		if wordSet[strings.ToLower(kw)] {
			details.StdSignals = append(details.StdSignals, kw)
		}
	}

	if len(details.StdSignals) > 0 {
		details.Tier = TierStandard
		return details
	}

	if wordCount > 30 {
		details.StdSignals = append(details.StdSignals, "medium_length")
		details.Tier = TierStandard
		return details
	}

	details.Tier = TierSimple
	return details
}

// InferTierFromModel infers the complexity tier from a model name.
// This is used for session affinity to determine if escalation is needed.
//
// Tier inference uses keyword matching on the model name:
// - Simple tier: "mini", "haiku", "flash", "nano", "micro", "lite"
// - Heavy tier: "opus", "ultra", "max", "turbo", "preview"
// - Standard tier: everything else
//
// Examples:
//   - "gpt-4o-mini" → simple
//   - "claude-opus-4-5" → heavy
//   - "gpt-4o" → standard
//   - "qwen-coder" → standard
func InferTierFromModel(model string) Tier {
	lower := strings.ToLower(model)

	// Heavy tier indicators (check first - they take priority)
	heavyKeywords := []string{"opus", "ultra", "max", "turbo", "preview"}
	for _, kw := range heavyKeywords {
		if strings.Contains(lower, kw) {
			return TierHeavy
		}
	}

	// Simple tier indicators
	simpleKeywords := []string{"mini", "haiku", "flash", "nano", "micro", "lite"}
	for _, kw := range simpleKeywords {
		if strings.Contains(lower, kw) {
			return TierSimple
		}
	}

	// Default to standard tier
	return TierStandard
}

// CompareTiers returns -1 if a < b, 0 if a == b, 1 if a > b.
// Tier ordering: simple < standard < heavy
func CompareTiers(a, b Tier) int {
	tierOrder := map[Tier]int{
		TierSimple:   1,
		TierStandard: 2,
		TierHeavy:    3,
	}

	aVal, aOk := tierOrder[a]
	bVal, bOk := tierOrder[b]

	// Unknown tiers default to standard
	if !aOk {
		aVal = tierOrder[TierStandard]
	}
	if !bOk {
		bVal = tierOrder[TierStandard]
	}

	if aVal < bVal {
		return -1
	}
	if aVal > bVal {
		return 1
	}
	return 0
}
