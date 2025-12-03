package masking

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewMasker(t *testing.T) {
	masker := NewMasker(nil)
	if masker == nil {
		t.Fatal("NewMasker() returned nil")
	}

	if masker.config == nil {
		t.Error("Config is nil")
	}
}

func TestMaskEmail(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	tests := []struct {
		name  string
		email string
	}{
		{
			name:  "Standard email",
			email: "user@example.com",
		},
		{
			name:  "Short email",
			email: "a@b.c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := masker.MaskEmail(tt.email)
			// Should be masked (not equal to original)
			if got == tt.email {
				t.Errorf("MaskEmail(%q) = %q, email not masked", tt.email, got)
			}
			// Should contain asterisks
			if !strings.Contains(got, "*") && len(tt.email) > 6 {
				t.Errorf("MaskEmail(%q) = %q, doesn't contain mask characters", tt.email, got)
			}
		})
	}
}

func TestMaskCreditCard(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	tests := []struct {
		name string
		cc   string
		want string
	}{
		{
			name: "Visa card",
			cc:   "4532-1234-5678-9010",
			want: "************9010",
		},
		{
			name: "No dashes",
			cc:   "4532123456789010",
			want: "************9010",
		},
		{
			name: "With spaces",
			cc:   "4532 1234 5678 9010",
			want: "************9010",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := masker.MaskCreditCard(tt.cc)
			if got != tt.want {
				t.Errorf("MaskCreditCard(%q) = %q, want %q", tt.cc, got, tt.want)
			}
		})
	}
}

func TestMaskSSN(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	ssn := "123-45-6789"
	masked := masker.MaskSSN(ssn)

	// SSN should be fully masked by default
	if !strings.Contains(masked, "*") || len(masked) != len(ssn) {
		t.Errorf("MaskSSN(%q) = %q, want fully masked", ssn, masked)
	}
}

func TestMaskPassword(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	tests := []string{
		"password123",
		"MySecretP@ssw0rd!",
		"short",
		"very-long-password-with-many-characters",
	}

	for _, password := range tests {
		masked := masker.MaskPassword(password)
		if masked != "[PASSWORD]" {
			t.Errorf("MaskPassword(%q) = %q, want [PASSWORD]", password, masked)
		}
	}
}

func TestMaskPhone(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	tests := []struct {
		name  string
		phone string
	}{
		{"US format with dashes", "555-123-4567"},
		{"US format with spaces", "555 123 4567"},
		{"With country code", "+1-555-123-4567"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masked := masker.MaskPhone(tt.phone)
			// Should be masked but retain some structure
			if masked == tt.phone {
				t.Errorf("MaskPhone(%q) returned unmasked value", tt.phone)
			}
		})
	}
}

func TestMaskIPAddress(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	ip := "192.168.1.100"
	masked := masker.MaskIPAddress(ip)

	// IP should be hashed by default
	if masked == ip {
		t.Errorf("MaskIPAddress(%q) returned unmasked value", ip)
	}

	// Hash should be consistent
	masked2 := masker.MaskIPAddress(ip)
	if masked != masked2 {
		t.Error("MaskIPAddress() produces inconsistent hashes")
	}
}

func TestMaskString_Strategies(t *testing.T) {
	tests := []struct {
		name     string
		strategy MaskingStrategy
		input    string
		checkFunc func(string, string) bool
	}{
		{
			name:     "Full masking",
			strategy: StrategyFull,
			input:    "sensitive",
			checkFunc: func(input, output string) bool {
				return output == "*********" && len(output) == len(input)
			},
		},
		{
			name:     "Partial masking",
			strategy: StrategyPartial,
			input:    "sensitivedata",
			checkFunc: func(input, output string) bool {
				return strings.HasPrefix(output, "se") && strings.HasSuffix(output, "data")
			},
		},
		{
			name:     "Redact",
			strategy: StrategyRedact,
			input:    "anything",
			checkFunc: func(input, output string) bool {
				return output == "[REDACTED]"
			},
		},
		{
			name:     "None",
			strategy: StrategyNone,
			input:    "unchanged",
			checkFunc: func(input, output string) bool {
				return output == input
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultMaskingConfig()
			config.DefaultStrategy = tt.strategy
			masker := NewMasker(config)

			output := masker.MaskString(tt.input, FieldTypeGeneric)
			if !tt.checkFunc(tt.input, output) {
				t.Errorf("Strategy %s failed: input=%q, output=%q", tt.strategy, tt.input, output)
			}
		})
	}
}

func TestMaskMap(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	input := map[string]any{
		"username": "alice",
		"email":    "alice@example.com",
		"password": "secret123",
		"age":      30,
		"active":   true,
	}

	masked := masker.MaskMap(input)

	// Check that sensitive fields are masked
	if masked["password"] == input["password"] {
		t.Error("Password was not masked")
	}

	// Email should be partially masked
	maskedEmail := masked["email"].(string)
	if maskedEmail == input["email"] {
		t.Error("Email was not masked")
	}

	// Non-sensitive fields should be unchanged
	if masked["age"] != input["age"] {
		t.Error("Non-sensitive field 'age' was modified")
	}

	if masked["active"] != input["active"] {
		t.Error("Non-sensitive field 'active' was modified")
	}
}

func TestMaskMap_Nested(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	input := map[string]any{
		"user": map[string]any{
			"name":     "Alice",
			"email":    "alice@example.com",
			"password": "secret",
		},
		"public": "data",
	}

	masked := masker.MaskMap(input)

	userMap := masked["user"].(map[string]any)
	if userMap["password"] == "secret" {
		t.Error("Nested password was not masked")
	}
}

func TestAutoMaskString(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	tests := []struct {
		name  string
		input string
		check func(string) bool
	}{
		{
			name:  "Email in text",
			input: "Contact us at support@example.com for help",
			check: func(output string) bool {
				return !strings.Contains(output, "support@example.com")
			},
		},
		{
			name:  "Credit card in text",
			input: "Card: 4532-1234-5678-9010",
			check: func(output string) bool {
				return !strings.Contains(output, "4532-1234-5678-9010")
			},
		},
		{
			name:  "SSN in text",
			input: "SSN: 123-45-6789",
			check: func(output string) bool {
				return !strings.Contains(output, "123-45-6789")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := masker.AutoMaskString(tt.input)
			if !tt.check(output) {
				t.Errorf("AutoMaskString(%q) = %q, sensitive data not masked", tt.input, output)
			}
		})
	}
}

func TestDetectFieldType(t *testing.T) {
	masker := NewMasker(nil)

	tests := []struct {
		fieldName string
		want      FieldType
	}{
		{"email", FieldTypeEmail},
		{"user_email", FieldTypeEmail},
		{"emailAddress", FieldTypeEmail},
		{"password", FieldTypePassword},
		{"pwd", FieldTypePassword},
		{"phone", FieldTypePhone},
		{"mobile", FieldTypePhone},
		{"ssn", FieldTypeSSN},
		{"social_security", FieldTypeSSN},
		{"credit_card", FieldTypeCreditCard},
		{"api_key", FieldTypeAPIKey},
		{"token", FieldTypeAPIKey},
		{"ip_address", FieldTypeIPAddress},
		{"random_field", FieldTypeGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.fieldName, func(t *testing.T) {
			got := masker.detectFieldType(tt.fieldName)
			if got != tt.want {
				t.Errorf("detectFieldType(%q) = %q, want %q", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestIsSensitiveField(t *testing.T) {
	tests := []struct {
		fieldName string
		want      bool
	}{
		{"password", true},
		{"user_password", true},
		{"api_key", true},
		{"secret_token", true},
		{"ssn", true},
		{"credit_card", true},
		{"username", false},
		{"email", false},
		{"age", false},
	}

	for _, tt := range tests {
		t.Run(tt.fieldName, func(t *testing.T) {
			got := IsSensitiveField(tt.fieldName)
			if got != tt.want {
				t.Errorf("IsSensitiveField(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestMaskTokenize(t *testing.T) {
	config := DefaultMaskingConfig()
	config.DefaultStrategy = StrategyTokenize
	config.FieldStrategies[FieldTypeEmail] = StrategyTokenize
	masker := NewMasker(config)

	value := "sensitive@example.com"

	// Tokenize same value multiple times
	token1 := masker.MaskString(value, FieldTypeEmail)
	token2 := masker.MaskString(value, FieldTypeEmail)

	// Should produce same token
	if token1 != token2 {
		t.Error("Tokenization not consistent for same value")
	}

	// Different values should produce different tokens
	token3 := masker.MaskString("different@example.com", FieldTypeEmail)
	if token1 == token3 {
		t.Error("Different values produced same token")
	}

	// Token should contain field type
	if !strings.Contains(token1, "TOK_email") {
		t.Errorf("Token %q doesn't contain field type", token1)
	}
}

func TestAddCustomRule(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	// Add custom rule for masking UUIDs
	uuidPattern := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	masker.AddCustomRule("uuid", uuidPattern, StrategyPartial)

	text := "User ID: 550e8400-e29b-41d4-a716-446655440000"
	masked := masker.ApplyCustomRules(text)

	// UUID should be masked
	if strings.Contains(masked, "550e8400-e29b-41d4-a716-446655440000") {
		t.Error("Custom rule did not mask UUID")
	}
}

func TestSanitizeForLogging(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{
			name:  "String with email",
			input: "User email: test@example.com",
		},
		{
			name: "Map with password",
			input: map[string]any{
				"username": "alice",
				"password": "secret123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := SanitizeForLogging(tt.input)
			if sanitized == nil {
				t.Error("SanitizeForLogging() returned nil")
			}

			// Basic check that it doesn't panic
			_ = sanitized
		})
	}
}

func TestMaskPartial_EdgeCases(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	tests := []struct {
		name  string
		input string
	}{
		{"Empty string", ""},
		{"Single char", "a"},
		{"Two chars", "ab"},
		{"Exactly min length", "abcdef"}, // 2 + 4 = 6
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			_ = masker.maskPartial(tt.input)
		})
	}
}

func TestDefaultMaskingConfig(t *testing.T) {
	config := DefaultMaskingConfig()

	if config.DefaultStrategy == "" {
		t.Error("DefaultStrategy is empty")
	}

	if len(config.FieldStrategies) == 0 {
		t.Error("FieldStrategies is empty")
	}

	if config.MaskChar == 0 {
		t.Error("MaskChar not set")
	}

	// Verify critical fields are configured
	criticalFields := []FieldType{
		FieldTypePassword,
		FieldTypeAPIKey,
		FieldTypeSSN,
		FieldTypeCreditCard,
	}

	for _, field := range criticalFields {
		if _, exists := config.FieldStrategies[field]; !exists {
			t.Errorf("Critical field %s not configured", field)
		}
	}
}

func TestMaskHash_Consistency(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())

	value := "test@example.com"

	hash1 := masker.maskHash(value)
	hash2 := masker.maskHash(value)

	if hash1 != hash2 {
		t.Error("maskHash() produces inconsistent hashes")
	}

	// Different value should produce different hash
	hash3 := masker.maskHash("different@example.com")
	if hash1 == hash3 {
		t.Error("Different values produced same hash")
	}
}
