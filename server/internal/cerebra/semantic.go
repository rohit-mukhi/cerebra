package cerebra

import (
	"strings"
	"unicode"
)

// Domain represents a semantic domain that can be matched against prompts
type Domain string

const (
	DomainCode     Domain = "code"
	DomainMath     Domain = "math"
	DomainCreative Domain = "creative"
	DomainSearch   Domain = "search"
	DomainData     Domain = "data"
)

// domainKeywords maps each domain to its hardcoded keyword set
// These keywords are matched case-insensitively as whole words
var domainKeywords = map[Domain][]string{
	DomainCode: {
		"code", "function", "class", "bug", "implement", "api", "script",
		"compile", "syntax", "refactor", "library", "module", "import",
		"variable", "algorithm", "program", "debug", "error", "exception", "test",
	},
	DomainMath: {
		"math", "calculate", "equation", "formula", "integral", "derivative",
		"proof", "theorem", "matrix", "vector", "probability", "statistics",
		"algebra", "geometry", "compute", "solve", "numerical",
	},
	DomainCreative: {
		"write", "story", "poem", "creative", "essay", "narrative", "fiction",
		"blog", "draft", "tone", "style", "rewrite", "summarize", "translate", "explain",
	},
	DomainSearch: {
		"search", "find", "lookup", "research", "browse", "fetch", "retrieve",
		"news", "latest", "current", "today", "web",
	},
	DomainData: {
		"data", "csv", "json", "sql", "query", "database", "table", "chart",
		"plot", "analyze", "dataset", "pipeline", "etl", "schema",
	},
}

// SemanticRouter performs domain-based routing using hardcoded keyword matching
type SemanticRouter struct {
	// domainMap maps domain names to model IDs (comes from runtime.semantic_model_map)
	domainMap map[string]string
}

// NewSemanticRouter creates a new semantic router with the given domain→model mapping
func NewSemanticRouter(domainMap map[string]string) *SemanticRouter {
	return &SemanticRouter{
		domainMap: domainMap,
	}
}

// MatchResult contains the result of a semantic match
type MatchResult struct {
	Domain     Domain
	Model      string
	Confidence float64 // 0.0 to 1.0, based on number of keyword hits
	HitCount   int     // Number of keywords matched
}

// Route attempts to match the prompt to a domain and returns the corresponding model
// Returns empty string if no match is found or no model is configured for the matched domain
func (sr *SemanticRouter) Route(prompt string) string {
	result := sr.Match(prompt)
	if result == nil {
		return ""
	}
	return result.Model
}

// Match performs keyword matching and returns detailed match information
// Returns nil if no domain matches or no model is configured
func (sr *SemanticRouter) Match(prompt string) *MatchResult {
	if sr.domainMap == nil || len(sr.domainMap) == 0 {
		return nil
	}

	// Tokenize prompt into words
	words := tokenize(prompt)
	if len(words) == 0 {
		return nil
	}

	// Count keyword hits per domain
	domainHits := make(map[Domain]int)
	maxHits := 0

	for domain, keywords := range domainKeywords {
		// Skip domains not configured in the domain map
		if _, exists := sr.domainMap[string(domain)]; !exists {
			continue
		}

		hits := countKeywordHits(words, keywords)
		if hits > 0 {
			domainHits[domain] = hits
			if hits > maxHits {
				maxHits = hits
			}
		}
	}

	// No matches found
	if maxHits == 0 {
		return nil
	}

	// Find domain with most hits (break ties by domain order: code, math, creative, search, data)
	var bestDomain Domain
	domainPriority := []Domain{DomainCode, DomainMath, DomainCreative, DomainSearch, DomainData}
	
	for _, domain := range domainPriority {
		if domainHits[domain] == maxHits {
			bestDomain = domain
			break
		}
	}

	// Calculate confidence based on hit density
	// confidence = min(hits / 3, 1.0) - 3+ hits = high confidence
	confidence := float64(maxHits) / 3.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return &MatchResult{
		Domain:     bestDomain,
		Model:      sr.domainMap[string(bestDomain)],
		Confidence: confidence,
		HitCount:   maxHits,
	}
}

// tokenize splits the prompt into lowercase words, removing punctuation
func tokenize(text string) []string {
	var words []string
	var currentWord strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			currentWord.WriteRune(unicode.ToLower(r))
		} else if currentWord.Len() > 0 {
			words = append(words, currentWord.String())
			currentWord.Reset()
		}
	}

	if currentWord.Len() > 0 {
		words = append(words, currentWord.String())
	}

	return words
}

// countKeywordHits counts how many keywords from the list appear in the words
// Uses exact word matching (not substring)
func countKeywordHits(words []string, keywords []string) int {
	// Build a set of keywords for O(1) lookup
	keywordSet := make(map[string]bool)
	for _, kw := range keywords {
		keywordSet[strings.ToLower(kw)] = true
	}

	// Count unique keyword hits
	hits := 0
	seen := make(map[string]bool)
	for _, word := range words {
		if keywordSet[word] && !seen[word] {
			hits++
			seen[word] = true
		}
	}

	return hits
}

// GetConfiguredDomains returns the list of domains that have models configured
func (sr *SemanticRouter) GetConfiguredDomains() []Domain {
	domains := make([]Domain, 0, len(sr.domainMap))
	for domainStr := range sr.domainMap {
		domains = append(domains, Domain(domainStr))
	}
	return domains
}
