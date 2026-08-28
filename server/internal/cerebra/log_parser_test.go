package cerebra

import (
	"testing"
)

func TestLogParser_Parse(t *testing.T) {
	parser := NewLogParser()

	tests := []struct {
		name                  string
		logs                  []string
		expectQuota           bool
		expectRateLimit       bool
		expectContext         bool
		shouldMarkUnavailable bool
	}{
		{
			name: "quota exceeded error",
			logs: []string{
				"Task started",
				"Error: insufficient_quota - you have exceeded your quota",
				"Task failed",
			},
			expectQuota:           true,
			expectRateLimit:       false,
			expectContext:         false,
			shouldMarkUnavailable: true,
		},
		{
			name: "rate limit error",
			logs: []string{
				"Making API request",
				"Error: rate_limit_exceeded - too many requests",
				"Retrying...",
			},
			expectQuota:           false,
			expectRateLimit:       true,
			expectContext:         false,
			shouldMarkUnavailable: true,
		},
		{
			name: "context length error - should NOT mark unavailable",
			logs: []string{
				"Sending prompt",
				"Error: context_length_exceeded - maximum context length is 128k tokens",
				"Task failed",
			},
			expectQuota:           false,
			expectRateLimit:       false,
			expectContext:         true,
			shouldMarkUnavailable: false,
		},
		{
			name: "quota exceeded alternate format",
			logs: []string{
				"API call failed",
				"quota exceeded for this model",
			},
			expectQuota:           true,
			expectRateLimit:       false,
			expectContext:         false,
			shouldMarkUnavailable: true,
		},
		{
			name: "HTTP 429 rate limit",
			logs: []string{
				"Request failed",
				"HTTP 429: Too Many Requests",
			},
			expectQuota:           false,
			expectRateLimit:       true,
			expectContext:         false,
			shouldMarkUnavailable: true,
		},
		{
			name: "no errors",
			logs: []string{
				"Task started",
				"Processing...",
				"Task completed successfully",
			},
			expectQuota:           false,
			expectRateLimit:       false,
			expectContext:         false,
			shouldMarkUnavailable: false,
		},
		{
			name: "multiple error types",
			logs: []string{
				"Error: insufficient quota",
				"Error: rate_limit_exceeded",
				"Error: context_length_exceeded",
			},
			expectQuota:           true,
			expectRateLimit:       true,
			expectContext:         true,
			shouldMarkUnavailable: true, // Quota/rate limit errors present
		},
		{
			name: "case insensitive matching",
			logs: []string{
				"ERROR: INSUFFICIENT_QUOTA",
				"RATE LIMIT EXCEEDED",
			},
			expectQuota:           true,
			expectRateLimit:       true,
			expectContext:         false,
			shouldMarkUnavailable: true,
		},
		{
			name: "token limit exceeded - context error",
			logs: []string{
				"Error: token limit exceeded for model",
				"max tokens exceeded",
			},
			expectQuota:           false,
			expectRateLimit:       false,
			expectContext:         true,
			shouldMarkUnavailable: false,
		},
		{
			name: "insufficient quota variation",
			logs: []string{
				"insufficient quota available",
			},
			expectQuota:           true,
			expectRateLimit:       false,
			expectContext:         false,
			shouldMarkUnavailable: true,
		},
		{
			name: "rate limited variation",
			logs: []string{
				"Request was rate_limited",
			},
			expectQuota:           false,
			expectRateLimit:       true,
			expectContext:         false,
			shouldMarkUnavailable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.logs)

			if result.HasQuotaError != tt.expectQuota {
				t.Errorf("HasQuotaError: expected %v, got %v", tt.expectQuota, result.HasQuotaError)
			}

			if result.HasRateLimitError != tt.expectRateLimit {
				t.Errorf("HasRateLimitError: expected %v, got %v", tt.expectRateLimit, result.HasRateLimitError)
			}

			if result.HasContextError != tt.expectContext {
				t.Errorf("HasContextError: expected %v, got %v", tt.expectContext, result.HasContextError)
			}

			shouldMark := parser.ShouldMarkUnavailable(tt.logs)
			if shouldMark != tt.shouldMarkUnavailable {
				t.Errorf("ShouldMarkUnavailable: expected %v, got %v", tt.shouldMarkUnavailable, shouldMark)
			}
		})
	}
}

func TestLogParser_ParseString(t *testing.T) {
	parser := NewLogParser()

	log := "Task started\nError: insufficient_quota\nTask failed"
	result := parser.ParseString(log)

	if !result.HasQuotaError {
		t.Errorf("expected quota error to be detected")
	}

	if len(result.QuotaErrorLines) == 0 {
		t.Errorf("expected quota error lines to be captured")
	}
}

func TestParseResult_GetErrorType(t *testing.T) {
	tests := []struct {
		name     string
		result   ParseResult
		expected string
	}{
		{
			name: "quota error only",
			result: ParseResult{
				HasQuotaError: true,
			},
			expected: "quota_exceeded",
		},
		{
			name: "rate limit error only",
			result: ParseResult{
				HasRateLimitError: true,
			},
			expected: "rate_limit",
		},
		{
			name: "context error only",
			result: ParseResult{
				HasContextError: true,
			},
			expected: "context_length",
		},
		{
			name: "multiple errors",
			result: ParseResult{
				HasQuotaError:     true,
				HasRateLimitError: true,
			},
			expected: "quota_exceeded,rate_limit",
		},
		{
			name:     "no errors",
			result:   ParseResult{},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorType := tt.result.GetErrorType()
			if errorType != tt.expected {
				t.Errorf("expected error type %s, got %s", tt.expected, errorType)
			}
		})
	}
}

func TestParseResult_GetRelevantLines(t *testing.T) {
	result := ParseResult{
		QuotaErrorLines:     []string{"quota error line 1", "quota error line 2"},
		RateLimitErrorLines: []string{"rate limit error line"},
		ContextErrorLines:   []string{"context error line"},
	}

	lines := result.GetRelevantLines()
	if len(lines) != 4 {
		t.Errorf("expected 4 lines, got %d", len(lines))
	}
}

func TestParseResult_HasAnyError(t *testing.T) {
	tests := []struct {
		name     string
		result   ParseResult
		expected bool
	}{
		{
			name: "has quota error",
			result: ParseResult{
				HasQuotaError: true,
			},
			expected: true,
		},
		{
			name: "has rate limit error",
			result: ParseResult{
				HasRateLimitError: true,
			},
			expected: true,
		},
		{
			name: "has context error",
			result: ParseResult{
				HasContextError: true,
			},
			expected: true,
		},
		{
			name:     "no errors",
			result:   ParseResult{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasError := tt.result.HasAnyError()
			if hasError != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, hasError)
			}
		})
	}
}

func TestCriticalDistinction(t *testing.T) {
	// This test ensures the critical distinction: context errors should NOT
	// mark models unavailable, while quota/rate limit errors should
	parser := NewLogParser()

	contextLogs := []string{
		"Error: context_length_exceeded",
		"Error: maximum context length reached",
		"Error: token limit exceeded",
	}

	quotaLogs := []string{
		"Error: insufficient_quota",
		"Error: quota exceeded",
	}

	rateLimitLogs := []string{
		"Error: rate_limit_exceeded",
		"Error: 429 too many requests",
	}

	// Context errors should NOT trigger unavailability
	if parser.ShouldMarkUnavailable(contextLogs) {
		t.Error("context errors should NOT mark model unavailable")
	}

	// Quota errors SHOULD trigger unavailability
	if !parser.ShouldMarkUnavailable(quotaLogs) {
		t.Error("quota errors SHOULD mark model unavailable")
	}

	// Rate limit errors SHOULD trigger unavailability
	if !parser.ShouldMarkUnavailable(rateLimitLogs) {
		t.Error("rate limit errors SHOULD mark model unavailable")
	}
}
