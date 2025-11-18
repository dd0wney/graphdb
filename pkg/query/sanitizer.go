package query

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// MaxQueryLength is the maximum allowed query length (10KB)
	MaxQueryLength = 10000
)

var (
	// Dangerous patterns that could indicate injection attacks
	dangerousPatterns = []string{
		"<script",      // XSS: Script tags
		"</script>",    // XSS: Script closing tags
		"javascript:",  // XSS: JavaScript protocol
		"eval(",        // Code injection
		"eval (",       // Code injection with space
		"drop ",        // SQL injection: DROP command
		"drop\t",       // SQL injection: DROP with tab
		"delete from",  // SQL injection: DELETE FROM
		"onclick",      // XSS: Event handler
		"onerror",      // XSS: Error handler
		"onload",       // XSS: Load handler
		"onmouseover",  // XSS: Mouse event handler
		"data:",        // XSS: Data URL
		"vbscript:",    // XSS: VBScript protocol
		"file:",        // File access attempt
		"<iframe",      // XSS: Iframe injection
		"<object",      // XSS: Object tag
		"<embed",       // XSS: Embed tag
		"union select", // SQL injection: UNION attack
		"\x00",         // Null byte injection
	}

	// whitespaceRegex matches one or more whitespace characters
	whitespaceRegex = regexp.MustCompile(`\s+`)
)

// SanitizeQuery validates and sanitizes a query string for security
// It checks for:
// - Maximum length (DoS prevention)
// - Dangerous patterns (XSS, SQL injection, code injection)
// - Empty/whitespace-only queries
// - Normalizes whitespace
func SanitizeQuery(query string) (string, error) {
	// Check if query is empty after trimming
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", errors.New("query cannot be empty")
	}

	// Check length before normalization to prevent DoS
	if len(query) > MaxQueryLength {
		return "", fmt.Errorf("query too long: maximum %d characters allowed, got %d", MaxQueryLength, len(query))
	}

	// Check for dangerous patterns (case-insensitive)
	queryLower := strings.ToLower(query)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(queryLower, strings.ToLower(pattern)) {
			return "", fmt.Errorf("query contains forbidden pattern: %s", pattern)
		}
	}

	// Normalize whitespace: replace multiple spaces/tabs/newlines with single space
	normalized := whitespaceRegex.ReplaceAllString(trimmed, " ")

	return normalized, nil
}
