package storage

import (
	"strings"
	"testing"
)

// TestSanitizeStringValue tests string value sanitization for XSS prevention
func TestSanitizeStringValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Plain text - no changes",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "HTML escaped - script tags",
			input:    "<script>alert('XSS')</script>",
			expected: "&lt;script&gt;alert(&#39;XSS&#39;)&lt;/script&gt;",
		},
		{
			name:     "HTML escaped - img tag with onerror",
			input:    `<img src=x onerror="alert(1)">`,
			expected: "&lt;img src=x onerror=&#34;alert(1)&#34;&gt;",
		},
		{
			name:     "HTML escaped - anchor with javascript",
			input:    `<a href="javascript:alert(1)">Click</a>`,
			expected: "&lt;a href=&#34;javascript:alert(1)&#34;&gt;Click&lt;/a&gt;",
		},
		{
			name:     "HTML escaped - div with onclick",
			input:    `<div onclick="malicious()">Content</div>`,
			expected: "&lt;div onclick=&#34;malicious()&#34;&gt;Content&lt;/div&gt;",
		},
		{
			name:     "Ampersand escaped",
			input:    "Tom & Jerry",
			expected: "Tom &amp; Jerry",
		},
		{
			name:     "Quotes escaped",
			input:    `He said "Hello"`,
			expected: "He said &#34;Hello&#34;",
		},
		{
			name:     "Apostrophe escaped",
			input:    "It's a test",
			expected: "It&#39;s a test",
		},
		{
			name:     "Less than and greater than",
			input:    "5 < 10 > 3",
			expected: "5 &lt; 10 &gt; 3",
		},
		{
			name:     "Unicode preserved",
			input:    "Hello 世界 🌍",
			expected: "Hello 世界 🌍",
		},
		{
			name:     "Newlines preserved",
			input:    "Line 1\nLine 2\nLine 3",
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Very long string - truncated",
			input:    strings.Repeat("a", 12000),
			expected: strings.Repeat("a", 10000),
		},
		{
			name:     "String at max length - not truncated",
			input:    strings.Repeat("b", 10000),
			expected: strings.Repeat("b", 10000),
		},
		{
			name:     "Complex HTML escaped",
			input:    `<iframe src="evil.com"><script>alert(document.cookie)</script></iframe>`,
			expected: "&lt;iframe src=&#34;evil.com&#34;&gt;&lt;script&gt;alert(document.cookie)&lt;/script&gt;&lt;/iframe&gt;",
		},
		{
			name:     "SQL injection attempt escaped",
			input:    "'; DROP TABLE users; --",
			expected: "&#39;; DROP TABLE users; --",
		},
		{
			name:     "Null bytes removed",
			input:    "test\x00value",
			expected: "testvalue",
		},
		{
			name:     "Multiple null bytes removed",
			input:    "a\x00b\x00c\x00",
			expected: "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeStringValue(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// TestSanitizeStringValue_Length tests string length enforcement
func TestSanitizeStringValue_Length(t *testing.T) {
	tests := []struct {
		name       string
		inputLen   int
		expectedLen int
	}{
		{"100 chars", 100, 100},
		{"1000 chars", 1000, 1000},
		{"10000 chars (at limit)", 10000, 10000},
		{"10001 chars (over limit)", 10001, 10000},
		{"50000 chars (way over)", 50000, 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := strings.Repeat("x", tt.inputLen)
			result := SanitizeStringValue(input)
			if len(result) != tt.expectedLen {
				t.Errorf("Expected length %d, got %d", tt.expectedLen, len(result))
			}
		})
	}
}

// TestSanitizePropertyValue tests sanitization of various property value types
func TestSanitizePropertyValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
		modified bool
	}{
		{
			name:     "String with HTML",
			input:    "<script>alert(1)</script>",
			expected: "&lt;script&gt;alert(1)&lt;/script&gt;",
			modified: true,
		},
		{
			name:     "Integer - unchanged",
			input:    42,
			expected: 42,
			modified: false,
		},
		{
			name:     "Float - unchanged",
			input:    3.14,
			expected: 3.14,
			modified: false,
		},
		{
			name:     "Boolean - unchanged",
			input:    true,
			expected: true,
			modified: false,
		},
		{
			name:     "Nil - unchanged",
			input:    nil,
			expected: nil,
			modified: false,
		},
		{
			name:     "String array - all sanitized",
			input:    []any{"<script>", "normal", "<img src=x>"},
			expected: []any{"&lt;script&gt;", "normal", "&lt;img src=x&gt;"},
			modified: true,
		},
		{
			name:     "Mixed array - strings sanitized",
			input:    []any{"<b>text</b>", 123, true},
			expected: []any{"&lt;b&gt;text&lt;/b&gt;", 123, true},
			modified: true,
		},
		{
			name: "Nested map - strings sanitized",
			input: map[string]any{
				"name":   "<script>XSS</script>",
				"age":    30,
				"active": true,
			},
			expected: map[string]any{
				"name":   "&lt;script&gt;XSS&lt;/script&gt;",
				"age":    30,
				"active": true,
			},
			modified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, modified := SanitizePropertyValue(tt.input)

			if modified != tt.modified {
				t.Errorf("Expected modified=%v, got %v", tt.modified, modified)
			}

			// Type-specific comparison
			switch expected := tt.expected.(type) {
			case string:
				if result != expected {
					t.Errorf("Expected '%v', got '%v'", expected, result)
				}
			case int, float64, bool:
				if result != expected {
					t.Errorf("Expected %v, got %v", expected, result)
				}
			case nil:
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
			case []any:
				resultSlice, ok := result.([]any)
				if !ok {
					t.Errorf("Result is not a slice")
					return
				}
				if len(resultSlice) != len(expected) {
					t.Errorf("Expected slice length %d, got %d", len(expected), len(resultSlice))
					return
				}
				for i := range expected {
					if resultSlice[i] != expected[i] {
						t.Errorf("At index %d: expected %v, got %v", i, expected[i], resultSlice[i])
					}
				}
			case map[string]any:
				resultMap, ok := result.(map[string]any)
				if !ok {
					t.Errorf("Result is not a map")
					return
				}
				if len(resultMap) != len(expected) {
					t.Errorf("Expected map length %d, got %d", len(expected), len(resultMap))
					return
				}
				for key, expectedVal := range expected {
					if resultMap[key] != expectedVal {
						t.Errorf("For key %s: expected %v, got %v", key, expectedVal, resultMap[key])
					}
				}
			}
		})
	}
}

// TestSanitizePropertyMap tests sanitization of entire property maps
func TestSanitizePropertyMap(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name: "Mixed types with XSS",
			input: map[string]any{
				"name":        "<script>alert(1)</script>",
				"description": "Normal text",
				"age":         30,
				"score":       95.5,
				"active":      true,
			},
			expected: map[string]any{
				"name":        "&lt;script&gt;alert(1)&lt;/script&gt;",
				"description": "Normal text",
				"age":         30,
				"score":       95.5,
				"active":      true,
			},
		},
		{
			name:     "Empty map",
			input:    map[string]any{},
			expected: map[string]any{},
		},
		{
			name: "All safe values",
			input: map[string]any{
				"count": 100,
				"ratio": 0.75,
				"flag":  false,
			},
			expected: map[string]any{
				"count": 100,
				"ratio": 0.75,
				"flag":  false,
			},
		},
		{
			name: "Complex nested structure",
			input: map[string]any{
				"user": map[string]any{
					"name":  "<b>Admin</b>",
					"email": "test@example.com",
				},
				"tags": []any{"<script>", "normal", "safe"},
			},
			expected: map[string]any{
				"user": map[string]any{
					"name":  "&lt;b&gt;Admin&lt;/b&gt;",
					"email": "test@example.com",
				},
				"tags": []any{"&lt;script&gt;", "normal", "safe"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizePropertyMap(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected map length %d, got %d", len(tt.expected), len(result))
				return
			}

			for key, expectedVal := range tt.expected {
				resultVal, exists := result[key]
				if !exists {
					t.Errorf("Expected key '%s' not found in result", key)
					continue
				}

				// Deep comparison for nested structures
				if !deepEqual(resultVal, expectedVal) {
					t.Errorf("For key '%s': expected %v, got %v", key, expectedVal, resultVal)
				}
			}
		})
	}
}

// Helper function for deep equality comparison
func deepEqual(a, b any) bool {
	switch aVal := a.(type) {
	case map[string]any:
		bMap, ok := b.(map[string]any)
		if !ok || len(aVal) != len(bMap) {
			return false
		}
		for k, v := range aVal {
			if !deepEqual(v, bMap[k]) {
				return false
			}
		}
		return true
	case []any:
		bSlice, ok := b.([]any)
		if !ok || len(aVal) != len(bSlice) {
			return false
		}
		for i := range aVal {
			if !deepEqual(aVal[i], bSlice[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
