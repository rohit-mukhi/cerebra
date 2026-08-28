package cerebra

import (
	"strings"
)

// LogParser scans runtime output for quota and rate limit signals
type LogParser struct{}

// NewLogParser creates a new log parser
func NewLogParser() *LogParser {
	return &LogParser{}
}

// ParseResult contains the result of log parsing
type ParseResult struct {
	HasQuotaError       bool
	HasRateLimitError   bool
	HasContextError     bool
	QuotaErrorLines     []string
	RateLimitErrorLines []string
	ContextErrorLines   []string
}

// Parse scans log lines for quota, rate limit, and context length errors
// Returns true if quota/rate limit errors found (should mark model unavailable)
// Returns false for context errors (should NOT mark model unavailable)
func (lp *LogParser) Parse(logs []string) *ParseResult {
	result := &ParseResult{
		QuotaErrorLines:     []string{},
		RateLimitErrorLines: []string{},
		ContextErrorLines:   []string{},
	}

	// Quota/rate limit signals that should blacklist the model
	quotaSignals := []string{
		"insufficient_quota",
		"quota exceeded",
		"quota_exceeded",
		"insufficient quota",
	}

	rateLimitSignals := []string{
		"rate_limit_exceeded",
		"rate limit exceeded",
		"too many requests",
		"rate_limited",
		"429",
	}

	// Context signals that should NOT blacklist the model
	contextSignals := []string{
		"context_length_exceeded",
		"context length exceeded",
		"maximum context length",
		"context too long",
		"token limit exceeded",
		"max tokens exceeded",
	}

	for _, line := range logs {
		lower := strings.ToLower(line)

		// Check for context errors first (these should NOT trigger unavailability)
		for _, signal := range contextSignals {
			if strings.Contains(lower, signal) {
				result.HasContextError = true
				result.ContextErrorLines = append(result.ContextErrorLines, line)
				break
			}
		}

		// Check for quota errors
		for _, signal := range quotaSignals {
			if strings.Contains(lower, signal) {
				result.HasQuotaError = true
				result.QuotaErrorLines = append(result.QuotaErrorLines, line)
				break
			}
		}

		// Check for rate limit errors
		for _, signal := range rateLimitSignals {
			if strings.Contains(lower, signal) {
				result.HasRateLimitError = true
				result.RateLimitErrorLines = append(result.RateLimitErrorLines, line)
				break
			}
		}
	}

	return result
}

// ShouldMarkUnavailable returns true if the logs indicate the model should be marked unavailable
func (lp *LogParser) ShouldMarkUnavailable(logs []string) bool {
	result := lp.Parse(logs)
	
	// Only mark unavailable for quota/rate limit errors, NOT context errors
	return result.HasQuotaError || result.HasRateLimitError
}

// ParseString is a convenience method that takes a single log string
func (lp *LogParser) ParseString(log string) *ParseResult {
	lines := strings.Split(log, "\n")
	return lp.Parse(lines)
}

// GetErrorType returns a descriptive error type for logging
func (pr *ParseResult) GetErrorType() string {
	var types []string
	
	if pr.HasQuotaError {
		types = append(types, "quota_exceeded")
	}
	if pr.HasRateLimitError {
		types = append(types, "rate_limit")
	}
	if pr.HasContextError {
		types = append(types, "context_length")
	}
	
	if len(types) == 0 {
		return "unknown"
	}
	
	return strings.Join(types, ",")
}

// GetRelevantLines returns all error lines found
func (pr *ParseResult) GetRelevantLines() []string {
	var lines []string
	lines = append(lines, pr.QuotaErrorLines...)
	lines = append(lines, pr.RateLimitErrorLines...)
	lines = append(lines, pr.ContextErrorLines...)
	return lines
}

// HasAnyError returns true if any error type was detected
func (pr *ParseResult) HasAnyError() bool {
	return pr.HasQuotaError || pr.HasRateLimitError || pr.HasContextError
}
