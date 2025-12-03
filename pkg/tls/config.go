package tls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// LoadTLSConfig loads or generates TLS configuration
func LoadTLSConfig(cfg *Config) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	var cert tls.Certificate
	var err error

	// Load existing certificate or generate new one
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		// Load from files
		cert, err = tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
		}
	} else if cfg.AutoGenerate {
		// Generate self-signed certificate
		cert, err = GenerateSelfSignedCert(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
		}
	} else {
		return nil, fmt.Errorf("TLS enabled but no certificate provided and auto-generation disabled")
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		MinVersion:         cfg.MinVersion,
		CipherSuites:       cfg.CipherSuites,
		ClientAuth:         cfg.ClientAuth,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	// Load CA certificate for client verification if provided
	if cfg.CAFile != "" {
		certPool, err := LoadCAPool(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load CA certificate: %w", err)
		}
		tlsConfig.ClientCAs = certPool
		if cfg.ClientAuth == tls.NoClientCert {
			// If CA is provided but client auth not set, default to VerifyClientCertIfGiven
			tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
		}
	}

	return tlsConfig, nil
}

// LoadCAPool loads a CA certificate pool from a file
func LoadCAPool(caFile string) (*x509.CertPool, error) {
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return certPool, nil
}

// VerifyCertificate verifies a certificate file
func VerifyCertificate(certFile string) error {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Check expiration
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate is not yet valid")
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate has expired")
	}

	return nil
}

// GetCertificateInfo returns information about a certificate
func GetCertificateInfo(certFile string) (*CertificateInfo, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &CertificateInfo{
		Subject:      cert.Subject.String(),
		Issuer:       cert.Issuer.String(),
		SerialNumber: cert.SerialNumber.String(),
		NotBefore:    cert.NotBefore,
		NotAfter:     cert.NotAfter,
		DNSNames:     cert.DNSNames,
		IsCA:         cert.IsCA,
	}, nil
}
