package daemon

import (
	"log/slog"
	"strings"
	"unicode"
)

// SmartRouter implements automatic model selection based on query complexity.
// It follows the flow diagram:
//
//	User query → Task Classifier → Complexity Scorer → Policy Engine
//	→ Model Selector (with fallback) → Agent runtime → Routing Logger
//
// Models are selected from a registry that maps complexity tiers to model IDs.
// When a task has no explicit model override, SmartRouter picks the best model
// for the job and logs its decision. Explicit agent.model settings are always
// respected — the router only acts when the model is unset or set to "auto".

// ComplexityTier represents the assessed complexity of a query.
type ComplexityTier int

const (
	// TierLightweight covers simple Q&A, greetings, lookups, one-liners.
	TierLightweight ComplexityTier = iota
	// TierStandard covers moderate tasks: summaries, simple code edits, drafts.
	TierStandard
	// TierAdvanced covers hard tasks: multi-file refactors, deep reasoning, architecture.
	TierAdvanced
)

func (t ComplexityTier) String() string {
	switch t {
	case TierLightweight:
		return "lightweight"
	case TierStandard:
		return "standard"
	default:
		return "advanced"
	}
}

// TaskCategory classifies the type of work in the query.
type TaskCategory string

const (
	CategoryCoding   TaskCategory = "coding"
	CategoryDocs     TaskCategory = "docs"
	CategoryChat     TaskCategory = "chat"
	CategoryAnalysis TaskCategory = "analysis"
	CategoryOther    TaskCategory = "other"
)

// ModelRegistry maps (provider, complexity tier) to a model ID.
// The model ID must be a valid selector for the given provider.
// An empty string means "use the provider's own default".
type ModelRegistry map[string]map[ComplexityTier]string

// DefaultOpenClawRegistry is the default model registry for the openclaw provider.
// It maps complexity tiers to OpenClaw agent IDs.
// Agents are listed in openclaw.json — these IDs correspond to the agent
// identifiers configured there. Adjust to match your actual agent names.
//
// Tier mapping rationale:
//   - lightweight → nova (restricted tool set, fast replies, web search available)
//   - standard    → iris (general purpose, balanced)
//   - advanced    → sage (full capabilities, complex reasoning tasks)
var DefaultOpenClawRegistry = ModelRegistry{
	"openclaw": {
		TierLightweight: "nova",
		TierStandard:    "iris",
		TierAdvanced:    "sage",
	},
}

// RoutingDecision records what the router chose and why.
type RoutingDecision struct {
	// OriginalModel is what was set before routing (empty = unset).
	OriginalModel string
	// SelectedModel is the model the router chose (may equal OriginalModel if
	// routing was skipped).
	SelectedModel string
	// Category is the classified task type.
	Category TaskCategory
	// Tier is the assessed complexity.
	Tier ComplexityTier
	// Reason is a short human-readable explanation.
	Reason string
	// Routed reports whether the router changed the model.
	Routed bool
}

// RoutingHeuristics holds team-level keyword overrides and custom weights
// that are applied on top of the built-in scoring signals.
type RoutingHeuristics struct {
	// Keywords maps a lowercase keyword to a score delta.
	// Positive values push toward Advanced; negative toward Lightweight.
	// Example: map[string]int{"hotfix": -1, "platform redesign": 3}
	Keywords map[string]int
}

// SmartRouterConfig controls which tasks the router acts on and what models
// it selects. Set Enabled=false to disable routing entirely (all tasks use
// whatever model the agent has configured).
type SmartRouterConfig struct {
	// Enabled turns smart routing on or off. Default: true.
	Enabled bool
	// Registry maps (provider, tier) → model. When nil, DefaultOpenClawRegistry
	// is used.
	Registry ModelRegistry
	// OnlyWhenModelEmpty skips routing when the agent already has a model set.
	// Set to false to allow the router to OVERRIDE an existing model choice.
	// Default: true (router only acts when model is empty or "auto").
	OnlyWhenModelEmpty bool
	// Heuristics holds optional team-level keyword overrides.
	Heuristics RoutingHeuristics
}

// DefaultSmartRouterConfig returns a SmartRouterConfig with sensible defaults.
func DefaultSmartRouterConfig() SmartRouterConfig {
	return SmartRouterConfig{
		Enabled:            true,
		Registry:           DefaultOpenClawRegistry,
		OnlyWhenModelEmpty: true,
	}
}

// RouteModel applies smart model routing to a task's model selection.
// It returns the (possibly updated) model string and a RoutingDecision
// explaining what happened. Call this BEFORE resolveTaskModelSelection.
//
// prompt is the full task prompt text (used for classification).
// provider is the agent runtime protocol family (e.g. "openclaw").
// currentModel is the current agent.model value (may be empty).
// cfg controls routing behaviour.
func RouteModel(
	prompt, provider, currentModel string,
	cfg SmartRouterConfig,
	taskLog *slog.Logger,
) (selectedModel string, decision RoutingDecision) {
	decision.OriginalModel = currentModel

	// Disabled or no registry: pass through unchanged.
	if !cfg.Enabled {
		decision.SelectedModel = currentModel
		decision.Reason = "routing disabled"
		return currentModel, decision
	}

	// Respect explicit model choices unless configured to override them.
	if cfg.OnlyWhenModelEmpty && currentModel != "" && currentModel != "auto" {
		decision.SelectedModel = currentModel
		decision.Reason = "explicit model set; routing skipped"
		return currentModel, decision
	}

	// Look up the registry for this provider.
	registry := cfg.Registry
	if registry == nil {
		registry = DefaultOpenClawRegistry
	}
	providerModels, ok := registry[provider]
	if !ok {
		// No registry entry for this provider; pass through.
		decision.SelectedModel = currentModel
		decision.Reason = "no registry for provider " + provider
		return currentModel, decision
	}

	// Step 1: Classify the task.
	category := classifyTask(prompt)
	decision.Category = category

	// Step 2: Score complexity.
	tier := scoreComplexity(prompt, category, cfg.Heuristics)
	decision.Tier = tier

	// Step 3: Policy engine — look up the model for this tier.
	chosen, ok := providerModels[tier]
	if !ok {
		// No model for this tier; fall back through tiers.
		for t := tier; t >= TierLightweight; t-- {
			if m, found := providerModels[t]; found {
				chosen = m
				break
			}
		}
	}

	if chosen == "" {
		// Registry exists but has no model for any tier; pass through.
		decision.SelectedModel = currentModel
		decision.Reason = "registry has no model for tier " + tier.String()
		return currentModel, decision
	}

	decision.SelectedModel = chosen
	decision.Reason = "auto-selected for " + string(category) + " / " + tier.String()
	decision.Routed = chosen != currentModel

	if decision.Routed {
		taskLog.Info("smart router: model selected",
			"provider", provider,
			"original_model", currentModel,
			"selected_model", chosen,
			"category", string(category),
			"tier", tier.String(),
			"reason", decision.Reason,
		)
	}

	return chosen, decision
}

// classifyTask determines the broad category of work from the prompt text.
func classifyTask(prompt string) TaskCategory {
	lower := strings.ToLower(prompt)

	// Coding signals: file extensions, programming keywords, CLI verbs.
	codingSignals := []string{
		".go", ".py", ".ts", ".js", ".rs", ".java", ".cpp", ".c ", ".rb",
		"func ", "function ", "class ", "def ", "import ", "package ",
		"refactor", "implement", "fix bug", "debug", "test ", "tests",
		"pull request", "pr #", "commit", "branch", "merge",
		"api endpoint", "database", "sql", "query", "schema",
	}
	for _, s := range codingSignals {
		if strings.Contains(lower, s) {
			return CategoryCoding
		}
	}

	// Docs signals.
	docsSignals := []string{
		"document", "readme", "wiki", "write up", "write a", "draft",
		"summarize", "summary", "explain", "describe", "documentation",
		"spec", "requirements", "proposal",
	}
	for _, s := range docsSignals {
		if strings.Contains(lower, s) {
			return CategoryDocs
		}
	}

	// Analysis signals.
	analysisSignals := []string{
		"analyze", "analyse", "compare", "evaluate", "assess", "review",
		"research", "investigate", "diagnose", "audit", "report",
		"architecture", "design", "plan", "strategy",
	}
	for _, s := range analysisSignals {
		if strings.Contains(lower, s) {
			return CategoryAnalysis
		}
	}

	// Chat/simple signals — short prompts, questions, greetings.
	if len(prompt) < 200 {
		return CategoryChat
	}

	return CategoryOther
}

// isDiffPrompt reports whether the prompt is primarily a unified diff.
func isDiffPrompt(prompt string) bool {
	for _, line := range strings.SplitN(prompt, "\n", 10) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "+++") || strings.HasPrefix(trimmed, "@@") {
			return true
		}
	}
	return false
}

// diffDescriptionWords counts words only in non-diff lines (the human
// description surrounding the patch), avoiding over-tiering on large diffs.
func diffDescriptionWords(prompt string) int {
	var sb strings.Builder
	for _, line := range strings.Split(prompt, "\n") {
		if !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "+++") &&
			!strings.HasPrefix(line, "@@") && !strings.HasPrefix(line, "+") &&
			!strings.HasPrefix(line, "-") {
			sb.WriteString(line)
			sb.WriteByte(' ')
		}
	}
	return countWords(sb.String())
}

// scoreComplexity assigns a complexity tier to the prompt based on
// lexical signals and structural features.
func scoreComplexity(prompt string, category TaskCategory, h RoutingHeuristics) ComplexityTier {
	lower := strings.ToLower(prompt)
	score := 0

	// ── Lightweight signals (subtract from score) ──────────────────────────
	lightweightSignals := []string{
		"what is", "what's", "how do i", "can you", "please", "hello",
		"hi ", "hey ", "thanks", "thank you", "weather", "tell me",
		"what time", "who is", "where is",
	}
	for _, s := range lightweightSignals {
		if strings.Contains(lower, s) {
			score--
		}
	}

	// ── Standard signals ───────────────────────────────────────────────────
	standardSignals := []string{
		"update", "change", "modify", "add ", "remove", "create",
		"write a function", "write a script", "fix ", "generate",
		"translate", "convert", "format",
	}
	for _, s := range standardSignals {
		if strings.Contains(lower, s) {
			score++
		}
	}

	// ── Advanced signals ───────────────────────────────────────────────────
	advancedSignals := []string{
		"refactor", "architect", "redesign", "optimize", "performance",
		"scalab", "multi-file", "multiple files", "system design",
		"end-to-end", "integration", "migrate", "migration",
		"security", "authentication", "authorization",
		"complex", "intricate", "comprehensive", "thorough",
		"entire codebase", "all files", "full implementation",
	}
	for _, s := range advancedSignals {
		if strings.Contains(lower, s) {
			score += 2
		}
	}

	// ── Custom team-level keywords ─────────────────────────────────────────
	for kw, delta := range h.Keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			score += delta
		}
	}

	// ── Structural signals ─────────────────────────────────────────────────

	// For diff prompts, measure only the description text to avoid
	// over-tiering on large but simple patches.
	var wordCount int
	if isDiffPrompt(prompt) {
		wordCount = diffDescriptionWords(prompt)
	} else {
		wordCount = countWords(prompt)
	}
	switch {
	case wordCount > 200:
		score += 3
	case wordCount > 100:
		score += 2
	case wordCount > 50:
		score += 1
	case wordCount < 15:
		score -= 2
	}

	// Code blocks suggest non-trivial tasks.
	codeBlockCount := strings.Count(prompt, "```")
	score += codeBlockCount / 2

	// Multiple numbered steps / bullet points suggest complex instructions.
	bulletCount := strings.Count(prompt, "\n-") + strings.Count(prompt, "\n*") +
		strings.Count(prompt, "\n1.") + strings.Count(prompt, "\n2.")
	if bulletCount >= 3 {
		score++
	}

	// Category adjustments.
	switch category {
	case CategoryCoding:
		score++ // Coding tasks are generally more complex than chat.
	case CategoryAnalysis:
		score++ // Analysis tasks are generally more complex than chat.
	case CategoryChat:
		score-- // Chat tasks are generally simpler.
	}

	// Map score to tier.
	switch {
	case score >= 4:
		return TierAdvanced
	case score >= 1:
		return TierStandard
	default:
		return TierLightweight
	}
}

// countWords counts the approximate number of words in a string.
func countWords(s string) int {
	return len(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == '\n' || r == '\r'
	}))
}
