package tls

import (
	"crypto/tls"
	"time"
)

// Config holds TLS configuration options
type Config struct {
	Enabled  bool   // Enable TLS
	CertFile string // Path to certificate file
	KeyFile  string // Path to private key file
	CAFile   string // Path to CA certificate (for client verification)

	// Certificate generation options (if CertFile/KeyFile not provided)
	AutoGenerate bool          // Auto-generate self-signed certificates
	Hosts        []string      // Hostnames/IPs for generated certificate
	Organization string        // Organization name for generated certificate
	ValidFor     time.Duration // Certificate validity duration (default 1 year)

	// TLS security settings
	MinVersion         uint16               // Minimum TLS version (default TLS 1.2)
	CipherSuites       []uint16             // Allowed cipher suites (default secure subset)
	ClientAuth         tls.ClientAuthType   // Client certificate requirement
	InsecureSkipVerify bool                 // Skip certificate verification (NOT for production)
}

// DefaultConfig returns a secure TLS configuration with recommended defaults
func DefaultConfig() *Config {
	return &Config{
		Enabled:      false,
		AutoGenerate: true,
		Hosts:        []string{"localhost", "127.0.0.1"},
		Organization: "GraphDB",
		ValidFor:     365 * 24 * time.Hour, // 1 year
		MinVersion:   tls.VersionTLS12,
		CipherSuites: SecureCipherSuites(),
		ClientAuth:   tls.NoClientCert,
	}
}

// CertificateInfo holds certificate metadata
type CertificateInfo struct {
	Subject      string
	Issuer       string
	SerialNumber string
	NotBefore    time.Time
	NotAfter     time.Time
	DNSNames     []string
	IsCA         bool
}

// IsExpired checks if the certificate has expired
func (ci *CertificateInfo) IsExpired() bool {
	return time.Now().After(ci.NotAfter)
}

// ExpiresIn returns the time until certificate expiration
func (ci *CertificateInfo) ExpiresIn() time.Duration {
	return time.Until(ci.NotAfter)
}

// SecureCipherSuites returns a list of secure cipher suites
// Based on OWASP and Mozilla recommendations (2024)
func SecureCipherSuites() []uint16 {
	return []uint16{
		// TLS 1.3 cipher suites (preferred)
		tls.TLS_AES_128_GCM_SHA256,
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,

		// TLS 1.2 cipher suites (fallback)
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	}
}
