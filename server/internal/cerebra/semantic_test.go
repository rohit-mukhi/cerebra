package cerebra

import (
	"testing"
)

func TestSemanticRouter_Match(t *testing.T) {
	tests := []struct {
		name           string
		domainMap      map[string]string
		prompt         string
		expectedDomain Domain
		expectedModel  string
		shouldMatch    bool
		minConfidence  float64
	}{
		{
			name: "code domain - multiple hits",
			domainMap: map[string]string{
				"code": "qwen-coder",
				"math": "deepseek-r1",
			},
			prompt:         "Fix the bug in the authentication function and add unit tests",
			expectedDomain: DomainCode,
			expectedModel:  "qwen-coder",
			shouldMatch:    true,
			minConfidence:  0.6, // 2 hits: bug, function (test might not be in keywords)
		},
		{
			name: "math domain - high confidence",
			domainMap: map[string]string{
				"code": "qwen-coder",
				"math": "deepseek-r1",
			},
			prompt:         "Calculate the derivative of this equation using the integral theorem",
			expectedDomain: DomainMath,
			expectedModel:  "deepseek-r1",
			shouldMatch:    true,
			minConfidence:  0.7,
		},
		{
			name: "creative domain",
			domainMap: map[string]string{
				"code":     "qwen-coder",
				"creative": "claude-opus-4-5",
			},
			prompt:         "Write a creative story about a robot learning to write poetry",
			expectedDomain: DomainCreative,
			expectedModel:  "claude-opus-4-5",
			shouldMatch:    true,
			minConfidence:  0.3,
		},
		{
			name: "search domain",
			domainMap: map[string]string{
				"search": "grok-3",
			},
			prompt:         "Search for the latest news about AI research today",
			expectedDomain: DomainSearch,
			expectedModel:  "grok-3",
			shouldMatch:    true,
			minConfidence:  0.3,
		},
		{
			name: "data domain",
			domainMap: map[string]string{
				"data": "gpt-4o",
			},
			prompt:         "Analyze this CSV dataset and create a database schema",
			expectedDomain: DomainData,
			expectedModel:  "gpt-4o",
			shouldMatch:    true,
			minConfidence:  0.3,
		},
		{
			name: "no match - unconfigured domain",
			domainMap: map[string]string{
				"code": "qwen-coder",
			},
			prompt:      "Write a poem about mathematics",
			shouldMatch: false,
		},
		{
			name: "no match - no keywords",
			domainMap: map[string]string{
				"code": "qwen-coder",
			},
			prompt:      "Hello there",
			shouldMatch: false,
		},
		{
			name:        "empty domain map",
			domainMap:   map[string]string{},
			prompt:      "Fix the bug in the code",
			shouldMatch: false,
		},
		{
			name: "tie breaker - code wins over creative",
			domainMap: map[string]string{
				"code":     "qwen-coder",
				"creative": "claude-opus",
			},
			prompt:         "Write code to implement a creative algorithm",
			expectedDomain: DomainCode,
			expectedModel:  "qwen-coder",
			shouldMatch:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewSemanticRouter(tt.domainMap)
			result := router.Match(tt.prompt)

			if tt.shouldMatch {
				if result == nil {
					t.Errorf("expected match but got nil")
					return
				}
				if result.Domain != tt.expectedDomain {
					t.Errorf("expected domain %s, got %s", tt.expectedDomain, result.Domain)
				}
				if result.Model != tt.expectedModel {
					t.Errorf("expected model %s, got %s", tt.expectedModel, result.Model)
				}
				if result.Confidence < tt.minConfidence {
					t.Errorf("expected confidence >= %f, got %f", tt.minConfidence, result.Confidence)
				}
				if result.HitCount == 0 {
					t.Errorf("expected hit count > 0, got 0")
				}
			} else {
				if result != nil {
					t.Errorf("expected no match but got domain %s", result.Domain)
				}
			}
		})
	}
}

func TestSemanticRouter_Route(t *testing.T) {
	domainMap := map[string]string{
		"code": "qwen-coder",
		"math": "deepseek-r1",
	}
	router := NewSemanticRouter(domainMap)

	model := router.Route("Fix the bug in the function")
	if model != "qwen-coder" {
		t.Errorf("expected qwen-coder, got %s", model)
	}

	model = router.Route("Calculate the integral")
	if model != "deepseek-r1" {
		t.Errorf("expected deepseek-r1, got %s", model)
	}

	model = router.Route("Hello world")
	if model != "" {
		t.Errorf("expected empty string, got %s", model)
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "Fix the bug in the function",
			expected: []string{"fix", "the", "bug", "in", "the", "function"},
		},
		{
			input:    "Hello, World! How are you?",
			expected: []string{"hello", "world", "how", "are", "you"},
		},
		{
			input:    "test123 code456",
			expected: []string{"test123", "code456"},
		},
		{
			input:    "",
			expected: []string{},
		},
		{
			input:    "   ",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tokenize(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for i, word := range result {
				if word != tt.expected[i] {
					t.Errorf("token %d: expected %s, got %s", i, tt.expected[i], word)
				}
			}
		})
	}
}

func TestCountKeywordHits(t *testing.T) {
	tests := []struct {
		name     string
		words    []string
		keywords []string
		expected int
	}{
		{
			name:     "multiple hits",
			words:    []string{"fix", "the", "bug", "in", "function"},
			keywords: []string{"bug", "function", "code"},
			expected: 2,
		},
		{
			name:     "no hits",
			words:    []string{"hello", "world"},
			keywords: []string{"bug", "function"},
			expected: 0,
		},
		{
			name:     "duplicate words count once",
			words:    []string{"bug", "bug", "bug"},
			keywords: []string{"bug"},
			expected: 1,
		},
		{
			name:     "case insensitive",
			words:    []string{"bug", "function"},
			keywords: []string{"Bug", "Function"},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countKeywordHits(tt.words, tt.keywords)
			if result != tt.expected {
				t.Errorf("expected %d hits, got %d", tt.expected, result)
			}
		})
	}
}

func TestSemanticRouter_GetConfiguredDomains(t *testing.T) {
	domainMap := map[string]string{
		"code": "qwen-coder",
		"math": "deepseek-r1",
	}
	router := NewSemanticRouter(domainMap)

	domains := router.GetConfiguredDomains()
	if len(domains) != 2 {
		t.Errorf("expected 2 domains, got %d", len(domains))
	}

	// Check both domains are present (order doesn't matter)
	foundCode := false
	foundMath := false
	for _, d := range domains {
		if d == DomainCode {
			foundCode = true
		}
		if d == DomainMath {
			foundMath = true
		}
	}

	if !foundCode || !foundMath {
		t.Errorf("expected code and math domains, got %v", domains)
	}
}

func TestDomainKeywordCoverage(t *testing.T) {
	// Ensure all defined domains have keywords
	expectedDomains := []Domain{DomainCode, DomainMath, DomainCreative, DomainSearch, DomainData}

	for _, domain := range expectedDomains {
		keywords, exists := domainKeywords[domain]
		if !exists {
			t.Errorf("domain %s has no keywords defined", domain)
			continue
		}
		if len(keywords) == 0 {
			t.Errorf("domain %s has empty keyword list", domain)
		}
	}
}
