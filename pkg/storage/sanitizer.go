package storage

import (
	"html"
	"strings"
)

const (
	// MaxPropertyValueLength is the maximum allowed length for string property values
	MaxPropertyValueLength = 10000
)

// SanitizeStringValue sanitizes a string value to prevent XSS attacks
// It performs the following:
// - HTML escaping to neutralize script tags, event handlers, etc.
// - Removal of null bytes
// - Length limiting to prevent DoS
func SanitizeStringValue(s string) string {
	// Remove null bytes (potential for null byte injection)
	s = strings.ReplaceAll(s, "\x00", "")

	// HTML escape to prevent XSS
	// This converts: < > & " ' to their HTML entity equivalents
	s = html.EscapeString(s)

	// Enforce maximum length
	if len(s) > MaxPropertyValueLength {
		s = s[:MaxPropertyValueLength]
	}

	return s
}

// SanitizePropertyValue sanitizes a property value based on its type
// Returns the sanitized value and a boolean indicating if it was modified
func SanitizePropertyValue(value interface{}) (interface{}, bool) {
	if value == nil {
		return nil, false
	}

	modified := false

	switch v := value.(type) {
	case string:
		sanitized := SanitizeStringValue(v)
		return sanitized, sanitized != v

	case []interface{}:
		// Sanitize each element in the array
		sanitizedSlice := make([]interface{}, len(v))
		for i, elem := range v {
			sanitizedElem, elemModified := SanitizePropertyValue(elem)
			sanitizedSlice[i] = sanitizedElem
			if elemModified {
				modified = true
			}
		}
		return sanitizedSlice, modified

	case map[string]interface{}:
		// Sanitize each value in the map
		sanitizedMap := make(map[string]interface{}, len(v))
		for key, val := range v {
			sanitizedVal, valModified := SanitizePropertyValue(val)
			sanitizedMap[key] = sanitizedVal
			if valModified {
				modified = true
			}
		}
		return sanitizedMap, modified

	default:
		// For other types (int, float64, bool), return as-is
		return value, false
	}
}

// SanitizePropertyMap sanitizes all string values in a property map
// This should be called before storing properties to prevent XSS attacks
func SanitizePropertyMap(properties map[string]interface{}) map[string]interface{} {
	if properties == nil {
		return nil
	}

	sanitized := make(map[string]interface{}, len(properties))
	for key, value := range properties {
		sanitizedValue, _ := SanitizePropertyValue(value)
		sanitized[key] = sanitizedValue
	}

	return sanitized
}
