package security

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"
)

// TLSValidator validates TLS configuration
type TLSValidator struct{}

// NewTLSValidator creates a new TLS validator
func NewTLSValidator() *TLSValidator {
	return &TLSValidator{}
}

// ValidateTLSConfig checks TLS configuration security
func (t *TLSValidator) ValidateTLSConfig(config *tls.Config) []Vulnerability {
	var vulns []Vulnerability

	// Check minimum TLS version
	if config.MinVersion < tls.VersionTLS12 {
		vulns = append(vulns, Vulnerability{
			Type:        VulnInsecureTransport,
			Severity:    SeverityHigh,
			Description: "TLS version below 1.2 is insecure",
			Remediation: "Set MinVersion to tls.VersionTLS12 or higher",
		})
	}

	// Check for insecure cipher suites
	vulns = append(vulns, t.checkInsecureCiphers(config)...)

	// Check if InsecureSkipVerify is enabled
	if config.InsecureSkipVerify {
		vulns = append(vulns, Vulnerability{
			Type:        VulnInsecureTransport,
			Severity:    SeverityCritical,
			Description: "Certificate verification is disabled",
			Remediation: "Set InsecureSkipVerify to false",
		})
	}

	return vulns
}

// checkInsecureCiphers checks for insecure cipher suites
func (t *TLSValidator) checkInsecureCiphers(config *tls.Config) []Vulnerability {
	var vulns []Vulnerability

	insecureCiphers := []uint16{
		tls.TLS_RSA_WITH_RC4_128_SHA,
		tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
		tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_RSA_WITH_AES_256_CBC_SHA,
	}

	for _, cipher := range config.CipherSuites {
		for _, insecure := range insecureCiphers {
			if cipher == insecure {
				vulns = append(vulns, Vulnerability{
					Type:        VulnWeakCrypto,
					Severity:    SeverityHigh,
					Description: fmt.Sprintf("Insecure cipher suite enabled: %d", cipher),
					Remediation: "Remove insecure cipher suites",
				})
			}
		}
	}

	return vulns
}

// ValidateTLSConnection tests a TLS connection
func (t *TLSValidator) ValidateTLSConnection(address string, timeout time.Duration) error {
	dialer := &net.Dialer{
		Timeout: timeout,
	}

	conn, err := tls.DialWithDialer(dialer, "tcp", address, &tls.Config{
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return fmt.Errorf("TLS connection failed: %w", err)
	}
	defer conn.Close()

	// Verify certificate chain
	err = conn.VerifyHostname(strings.Split(address, ":")[0])
	if err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	return nil
}

// ValidateCertificate validates an X.509 certificate
func ValidateCertificate(cert *x509.Certificate) []Vulnerability {
	var vulns []Vulnerability

	// Check expiration
	vulns = append(vulns, checkCertificateExpiration(cert)...)

	// Check key size
	vulns = append(vulns, checkCertificateKeySize(cert)...)

	return vulns
}

// checkCertificateExpiration checks certificate expiration status
func checkCertificateExpiration(cert *x509.Certificate) []Vulnerability {
	var vulns []Vulnerability

	if time.Now().After(cert.NotAfter) {
		vulns = append(vulns, Vulnerability{
			Type:        VulnWeakCrypto,
			Severity:    SeverityCritical,
			Description: "Certificate has expired",
			Remediation: "Renew the certificate",
		})
	}

	// Check if expiring soon (30 days)
	if time.Now().Add(30 * 24 * time.Hour).After(cert.NotAfter) {
		vulns = append(vulns, Vulnerability{
			Type:        VulnWeakCrypto,
			Severity:    SeverityMedium,
			Description: "Certificate expires within 30 days",
			Remediation: "Renew the certificate soon",
		})
	}

	return vulns
}

// checkCertificateKeySize checks certificate key size
func checkCertificateKeySize(cert *x509.Certificate) []Vulnerability {
	var vulns []Vulnerability

	if cert.PublicKeyAlgorithm == x509.RSA {
		if rsaKey, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			if rsaKey.N.BitLen() < 2048 {
				vulns = append(vulns, Vulnerability{
					Type:        VulnWeakCrypto,
					Severity:    SeverityHigh,
					Description: fmt.Sprintf("RSA key size %d is below recommended 2048", rsaKey.N.BitLen()),
					Remediation: "Use at least 2048-bit RSA keys",
				})
			}
		}
	}

	return vulns
}
