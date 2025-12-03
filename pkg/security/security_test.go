package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"testing"
	"time"
)

func TestNewSecurityScanner(t *testing.T) {
	scanner := NewSecurityScanner()
	if scanner == nil {
		t.Fatal("NewSecurityScanner() returned nil")
	}

	if len(scanner.GetVulnerabilities()) != 0 {
		t.Error("New scanner should have no vulnerabilities")
	}
}

func TestSecurityScanner_AddVulnerability(t *testing.T) {
	scanner := NewSecurityScanner()

	vuln := Vulnerability{
		Type:        VulnInjection,
		Severity:    SeverityHigh,
		Description: "Test vulnerability",
	}

	scanner.addVulnerability(vuln)

	vulns := scanner.GetVulnerabilities()
	if len(vulns) != 1 {
		t.Errorf("Expected 1 vulnerability, got %d", len(vulns))
	}

	if vulns[0].Type != VulnInjection {
		t.Errorf("Type = %s, want %s", vulns[0].Type, VulnInjection)
	}
}

func TestSecurityScanner_GetVulnerabilitiesBySeverity(t *testing.T) {
	scanner := NewSecurityScanner()

	scanner.addVulnerability(Vulnerability{Severity: SeverityCritical})
	scanner.addVulnerability(Vulnerability{Severity: SeverityHigh})
	scanner.addVulnerability(Vulnerability{Severity: SeverityCritical})
	scanner.addVulnerability(Vulnerability{Severity: SeverityMedium})

	critical := scanner.GetVulnerabilitiesBySeverity(SeverityCritical)
	if len(critical) != 2 {
		t.Errorf("Expected 2 critical vulnerabilities, got %d", len(critical))
	}

	high := scanner.GetVulnerabilitiesBySeverity(SeverityHigh)
	if len(high) != 1 {
		t.Errorf("Expected 1 high vulnerability, got %d", len(high))
	}
}

func TestInputValidator_ValidateString(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name      string
		input     string
		maxLength int
		wantError bool
	}{
		{
			name:      "Valid string",
			input:     "Hello World",
			maxLength: 100,
			wantError: false,
		},
		{
			name:      "Too long",
			input:     "This is a very long string",
			maxLength: 10,
			wantError: true,
		},
		{
			name:      "Null byte",
			input:     "Hello\x00World",
			maxLength: 100,
			wantError: true,
		},
		{
			name:      "Control character",
			input:     "Hello\x01World",
			maxLength: 100,
			wantError: true,
		},
		{
			name:      "Valid with tab",
			input:     "Hello\tWorld",
			maxLength: 100,
			wantError: false,
		},
		{
			name:      "Valid with newline",
			input:     "Hello\nWorld",
			maxLength: 100,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateString(tt.input, tt.maxLength)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateString() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestInputValidator_ValidateNoSQLInjection(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"Safe input", "john_doe", false},
		{"SQL UNION", "' UNION SELECT", true},
		{"SQL DROP", "'; DROP TABLE users--", true},
		{"SQL OR", "' OR '1'='1", true},
		{"Comment", "admin'--", true},
		{"Semicolon", "test; DELETE FROM", true},
		{"Safe with spaces", "John Doe Jr", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateNoSQLInjection(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateNoSQLInjection() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestInputValidator_ValidateNoPathTraversal(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"Safe path", "documents/file.txt", false},
		{"Dotdot", "../../etc/passwd", true},
		{"Windows path", "..\\..\\windows\\system32", true},
		{"URL encoded", "%2e%2e/etc/passwd", true},
		{"Semicolon", "..;/etc/passwd", true},
		{"Null byte", "..%00/etc/passwd", true},
		{"Safe filename", "my-file_123.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateNoPathTraversal(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateNoPathTraversal() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestInputValidator_ValidateNoXSS(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"Safe text", "Hello World", false},
		{"Script tag", "<script>alert('xss')</script>", true},
		{"JavaScript protocol", "javascript:alert(1)", true},
		{"Onerror", "<img src=x onerror=alert(1)>", true},
		{"Onload", "<body onload=alert(1)>", true},
		{"Iframe", "<iframe src='evil.com'></iframe>", true},
		{"Eval", "eval(malicious)", true},
		{"Safe HTML-like", "Cost: < $100", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateNoXSS(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateNoXSS() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestInputValidator_ValidateEmail(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name      string
		email     string
		wantError bool
	}{
		{"Valid email", "user@example.com", false},
		{"Valid with plus", "user+tag@example.com", false},
		{"Valid with dots", "first.last@example.com", false},
		{"Missing @", "userexample.com", true},
		{"Missing domain", "user@", true},
		{"Missing TLD", "user@example", true},
		{"Invalid characters", "user @example.com", true},
		{"Too long", "a" + string(make([]byte, 250)) + "@example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateEmail(tt.email)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateEmail() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestInputValidator_ValidateUsername(t *testing.T) {
	validator := NewInputValidator()

	tests := []struct {
		name      string
		username  string
		wantError bool
	}{
		{"Valid username", "john_doe", false},
		{"Valid with dash", "john-doe", false},
		{"Valid with numbers", "user123", false},
		{"Too short", "ab", true},
		{"Too long", "this_is_a_very_long_username_that_exceeds_limit", true},
		{"Invalid characters", "john doe", true},
		{"Special chars", "john@doe", true},
		{"Valid mixed", "User_123-test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateUsername(tt.username)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateUsername() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestPasswordValidator_Validate(t *testing.T) {
	validator := DefaultPasswordValidator()

	tests := []struct {
		name      string
		password  string
		wantError bool
	}{
		{"Strong password", "MyP@ssw0rd123!", false},
		{"Too short", "Pass1!", true},
		{"No uppercase", "myp@ssw0rd123!", true},
		{"No lowercase", "MYP@SSW0RD123!", true},
		{"No digit", "MyP@ssword!", true},
		{"No special", "MyPassword123", true},
		{"Common password", "Password123", true},
		{"All requirements", "C0mpl3x!Pass", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.password)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestPasswordValidator_CalculateStrength(t *testing.T) {
	validator := DefaultPasswordValidator()

	tests := []struct {
		name     string
		password string
		minScore int
	}{
		{"Weak", "pass", 0},
		{"Medium", "Password1", 50},
		{"Strong", "MyP@ssw0rd123!", 80},
		{"Very strong", "C0mpl3x!P@ssw0rd#2024", 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := validator.CalculateStrength(tt.password)
			if score < tt.minScore {
				t.Errorf("CalculateStrength() = %d, want at least %d", score, tt.minScore)
			}
		})
	}
}

func TestTLSValidator_ValidateTLSConfig(t *testing.T) {
	validator := NewTLSValidator()

	tests := []struct {
		name      string
		config    *tls.Config
		wantVulns int
	}{
		{
			name: "Secure config",
			config: &tls.Config{
				MinVersion: tls.VersionTLS12,
				CipherSuites: []uint16{
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				},
			},
			wantVulns: 0,
		},
		{
			name: "Old TLS version",
			config: &tls.Config{
				MinVersion: tls.VersionTLS10,
			},
			wantVulns: 1,
		},
		{
			name: "Insecure skip verify",
			config: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true,
			},
			wantVulns: 1,
		},
		{
			name: "Weak cipher",
			config: &tls.Config{
				MinVersion: tls.VersionTLS12,
				CipherSuites: []uint16{
					tls.TLS_RSA_WITH_RC4_128_SHA,
				},
			},
			wantVulns: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vulns := validator.ValidateTLSConfig(tt.config)
			if len(vulns) != tt.wantVulns {
				t.Errorf("ValidateTLSConfig() returned %d vulnerabilities, want %d", len(vulns), tt.wantVulns)
			}
		})
	}
}

func TestCryptoValidator_ValidateRSAKeySize(t *testing.T) {
	validator := NewCryptoValidator()

	tests := []struct {
		name      string
		keySize   int
		wantError bool
	}{
		{"1024 bit (weak)", 1024, true},
		{"2048 bit (secure)", 2048, false},
		{"4096 bit (very secure)", 4096, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := rsa.GenerateKey(rand.Reader, tt.keySize)
			if err != nil {
				t.Fatalf("Failed to generate key: %v", err)
			}

			err = validator.ValidateRSAKeySize(key)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateRSAKeySize() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestCryptoValidator_ValidateRandomness(t *testing.T) {
	validator := NewCryptoValidator()

	err := validator.ValidateRandomness(32)
	if err != nil {
		t.Errorf("ValidateRandomness() failed: %v", err)
	}
}

func TestPenetrationTestHelper_InjectionPayloads(t *testing.T) {
	helper := NewPenetrationTestHelper()
	payloads := helper.InjectionPayloads()

	if len(payloads) == 0 {
		t.Error("InjectionPayloads() returned empty list")
	}

	// Verify common payloads are present
	found := false
	for _, payload := range payloads {
		if payload == "' OR '1'='1" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Common SQL injection payload not found")
	}
}

func TestPenetrationTestHelper_XSSPayloads(t *testing.T) {
	helper := NewPenetrationTestHelper()
	payloads := helper.XSSPayloads()

	if len(payloads) == 0 {
		t.Error("XSSPayloads() returned empty list")
	}

	// Verify script tag is present
	found := false
	for _, payload := range payloads {
		if payload == "<script>alert('XSS')</script>" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Common XSS payload not found")
	}
}

func TestPenetrationTestHelper_PathTraversalPayloads(t *testing.T) {
	helper := NewPenetrationTestHelper()
	payloads := helper.PathTraversalPayloads()

	if len(payloads) == 0 {
		t.Error("PathTraversalPayloads() returned empty list")
	}

	// Verify common payload is present
	found := false
	for _, payload := range payloads {
		if payload == "../../etc/passwd" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Common path traversal payload not found")
	}
}

func TestPenetrationTestHelper_TestInjection(t *testing.T) {
	helper := NewPenetrationTestHelper()
	validator := NewInputValidator()

	// Test with proper validation (should catch all)
	failures := helper.TestInjection(func(input string) error {
		return validator.ValidateNoSQLInjection(input)
	})

	if len(failures) > 0 {
		t.Errorf("Validation failed to catch %d injection payloads", len(failures))
	}

	// Test with no validation (should fail all)
	failures = helper.TestInjection(func(input string) error {
		return nil // No validation
	})

	if len(failures) != len(helper.InjectionPayloads()) {
		t.Error("Expected all payloads to fail when no validation is present")
	}
}

func TestPenetrationTestHelper_TestXSS(t *testing.T) {
	helper := NewPenetrationTestHelper()
	validator := NewInputValidator()

	// Test with proper validation (should catch all)
	failures := helper.TestXSS(func(input string) error {
		return validator.ValidateNoXSS(input)
	})

	if len(failures) > 0 {
		t.Errorf("Validation failed to catch %d XSS payloads", len(failures))
	}
}

func TestPenetrationTestHelper_TestPathTraversal(t *testing.T) {
	helper := NewPenetrationTestHelper()
	validator := NewInputValidator()

	// Test with proper validation (should catch all)
	failures := helper.TestPathTraversal(func(input string) error {
		return validator.ValidateNoPathTraversal(input)
	})

	if len(failures) > 0 {
		t.Errorf("Validation failed to catch %d path traversal payloads", len(failures))
	}
}

func TestRateLimitTester(t *testing.T) {
	tester := NewRateLimitTester()

	count := 0
	limited, successCount := tester.TestRateLimit(func() error {
		count++
		// Simulate rate limiting after 5 requests
		if count > 5 {
			return fmt.Errorf("rate limited")
		}
		return nil
	}, 100, 1*time.Second)

	if !limited {
		t.Error("Expected rate limiting to be detected")
	}

	if successCount <= 0 || successCount > 6 {
		t.Errorf("Success count = %d, expected 1-6", successCount)
	}
}

func TestGenerateSecureToken(t *testing.T) {
	tests := []int{16, 32, 64, 128}

	for _, length := range tests {
		t.Run(fmt.Sprintf("Length_%d", length), func(t *testing.T) {
			token, err := GenerateSecureToken(length)
			if err != nil {
				t.Fatalf("GenerateSecureToken() failed: %v", err)
			}

			if len(token) != length {
				t.Errorf("Token length = %d, want %d", len(token), length)
			}

			// Verify it's not all the same character
			allSame := true
			if len(token) > 1 {
				first := token[0]
				for i := 1; i < len(token); i++ {
					if token[i] != first {
						allSame = false
						break
					}
				}
			}

			if allSame {
				t.Error("Token has no randomness (all same character)")
			}
		})
	}
}

func TestGenerateSecureToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	count := 100

	for i := 0; i < count; i++ {
		token, err := GenerateSecureToken(32)
		if err != nil {
			t.Fatalf("GenerateSecureToken() failed: %v", err)
		}

		if tokens[token] {
			t.Error("Generated duplicate token")
		}
		tokens[token] = true
	}

	if len(tokens) != count {
		t.Errorf("Generated %d unique tokens, want %d", len(tokens), count)
	}
}

func TestValidateCertificate(t *testing.T) {
	// Create a test certificate
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	// Test valid certificate
	vulns := ValidateCertificate(cert)
	if len(vulns) > 0 {
		t.Errorf("Valid certificate reported %d vulnerabilities", len(vulns))
	}

	// Test expired certificate
	expiredTemplate := *template
	expiredTemplate.NotAfter = time.Now().Add(-1 * time.Hour)
	derBytes, _ = x509.CreateCertificate(rand.Reader, &expiredTemplate, &expiredTemplate, &privateKey.PublicKey, privateKey)
	expiredCert, _ := x509.ParseCertificate(derBytes)

	vulns = ValidateCertificate(expiredCert)
	if len(vulns) == 0 {
		t.Error("Expired certificate should report vulnerabilities")
	}

	// Verify it's marked as critical
	foundCritical := false
	for _, vuln := range vulns {
		if vuln.Severity == SeverityCritical {
			foundCritical = true
			break
		}
	}

	if !foundCritical {
		t.Error("Expired certificate should have critical severity")
	}
}

func TestValidateCertificate_WeakKey(t *testing.T) {
	// Create certificate with weak 1024-bit key
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	vulns := ValidateCertificate(cert)

	// Should report weak key
	foundWeakKey := false
	for _, vuln := range vulns {
		if vuln.Type == VulnWeakCrypto && vuln.Severity == SeverityHigh {
			foundWeakKey = true
			break
		}
	}

	if !foundWeakKey {
		t.Error("Weak RSA key should be detected")
	}
}

func TestDefaultPasswordValidator(t *testing.T) {
	validator := DefaultPasswordValidator()

	if validator.MinLength != 12 {
		t.Errorf("MinLength = %d, want 12", validator.MinLength)
	}

	if !validator.RequireUpper {
		t.Error("RequireUpper should be true")
	}

	if !validator.RequireLower {
		t.Error("RequireLower should be true")
	}

	if !validator.RequireDigit {
		t.Error("RequireDigit should be true")
	}

	if !validator.RequireSpecial {
		t.Error("RequireSpecial should be true")
	}
}

func TestVulnerabilityTypes(t *testing.T) {
	// Verify all vulnerability types are defined
	types := []VulnerabilityType{
		VulnInjection,
		VulnPathTraversal,
		VulnWeakCrypto,
		VulnWeakPassword,
		VulnMissingAuth,
		VulnInsecureTransport,
		VulnXSS,
		VulnCSRF,
		VulnRateLimit,
		VulnInfoDisclosure,
	}

	if len(types) != 10 {
		t.Errorf("Expected 10 vulnerability types, got %d", len(types))
	}
}

func TestSeverityLevels(t *testing.T) {
	// Verify all severity levels are defined
	severities := []Severity{
		SeverityCritical,
		SeverityHigh,
		SeverityMedium,
		SeverityLow,
		SeverityInfo,
	}

	if len(severities) != 5 {
		t.Errorf("Expected 5 severity levels, got %d", len(severities))
	}
}
