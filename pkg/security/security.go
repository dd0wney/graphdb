package security

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"math/big"
)

// SecurityScanner performs security vulnerability scanning
type SecurityScanner struct {
	vulnerabilities []Vulnerability
}

// NewSecurityScanner creates a new security scanner
func NewSecurityScanner() *SecurityScanner {
	return &SecurityScanner{
		vulnerabilities: make([]Vulnerability, 0),
	}
}

// GetVulnerabilities returns all detected vulnerabilities
func (s *SecurityScanner) GetVulnerabilities() []Vulnerability {
	return s.vulnerabilities
}

// GetVulnerabilitiesBySeverity returns vulnerabilities of a specific severity
func (s *SecurityScanner) GetVulnerabilitiesBySeverity(severity Severity) []Vulnerability {
	var result []Vulnerability
	for _, vuln := range s.vulnerabilities {
		if vuln.Severity == severity {
			result = append(result, vuln)
		}
	}
	return result
}

// AddVulnerability adds a vulnerability to the scanner
func (s *SecurityScanner) addVulnerability(vuln Vulnerability) {
	s.vulnerabilities = append(s.vulnerabilities, vuln)
}

// CryptoValidator validates cryptographic implementations
type CryptoValidator struct{}

// NewCryptoValidator creates a new crypto validator
func NewCryptoValidator() *CryptoValidator {
	return &CryptoValidator{}
}

// ValidateRSAKeySize checks RSA key size
func (c *CryptoValidator) ValidateRSAKeySize(key *rsa.PrivateKey) error {
	minSize := 2048
	if key.N.BitLen() < minSize {
		return fmt.Errorf("RSA key size %d is below minimum %d", key.N.BitLen(), minSize)
	}
	return nil
}

// ValidateRandomness tests random number generation
func (c *CryptoValidator) ValidateRandomness(sampleSize int) error {
	bytes := make([]byte, sampleSize)
	_, err := rand.Read(bytes)
	if err != nil {
		return fmt.Errorf("random generation failed: %w", err)
	}

	// Check for all zeros (weak randomness indicator)
	allZero := true
	for _, b := range bytes {
		if b != 0 {
			allZero = false
			break
		}
	}

	if allZero {
		return fmt.Errorf("random generation produced all zeros")
	}

	return nil
}

// GenerateSecureToken generates a cryptographically secure random token
func GenerateSecureToken(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)

	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}

	return string(result), nil
}
