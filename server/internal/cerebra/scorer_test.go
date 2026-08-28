package cerebra

import (
	"testing"
)

func TestScorer_Score(t *testing.T) {
	scorer := NewScorer()

	tests := []struct {
		name         string
		prompt       string
		expectedTier Tier
	}{
		// Simple tier tests
		{
			name:         "simple - short prompt with standard keyword",
			prompt:       "Fix typo in README",
			expectedTier: TierStandard, // "fix" is a standard keyword
		},
		{
			name:         "simple - very short with standard keyword",
			prompt:       "Update version",
			expectedTier: TierStandard, // "update" is a standard keyword
		},
		{
			name:         "simple - no keywords",
			prompt:       "Hello world nice day today",
			expectedTier: TierSimple,
		},

		// Standard tier tests
		{
			name:         "standard - fix keyword",
			prompt:       "Fix the authentication issue",
			expectedTier: TierStandard,
		},
		{
			name:         "standard - add keyword",
			prompt:       "Add logging to the service",
			expectedTier: TierStandard,
		},
		{
			name:         "standard - test keyword",
			prompt:       "Test the new feature",
			expectedTier: TierStandard,
		},
		{
			name:         "standard - implement keyword",
			prompt:       "Implement the user registration flow",
			expectedTier: TierStandard,
		},
		{
			name:         "simple - medium length no keywords",
			prompt:       "Lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua ut enim ad minim veniam quis nostrud",
			expectedTier: TierSimple, // 22 words, no keywords
		},

		// Heavy tier tests
		{
			name:         "heavy - refactor keyword",
			prompt:       "Refactor the authentication system",
			expectedTier: TierHeavy,
		},
		{
			name:         "heavy - architect keyword",
			prompt:       "Architect a new microservices platform",
			expectedTier: TierHeavy,
		},
		{
			name:         "heavy - design keyword",
			prompt:       "Design the entire database schema",
			expectedTier: TierHeavy,
		},
		{
			name:         "heavy - migrate keyword",
			prompt:       "Migrate the entire codebase to TypeScript",
			expectedTier: TierHeavy,
		},
		{
			name:         "heavy - rebuild keyword",
			prompt:       "Rebuild the frontend from scratch",
			expectedTier: TierHeavy,
		},
		{
			name:         "standard - very long prompt without heavy keywords",
			prompt:       "Lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua ut enim ad minim veniam quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur excepteur sint occaecat cupidatat non proident sunt in culpa qui officia deserunt mollit anim id est laborum sed ut perspiciatis unde omnis iste natus error sit voluptatem accusantium",
			expectedTier: TierStandard, // 68 words, no heavy keywords
		},
		{
			name:         "heavy - architecture keyword",
			prompt:       "Design the architecture for the new system",
			expectedTier: TierHeavy,
		},

		// Edge cases
		{
			name:         "case insensitive - heavy",
			prompt:       "REFACTOR the entire codebase",
			expectedTier: TierHeavy,
		},
		{
			name:         "case insensitive - standard",
			prompt:       "FIX the bug",
			expectedTier: TierStandard,
		},
		{
			name:         "mixed case",
			prompt:       "Fix and Refactor the authentication module",
			expectedTier: TierHeavy, // Heavy keyword takes precedence
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.Score(tt.prompt)
			if result != tt.expectedTier {
				t.Errorf("expected tier %s, got %s", tt.expectedTier, result)
			}
		})
	}
}

func TestScorer_ScoreWithDetails(t *testing.T) {
	scorer := NewScorer()

	tests := []struct {
		name              string
		prompt            string
		expectedTier      Tier
		expectHeavySignal bool
		expectStdSignal   bool
	}{
		{
			name:              "heavy with signals",
			prompt:            "Refactor the authentication system",
			expectedTier:      TierHeavy,
			expectHeavySignal: true,
			expectStdSignal:   false,
		},
		{
			name:              "standard with signals",
			prompt:            "Fix the bug in the login function",
			expectedTier:      TierStandard,
			expectHeavySignal: false,
			expectStdSignal:   true,
		},
		{
			name:              "simple - no signals",
			prompt:            "Hello world",
			expectedTier:      TierSimple,
			expectHeavySignal: false,
			expectStdSignal:   false,
		},
		{
			name:              "standard - very long prompt without heavy keywords",
			prompt:            "Lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua ut enim ad minim veniam quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur excepteur sint occaecat cupidatat non proident sunt in culpa qui officia deserunt mollit anim id est laborum",
			expectedTier:      TierStandard, // No heavy keywords, but has medium_length signal
			expectHeavySignal: false,
			expectStdSignal:   true, // Should have "medium_length" signal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := scorer.ScoreWithDetails(tt.prompt)

			if details.Tier != tt.expectedTier {
				t.Errorf("expected tier %s, got %s", tt.expectedTier, details.Tier)
			}

			if tt.expectHeavySignal && len(details.HeavySignals) == 0 {
				t.Errorf("expected heavy signals, got none")
			}
			if !tt.expectHeavySignal && len(details.HeavySignals) > 0 {
				t.Errorf("expected no heavy signals, got %v", details.HeavySignals)
			}

			if tt.expectStdSignal && len(details.StdSignals) == 0 {
				t.Errorf("expected standard signals, got none")
			}
			if !tt.expectStdSignal && len(details.StdSignals) > 0 {
				t.Errorf("expected no standard signals, got %v", details.StdSignals)
			}

			if details.WordCount == 0 && tt.prompt != "" {
				t.Errorf("expected word count > 0 for non-empty prompt")
			}
		})
	}
}

func TestContainsAnyKeyword(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		keywords []string
		expected bool
	}{
		{
			name:     "contains keyword",
			text:     "Fix the bug in the code",
			keywords: []string{"bug", "error"},
			expected: true,
		},
		{
			name:     "no keyword match",
			text:     "Hello world",
			keywords: []string{"bug", "error"},
			expected: false,
		},
		{
			name:     "case insensitive",
			text:     "Fix the BUG",
			keywords: []string{"bug"},
			expected: true,
		},
		{
			name:     "substring match",
			text:     "debug the application",
			keywords: []string{"bug"},
			expected: false, // "bug" is NOT a whole word in "debug"
		},
		{
			name:     "multiple keywords, one matches",
			text:     "Refactor the code",
			keywords: []string{"refactor", "architect"},
			expected: true,
		},
		{
			name:     "empty text",
			text:     "",
			keywords: []string{"bug"},
			expected: false,
		},
		{
			name:     "empty keywords",
			text:     "Fix the bug",
			keywords: []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsAnyKeyword(tt.text, tt.keywords)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestTierPrecedence(t *testing.T) {
	scorer := NewScorer()

	// Heavy keywords should always win over standard keywords
	prompt := "Fix and refactor the authentication system"
	tier := scorer.Score(prompt)
	if tier != TierHeavy {
		t.Errorf("expected heavy tier to take precedence, got %s", tier)
	}

	// Long prompts should be heavy even without keywords
	longPrompt := ""
	for i := 0; i < 110; i++ {
		longPrompt += "word "
	}
	tier = scorer.Score(longPrompt)
	if tier != TierHeavy {
		t.Errorf("expected heavy tier for long prompt, got %s", tier)
	}
}

func TestInferTierFromModel(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		expectedTier Tier
	}{
		// Simple tier models
		{
			name:         "gpt-4o-mini",
			model:        "gpt-4o-mini",
			expectedTier: TierSimple,
		},
		{
			name:         "claude-3-haiku",
			model:        "claude-3-haiku",
			expectedTier: TierSimple,
		},
		{
			name:         "gemini-flash",
			model:        "gemini-2.0-flash-exp",
			expectedTier: TierSimple,
		},
		{
			name:         "nano model",
			model:        "model-nano-v1",
			expectedTier: TierSimple,
		},
		{
			name:         "micro model",
			model:        "llama-micro",
			expectedTier: TierSimple,
		},
		{
			name:         "lite model",
			model:        "gpt-lite-3",
			expectedTier: TierSimple,
		},

		// Heavy tier models
		{
			name:         "claude-opus",
			model:        "claude-opus-4-5",
			expectedTier: TierHeavy,
		},
		{
			name:         "gpt-4-turbo",
			model:        "gpt-4-turbo",
			expectedTier: TierHeavy,
		},
		{
			name:         "ultra model",
			model:        "gemini-ultra",
			expectedTier: TierHeavy,
		},
		{
			name:         "max model",
			model:        "claude-max",
			expectedTier: TierHeavy,
		},
		{
			name:         "preview model",
			model:        "o1-preview",
			expectedTier: TierHeavy,
		},

		// Standard tier models (no special keywords)
		{
			name:         "gpt-4o",
			model:        "gpt-4o",
			expectedTier: TierStandard,
		},
		{
			name:         "claude-3-sonnet",
			model:        "claude-3-sonnet",
			expectedTier: TierStandard,
		},
		{
			name:         "qwen-coder",
			model:        "qwen-coder",
			expectedTier: TierStandard,
		},
		{
			name:         "deepseek-v2",
			model:        "deepseek-v2",
			expectedTier: TierStandard,
		},
		{
			name:         "custom model",
			model:        "my-custom-llm-v3",
			expectedTier: TierStandard,
		},

		// Case insensitivity
		{
			name:         "uppercase MINI",
			model:        "GPT-4O-MINI",
			expectedTier: TierSimple,
		},
		{
			name:         "mixed case Opus",
			model:        "Claude-Opus-4",
			expectedTier: TierHeavy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier := InferTierFromModel(tt.model)
			if tier != tt.expectedTier {
				t.Errorf("InferTierFromModel(%q) = %v, want %v", tt.model, tier, tt.expectedTier)
			}
		})
	}
}

func TestCompareTiers(t *testing.T) {
	tests := []struct {
		name     string
		a        Tier
		b        Tier
		expected int
	}{
		// Equal comparisons
		{
			name:     "simple == simple",
			a:        TierSimple,
			b:        TierSimple,
			expected: 0,
		},
		{
			name:     "standard == standard",
			a:        TierStandard,
			b:        TierStandard,
			expected: 0,
		},
		{
			name:     "heavy == heavy",
			a:        TierHeavy,
			b:        TierHeavy,
			expected: 0,
		},

		// Less than comparisons
		{
			name:     "simple < standard",
			a:        TierSimple,
			b:        TierStandard,
			expected: -1,
		},
		{
			name:     "simple < heavy",
			a:        TierSimple,
			b:        TierHeavy,
			expected: -1,
		},
		{
			name:     "standard < heavy",
			a:        TierStandard,
			b:        TierHeavy,
			expected: -1,
		},

		// Greater than comparisons
		{
			name:     "standard > simple",
			a:        TierStandard,
			b:        TierSimple,
			expected: 1,
		},
		{
			name:     "heavy > simple",
			a:        TierHeavy,
			b:        TierSimple,
			expected: 1,
		},
		{
			name:     "heavy > standard",
			a:        TierHeavy,
			b:        TierStandard,
			expected: 1,
		},

		// Unknown tiers default to standard
		{
			name:     "unknown tier defaults to standard",
			a:        Tier("unknown"),
			b:        TierStandard,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareTiers(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("CompareTiers(%v, %v) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
