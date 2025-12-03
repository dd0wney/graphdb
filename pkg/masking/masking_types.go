package masking

import (
	"regexp"
)

// MaskingStrategy defines how data should be masked
type MaskingStrategy string

const (
	StrategyFull     MaskingStrategy = "full"     // Replace entire value with mask
	StrategyPartial  MaskingStrategy = "partial"  // Show first/last N chars, mask middle
	StrategyHash     MaskingStrategy = "hash"     // Replace with SHA-256 hash
	StrategyRedact   MaskingStrategy = "redact"   // Replace with [REDACTED]
	StrategyTokenize MaskingStrategy = "tokenize" // Replace with consistent token
	StrategyNone     MaskingStrategy = "none"     // No masking
)

// FieldType represents the type of sensitive data
type FieldType string

const (
	FieldTypeEmail      FieldType = "email"
	FieldTypePhone      FieldType = "phone"
	FieldTypeSSN        FieldType = "ssn"
	FieldTypeCreditCard FieldType = "credit_card"
	FieldTypePassword   FieldType = "password"
	FieldTypeAPIKey     FieldType = "api_key"
	FieldTypeIPAddress  FieldType = "ip_address"
	FieldTypeName       FieldType = "name"
	FieldTypeAddress    FieldType = "address"
	FieldTypeGeneric    FieldType = "generic"
)

// MaskingConfig holds configuration for data masking
type MaskingConfig struct {
	DefaultStrategy  MaskingStrategy
	FieldStrategies  map[FieldType]MaskingStrategy
	CustomPatterns   map[string]*regexp.Regexp
	ShowFirstChars   int  // For partial masking
	ShowLastChars    int  // For partial masking
	MaskChar         rune // Character to use for masking
	EnableAutoDetect bool // Auto-detect sensitive fields
}

// MaskingRule defines a custom masking rule
type MaskingRule struct {
	Pattern     *regexp.Regexp
	Strategy    MaskingStrategy
	ReplaceFunc func(string) string // Custom replacement function
}

// DefaultMaskingConfig returns a secure default configuration
func DefaultMaskingConfig() *MaskingConfig {
	return &MaskingConfig{
		DefaultStrategy: StrategyPartial,
		FieldStrategies: map[FieldType]MaskingStrategy{
			FieldTypePassword:   StrategyFull,
			FieldTypeAPIKey:     StrategyFull,
			FieldTypeSSN:        StrategyFull,
			FieldTypeCreditCard: StrategyPartial,
			FieldTypeEmail:      StrategyPartial,
			FieldTypePhone:      StrategyPartial,
			FieldTypeIPAddress:  StrategyHash,
		},
		CustomPatterns:   make(map[string]*regexp.Regexp),
		ShowFirstChars:   2,
		ShowLastChars:    4,
		MaskChar:         '*',
		EnableAutoDetect: true,
	}
}
