package masking

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// getStrategy returns the masking strategy for a field type
func (m *Masker) getStrategy(fieldType FieldType) MaskingStrategy {
	if strategy, exists := m.config.FieldStrategies[fieldType]; exists {
		return strategy
	}
	return m.config.DefaultStrategy
}

func (m *Masker) maskFull(value string) string {
	return strings.Repeat(string(m.config.MaskChar), len(value))
}

func (m *Masker) maskPartial(value string) string {
	length := len(value)

	// If value is too short, mask fully
	if length <= m.config.ShowFirstChars+m.config.ShowLastChars {
		return m.maskFull(value)
	}

	first := value[:m.config.ShowFirstChars]
	last := value[length-m.config.ShowLastChars:]
	middleLength := length - m.config.ShowFirstChars - m.config.ShowLastChars

	return first + strings.Repeat(string(m.config.MaskChar), middleLength) + last
}

func (m *Masker) maskHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:8]) // First 8 bytes for brevity
}

func (m *Masker) maskTokenize(value string, fieldType FieldType) string {
	// Generate consistent token for same value
	key := fmt.Sprintf("%s:%s", fieldType, value)

	if token, exists := m.tokens[key]; exists {
		return token
	}

	// Generate new token
	hash := sha256.Sum256([]byte(value))
	token := fmt.Sprintf("TOK_%s_%s", fieldType, hex.EncodeToString(hash[:4]))
	m.tokens[key] = token

	return token
}

func (m *Masker) detectFieldType(fieldName string) FieldType {
	lowerName := strings.ToLower(fieldName)

	// Check for common field name patterns (order matters - more specific first)
	// Check exact patterns first, then substring patterns
	if strings.Contains(lowerName, "email") {
		return FieldTypeEmail
	}
	if strings.Contains(lowerName, "password") || strings.Contains(lowerName, "passwd") || strings.Contains(lowerName, "pwd") {
		return FieldTypePassword
	}
	if strings.Contains(lowerName, "api_key") || strings.Contains(lowerName, "apikey") || strings.Contains(lowerName, "token") || strings.Contains(lowerName, "secret") || strings.Contains(lowerName, "bearer") {
		return FieldTypeAPIKey
	}
	if strings.Contains(lowerName, "phone") || strings.Contains(lowerName, "telephone") || strings.Contains(lowerName, "mobile") || strings.Contains(lowerName, "cell") {
		return FieldTypePhone
	}
	if strings.Contains(lowerName, "ssn") || strings.Contains(lowerName, "social_security") || strings.Contains(lowerName, "socialsecurity") {
		return FieldTypeSSN
	}
	if strings.Contains(lowerName, "credit_card") || strings.Contains(lowerName, "creditcard") || strings.Contains(lowerName, "cc") || strings.Contains(lowerName, "card_number") {
		return FieldTypeCreditCard
	}
	if lowerName == "ip" || strings.Contains(lowerName, "ipaddress") || strings.Contains(lowerName, "ip_address") {
		return FieldTypeIPAddress
	}
	if strings.Contains(lowerName, "firstname") || strings.Contains(lowerName, "lastname") || strings.Contains(lowerName, "fullname") {
		return FieldTypeName
	}
	if strings.Contains(lowerName, "address") || strings.Contains(lowerName, "street") || strings.Contains(lowerName, "city") || strings.Contains(lowerName, "zip") || strings.Contains(lowerName, "postal") {
		return FieldTypeAddress
	}

	return FieldTypeGeneric
}

func (m *Masker) autoMaskString(text string) string {
	result := text

	// Apply patterns in order of specificity
	// Email
	if m.patterns[FieldTypeEmail] != nil {
		result = m.patterns[FieldTypeEmail].ReplaceAllStringFunc(result, func(match string) string {
			return m.MaskEmail(match)
		})
	}

	// SSN
	if m.patterns[FieldTypeSSN] != nil {
		result = m.patterns[FieldTypeSSN].ReplaceAllStringFunc(result, func(match string) string {
			return m.MaskSSN(match)
		})
	}

	// Credit Card
	if m.patterns[FieldTypeCreditCard] != nil {
		result = m.patterns[FieldTypeCreditCard].ReplaceAllStringFunc(result, func(match string) string {
			return m.MaskCreditCard(match)
		})
	}

	// Phone
	if m.patterns[FieldTypePhone] != nil {
		result = m.patterns[FieldTypePhone].ReplaceAllStringFunc(result, func(match string) string {
			return m.MaskPhone(match)
		})
	}

	// IP Address
	if m.patterns[FieldTypeIPAddress] != nil {
		result = m.patterns[FieldTypeIPAddress].ReplaceAllStringFunc(result, func(match string) string {
			return m.MaskIPAddress(match)
		})
	}

	return result
}

func (m *Masker) maskSlice(slice []any) []any {
	result := make([]any, len(slice))

	for i, item := range slice {
		switch v := item.(type) {
		case string:
			if m.config.EnableAutoDetect {
				result[i] = m.autoMaskString(v)
			} else {
				result[i] = v
			}
		case map[string]any:
			result[i] = m.MaskMap(v)
		case []any:
			result[i] = m.maskSlice(v)
		default:
			result[i] = v
		}
	}

	return result
}
