package query

import (
	"strings"
	"testing"
)

// TestSanitizeQuery tests query string sanitization for security
func TestSanitizeQuery(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		expectError bool
		errorType   string
	}{
		{
			name:        "Valid simple query",
			query:       "MATCH (n:Person) RETURN n",
			expectError: false,
		},
		{
			name:        "Valid query with WHERE clause",
			query:       "MATCH (n:Person) WHERE n.age > 30 RETURN n",
			expectError: false,
		},
		{
			name:        "Valid query with properties",
			query:       "MATCH (n:Person {name: 'Alice'}) RETURN n.age",
			expectError: false,
		},
		{
			name:        "Query with <script> tag - XSS attempt",
			query:       "MATCH (n:Person) WHERE n.name = '<script>alert(1)</script>' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with javascript: protocol - XSS attempt",
			query:       "MATCH (n) SET n.url = 'javascript:alert(1)' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with eval() - code injection attempt",
			query:       "MATCH (n) WHERE eval('malicious code') RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with DROP command - SQL injection attempt",
			query:       "MATCH (n:Person); DROP TABLE users; --",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with DELETE all - destructive attempt",
			query:       "DELETE FROM nodes WHERE 1=1",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query too long - DoS prevention",
			query:       strings.Repeat("MATCH (n) RETURN n ", 1000),
			expectError: true,
			errorType:   "too long",
		},
		{
			name:        "Query at max length - should pass",
			query:       strings.Repeat("a", 10000),
			expectError: false,
		},
		{
			name:        "Empty query - invalid",
			query:       "",
			expectError: true,
			errorType:   "empty",
		},
		{
			name:        "Whitespace only query - invalid",
			query:       "   \n\t  ",
			expectError: true,
			errorType:   "empty",
		},
		{
			name:        "Query with onclick attribute - XSS attempt",
			query:       "MATCH (n) SET n.html = '<div onclick=\"alert(1)\">Click</div>' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with onerror attribute - XSS attempt",
			query:       "MATCH (n) SET n.img = '<img src=x onerror=alert(1)>' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with data: URL - potential XSS",
			query:       "MATCH (n) SET n.url = 'data:text/html,<script>alert(1)</script>' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with vbscript: protocol - XSS attempt",
			query:       "MATCH (n) SET n.url = 'vbscript:msgbox(1)' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with file: protocol - potential file access",
			query:       "MATCH (n) SET n.path = 'file:///etc/passwd' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Valid query with legitimate script keyword in string",
			query:       "MATCH (n:Movie) WHERE n.title CONTAINS 'The Script' RETURN n",
			expectError: false, // Should allow 'script' in legitimate context
		},
		{
			name:        "Query with iframe injection",
			query:       "MATCH (n) SET n.content = '<iframe src=\"evil.com\"></iframe>' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with object/embed tags",
			query:       "MATCH (n) SET n.html = '<object data=\"evil.swf\"></object>' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with UNION attack - SQL injection",
			query:       "MATCH (n) WHERE n.id = 1 UNION SELECT * FROM users RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with comment injection",
			query:       "MATCH (n) WHERE n.id = 1 /* comment */ OR 1=1 -- RETURN n",
			expectError: false, // Comments might be legitimate
		},
		{
			name:        "Query with null bytes - null byte injection",
			query:       "MATCH (n) WHERE n.name = 'test\x00' RETURN n",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Query with multiple dangerous patterns",
			query:       "<script>eval('DROP TABLE')</script>",
			expectError: true,
			errorType:   "forbidden pattern",
		},
		{
			name:        "Valid complex query",
			query:       "MATCH (a:Person)-[:KNOWS]->(b:Person) WHERE a.age > 25 AND b.city = 'NYC' RETURN a.name, b.name, COUNT(*) ORDER BY a.name LIMIT 100",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized, err := SanitizeQuery(tt.query)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for query '%s' but got nil", tt.query)
				} else if tt.errorType != "" && !strings.Contains(strings.ToLower(err.Error()), tt.errorType) {
					t.Errorf("Expected error containing '%s' but got: %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for query '%s' but got: %v", tt.query, err)
				}
				if sanitized == "" && tt.query != "" {
					t.Error("Sanitized query should not be empty for valid input")
				}
			}
		})
	}
}

// TestSanitizeQuery_CaseInsensitive tests that dangerous patterns are caught regardless of case
func TestSanitizeQuery_CaseInsensitive(t *testing.T) {
	dangerousPatterns := []string{
		"<SCRIPT>alert(1)</SCRIPT>",
		"<ScRiPt>alert(1)</ScRiPt>",
		"JAVASCRIPT:alert(1)",
		"JavaScript:alert(1)",
		"eval('code')",
		"EVAL('code')",
		"DROP TABLE users",
		"drop table users",
		"DrOp TaBlE users",
	}

	for _, pattern := range dangerousPatterns {
		t.Run(pattern, func(t *testing.T) {
			_, err := SanitizeQuery(pattern)
			if err == nil {
				t.Errorf("Expected error for dangerous pattern '%s' but got nil", pattern)
			}
		})
	}
}

// TestSanitizeQuery_Length tests length validation
func TestSanitizeQuery_Length(t *testing.T) {
	tests := []struct {
		name        string
		length      int
		expectError bool
	}{
		{"1 char", 1, false},
		{"100 chars", 100, false},
		{"1000 chars", 1000, false},
		{"10000 chars (at limit)", 10000, false},
		{"10001 chars (over limit)", 10001, true},
		{"50000 chars (way over)", 50000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := strings.Repeat("a", tt.length)
			_, err := SanitizeQuery(query)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for %d char query but got nil", tt.length)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error for %d char query but got: %v", tt.length, err)
			}
		})
	}
}

// TestSanitizeQuery_NormalizesWhitespace tests that excessive whitespace is normalized
func TestSanitizeQuery_NormalizesWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "Single spaces preserved",
			query:    "MATCH (n) RETURN n",
			expected: "MATCH (n) RETURN n",
		},
		{
			name:     "Multiple spaces normalized",
			query:    "MATCH    (n)    RETURN    n",
			expected: "MATCH (n) RETURN n",
		},
		{
			name:     "Newlines normalized",
			query:    "MATCH (n)\nWHERE n.age > 30\nRETURN n",
			expected: "MATCH (n) WHERE n.age > 30 RETURN n",
		},
		{
			name:     "Tabs normalized",
			query:    "MATCH\t(n)\tRETURN\tn",
			expected: "MATCH (n) RETURN n",
		},
		{
			name:     "Mixed whitespace normalized",
			query:    "MATCH  \n\t  (n)  \t\n  RETURN   n",
			expected: "MATCH (n) RETURN n",
		},
		{
			name:     "Leading/trailing whitespace trimmed",
			query:    "  MATCH (n) RETURN n  ",
			expected: "MATCH (n) RETURN n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized, err := SanitizeQuery(tt.query)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if sanitized != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, sanitized)
			}
		})
	}
}

// TestSanitizeQuery_PreservesValidSQL tests that valid queries are preserved
func TestSanitizeQuery_PreservesValidSQL(t *testing.T) {
	validQueries := []string{
		"MATCH (n:Person) RETURN n",
		"MATCH (n:Person {name: 'Alice'}) RETURN n.age",
		"MATCH (a)-[:KNOWS]->(b) RETURN a, b",
		"MATCH (n) WHERE n.age > 30 AND n.city = 'NYC' RETURN n",
		"CREATE (n:Person {name: 'Bob', age: 25})",
		"MATCH (n:Person) DELETE n",
		"MATCH (n:Person) SET n.age = 31 RETURN n",
		"MATCH (n:Person) REMOVE n.temporary RETURN n",
	}

	for _, query := range validQueries {
		t.Run(query, func(t *testing.T) {
			sanitized, err := SanitizeQuery(query)
			if err != nil {
				t.Errorf("Valid query rejected: %v", err)
			}
			// Should preserve the query (minus whitespace normalization)
			if len(sanitized) == 0 {
				t.Error("Sanitized query should not be empty")
			}
		})
	}
}

// TestSanitizeQuery_Unicode tests handling of unicode characters
func TestSanitizeQuery_Unicode(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		expectError bool
	}{
		{
			name:        "Valid unicode in string",
			query:       "MATCH (n:Person) WHERE n.name = '日本語' RETURN n",
			expectError: false,
		},
		{
			name:        "Unicode in property name",
			query:       "MATCH (n) RETURN n.名前",
			expectError: false,
		},
		{
			name:        "Emoji in query",
			query:       "MATCH (n:Person) WHERE n.status = '😀' RETURN n",
			expectError: false,
		},
		{
			name:        "Unicode with dangerous pattern",
			query:       "MATCH (n) WHERE n.name = '<script>日本語</script>' RETURN n",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SanitizeQuery(tt.query)
			if tt.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}
