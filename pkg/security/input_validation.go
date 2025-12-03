package security

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// InputValidator validates input for security issues
type InputValidator struct{}

// NewInputValidator creates a new input validator
func NewInputValidator() *InputValidator {
	return &InputValidator{}
}

// ValidateString checks a string for injection attacks
func (v *InputValidator) ValidateString(input string, maxLength int) error {
	if len(input) > maxLength {
		return fmt.Errorf("input exceeds maximum length of %d", maxLength)
	}

	// Check for null bytes
	if strings.Contains(input, "\x00") {
		return fmt.Errorf("input contains null bytes")
	}

	// Check for control characters (excluding common whitespace)
	for _, r := range input {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return fmt.Errorf("input contains control characters")
		}
	}

	return nil
}

// ValidateNoSQLInjection checks for SQL injection patterns
func (v *InputValidator) ValidateNoSQLInjection(input string) error {
	patterns := []string{
		`(?i)\b(union|select|insert|update|delete|drop|create|alter|exec|execute)\b`,
		`--`,
		`/\*`,
		`\*/`,
		`;`,
		`'`,
		`"`,
		`\bor\b.*=.*`,
		`\band\b.*=.*`,
	}

	for _, pattern := range patterns {
		matched, _ := regexp.MatchString(pattern, input)
		if matched {
			return fmt.Errorf("potential SQL injection detected")
		}
	}

	return nil
}

// ValidateNoPathTraversal checks for path traversal attempts
func (v *InputValidator) ValidateNoPathTraversal(input string) error {
	dangerous := []string{
		"..",
		"./",
		"../",
		"..\\",
		".\\",
		"%2e%2e",
		"%252e%252e",
		"..;",
		"..%00",
		"..%0d",
		"..%5c",
	}

	lowerInput := strings.ToLower(input)
	for _, pattern := range dangerous {
		if strings.Contains(lowerInput, pattern) {
			return fmt.Errorf("potential path traversal detected")
		}
	}

	return nil
}

// ValidateNoXSS checks for XSS patterns
func (v *InputValidator) ValidateNoXSS(input string) error {
	patterns := []string{
		`<script`,
		`javascript:`,
		`onerror\s*=`,
		`onload\s*=`,
		`onclick\s*=`,
		`onfocus\s*=`,
		`<iframe`,
		`<object`,
		`<embed`,
		`<img.*src=`,
		`<svg`,
		`eval\(`,
	}

	lowerInput := strings.ToLower(input)
	for _, pattern := range patterns {
		matched, _ := regexp.MatchString(pattern, lowerInput)
		if matched {
			return fmt.Errorf("potential XSS detected")
		}
	}

	return nil
}

// ValidateEmail validates email format
func (v *InputValidator) ValidateEmail(email string) error {
	pattern := `^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`
	matched, _ := regexp.MatchString(pattern, email)
	if !matched {
		return fmt.Errorf("invalid email format")
	}

	if len(email) > 254 {
		return fmt.Errorf("email exceeds maximum length")
	}

	return nil
}

// ValidateUsername validates username format
func (v *InputValidator) ValidateUsername(username string) error {
	if len(username) < 3 || len(username) > 32 {
		return fmt.Errorf("username must be 3-32 characters")
	}

	pattern := `^[a-zA-Z0-9_\-]+$`
	matched, _ := regexp.MatchString(pattern, username)
	if !matched {
		return fmt.Errorf("username contains invalid characters")
	}

	return nil
}

// PasswordValidator validates password strength
type PasswordValidator struct {
	MinLength      int
	RequireUpper   bool
	RequireLower   bool
	RequireDigit   bool
	RequireSpecial bool
}

// DefaultPasswordValidator returns a validator with secure defaults
func DefaultPasswordValidator() *PasswordValidator {
	return &PasswordValidator{
		MinLength:      12,
		RequireUpper:   true,
		RequireLower:   true,
		RequireDigit:   true,
		RequireSpecial: true,
	}
}

// Validate checks password strength
func (p *PasswordValidator) Validate(password string) error {
	if len(password) < p.MinLength {
		return fmt.Errorf("password must be at least %d characters", p.MinLength)
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool

	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	if p.RequireUpper && !hasUpper {
		return fmt.Errorf("password must contain uppercase letter")
	}

	if p.RequireLower && !hasLower {
		return fmt.Errorf("password must contain lowercase letter")
	}

	if p.RequireDigit && !hasDigit {
		return fmt.Errorf("password must contain digit")
	}

	if p.RequireSpecial && !hasSpecial {
		return fmt.Errorf("password must contain special character")
	}

	// Check for common weak passwords
	weakPasswords := []string{
		"password", "Password123", "123456", "qwerty", "admin",
		"letmein", "welcome", "monkey", "dragon", "master",
	}

	lowerPassword := strings.ToLower(password)
	for _, weak := range weakPasswords {
		if lowerPassword == strings.ToLower(weak) {
			return fmt.Errorf("password is too common")
		}
	}

	return nil
}

// CalculateStrength returns password strength score (0-100)
func (p *PasswordValidator) CalculateStrength(password string) int {
	score := 0

	// Length bonus
	if len(password) >= 8 {
		score += 20
	}
	if len(password) >= 12 {
		score += 10
	}
	if len(password) >= 16 {
		score += 10
	}

	// Character variety
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			hasSpecial = true
		}
	}

	if hasUpper {
		score += 15
	}
	if hasLower {
		score += 15
	}
	if hasDigit {
		score += 15
	}
	if hasSpecial {
		score += 15
	}

	if score > 100 {
		score = 100
	}

	return score
}
