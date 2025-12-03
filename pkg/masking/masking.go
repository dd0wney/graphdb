package masking

import (
	"regexp"
	"strings"
)

// Masker handles data masking operations
type Masker struct {
	config   *MaskingConfig
	tokens   map[string]string          // For tokenization
	patterns map[FieldType]*regexp.Regexp
}

// NewMasker creates a new data masker
func NewMasker(config *MaskingConfig) *Masker {
	if config == nil {
		config = DefaultMaskingConfig()
	}

	m := &Masker{
		config:   config,
		tokens:   make(map[string]string),
		patterns: make(map[FieldType]*regexp.Regexp),
	}

	// Compile regex patterns for auto-detection
	m.patterns[FieldTypeEmail] = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	m.patterns[FieldTypePhone] = regexp.MustCompile(`(\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}`)
	m.patterns[FieldTypeSSN] = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	m.patterns[FieldTypeCreditCard] = regexp.MustCompile(`\b\d{4}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{4}\b`)
	m.patterns[FieldTypeIPAddress] = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	m.patterns[FieldTypeAPIKey] = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|bearer)\s*[:=]\s*['"]?([a-zA-Z0-9\-_]{20,})['"]?`)

	return m
}

// MaskString masks a string value based on field type
func (m *Masker) MaskString(value string, fieldType FieldType) string {
	if value == "" {
		return value
	}

	strategy := m.getStrategy(fieldType)

	switch strategy {
	case StrategyFull:
		return m.maskFull(value)
	case StrategyPartial:
		return m.maskPartial(value)
	case StrategyHash:
		return m.maskHash(value)
	case StrategyRedact:
		return "[REDACTED]"
	case StrategyTokenize:
		return m.maskTokenize(value, fieldType)
	case StrategyNone:
		return value
	default:
		return m.maskPartial(value)
	}
}

// MaskEmail masks an email address
func (m *Masker) MaskEmail(email string) string {
	return m.MaskString(email, FieldTypeEmail)
}

// MaskPhone masks a phone number
func (m *Masker) MaskPhone(phone string) string {
	return m.MaskString(phone, FieldTypePhone)
}

// MaskCreditCard masks a credit card number
func (m *Masker) MaskCreditCard(cc string) string {
	// Remove spaces and dashes
	cc = strings.ReplaceAll(cc, " ", "")
	cc = strings.ReplaceAll(cc, "-", "")

	if len(cc) < 13 || len(cc) > 19 {
		return m.maskFull(cc)
	}

	// Show last 4 digits only (PCI-DSS compliant)
	return strings.Repeat(string(m.config.MaskChar), len(cc)-4) + cc[len(cc)-4:]
}

// MaskSSN masks a social security number
func (m *Masker) MaskSSN(ssn string) string {
	return m.MaskString(ssn, FieldTypeSSN)
}

// MaskPassword always returns a fixed string for passwords
func (m *Masker) MaskPassword(password string) string {
	return "[PASSWORD]"
}

// MaskAPIKey masks an API key
func (m *Masker) MaskAPIKey(key string) string {
	return m.MaskString(key, FieldTypeAPIKey)
}

// MaskIPAddress masks an IP address
func (m *Masker) MaskIPAddress(ip string) string {
	return m.MaskString(ip, FieldTypeIPAddress)
}

// MaskMap masks sensitive fields in a map
func (m *Masker) MaskMap(data map[string]any) map[string]any {
	result := make(map[string]any)

	for key, value := range data {
		// Check if key indicates sensitive data
		fieldType := m.detectFieldType(key)

		switch v := value.(type) {
		case string:
			if fieldType != FieldTypeGeneric {
				result[key] = m.MaskString(v, fieldType)
			} else if m.config.EnableAutoDetect {
				result[key] = m.autoMaskString(v)
			} else {
				result[key] = v
			}
		case map[string]any:
			result[key] = m.MaskMap(v)
		case []any:
			result[key] = m.maskSlice(v)
		default:
			result[key] = v
		}
	}

	return result
}

// AutoMaskString automatically detects and masks sensitive data in a string
func (m *Masker) AutoMaskString(text string) string {
	if !m.config.EnableAutoDetect {
		return text
	}
	return m.autoMaskString(text)
}

// AddCustomRule adds a custom masking rule
func (m *Masker) AddCustomRule(name string, pattern *regexp.Regexp, strategy MaskingStrategy) {
	m.config.CustomPatterns[name] = pattern
}

// ApplyCustomRules applies custom masking rules to text
func (m *Masker) ApplyCustomRules(text string) string {
	result := text

	for _, pattern := range m.config.CustomPatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			return m.maskPartial(match)
		})
	}

	return result
}

// IsSensitiveField checks if a field name indicates sensitive data
func IsSensitiveField(fieldName string) bool {
	lowerName := strings.ToLower(fieldName)
	sensitiveKeywords := []string{
		"password", "passwd", "pwd", "secret", "token", "key",
		"ssn", "social_security", "credit_card", "cvv", "pin",
		"api_key", "bearer", "authorization",
	}

	for _, keyword := range sensitiveKeywords {
		if strings.Contains(lowerName, keyword) {
			return true
		}
	}

	return false
}

// SanitizeForLogging sanitizes a value for safe logging
func SanitizeForLogging(value any) any {
	masker := NewMasker(DefaultMaskingConfig())

	switch v := value.(type) {
	case string:
		return masker.AutoMaskString(v)
	case map[string]any:
		return masker.MaskMap(v)
	default:
		return value
	}
}
