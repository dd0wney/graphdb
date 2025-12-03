package tls

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("Default config should have TLS disabled")
	}

	if !cfg.AutoGenerate {
		t.Error("Default config should enable auto-generation")
	}

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want TLS 1.2 (%d)", cfg.MinVersion, tls.VersionTLS12)
	}

	if len(cfg.Hosts) == 0 {
		t.Error("Default config should have default hosts")
	}

	if cfg.ClientAuth != tls.NoClientCert {
		t.Error("Default config should not require client certificates")
	}
}

func TestGenerateSelfSignedCert(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Hosts = []string{"localhost", "example.com"}
	cfg.Organization = "Test Org"
	cfg.ValidFor = 24 * time.Hour

	cert, err := GenerateSelfSignedCert(cfg)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() failed: %v", err)
	}

	if len(cert.Certificate) == 0 {
		t.Error("Generated certificate is empty")
	}

	if cert.PrivateKey == nil {
		t.Error("Generated certificate has no private key")
	}
}

func TestGenerateAndSaveCertificate(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "test.crt")
	keyFile := filepath.Join(tempDir, "test.key")

	cfg := DefaultConfig()
	cfg.Hosts = []string{"localhost"}
	cfg.ValidFor = 24 * time.Hour

	err := GenerateAndSaveCertificate(cfg, certFile, keyFile)
	if err != nil {
		t.Fatalf("GenerateAndSaveCertificate() failed: %v", err)
	}

	// Check files were created
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Error("Certificate file was not created")
	}

	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Error("Key file was not created")
	}

	// Check key file has restrictive permissions
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("Failed to stat key file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("Key file permissions = %o, want 0600", perm)
	}

	// Try loading the certificate
	_, err = tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Errorf("Failed to load generated certificate: %v", err)
	}
}

func TestLoadTLSConfig_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	tlsConfig, err := LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("LoadTLSConfig() with disabled TLS failed: %v", err)
	}

	if tlsConfig != nil {
		t.Error("LoadTLSConfig() with disabled TLS should return nil")
	}
}

func TestLoadTLSConfig_AutoGenerate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.AutoGenerate = true
	cfg.Hosts = []string{"localhost"}

	tlsConfig, err := LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("LoadTLSConfig() with auto-generate failed: %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("LoadTLSConfig() returned nil config")
	}

	if len(tlsConfig.Certificates) == 0 {
		t.Error("TLS config has no certificates")
	}

	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want %d", tlsConfig.MinVersion, tls.VersionTLS12)
	}
}

func TestLoadTLSConfig_FromFiles(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "test.crt")
	keyFile := filepath.Join(tempDir, "test.key")

	// Generate certificate files
	genCfg := DefaultConfig()
	genCfg.Hosts = []string{"localhost"}
	err := GenerateAndSaveCertificate(genCfg, certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	// Load TLS config from files
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CertFile = certFile
	cfg.KeyFile = keyFile

	tlsConfig, err := LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("LoadTLSConfig() from files failed: %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("LoadTLSConfig() returned nil config")
	}

	if len(tlsConfig.Certificates) == 0 {
		t.Error("TLS config has no certificates")
	}
}

func TestLoadTLSConfig_WithCA(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "test.crt")
	keyFile := filepath.Join(tempDir, "test.key")
	caFile := filepath.Join(tempDir, "ca.crt")

	// Generate certificate
	genCfg := DefaultConfig()
	genCfg.Hosts = []string{"localhost"}
	err := GenerateAndSaveCertificate(genCfg, certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	// Use the same cert as CA for testing
	certData, _ := os.ReadFile(certFile)
	os.WriteFile(caFile, certData, 0644)

	// Load TLS config with CA
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CertFile = certFile
	cfg.KeyFile = keyFile
	cfg.CAFile = caFile

	tlsConfig, err := LoadTLSConfig(cfg)
	if err != nil {
		t.Fatalf("LoadTLSConfig() with CA failed: %v", err)
	}

	if tlsConfig.ClientCAs == nil {
		t.Error("ClientCAs not loaded")
	}

	if tlsConfig.ClientAuth == tls.NoClientCert {
		t.Error("ClientAuth should be set when CA is provided")
	}
}

func TestLoadTLSConfig_MissingCertificate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.AutoGenerate = false
	cfg.CertFile = ""
	cfg.KeyFile = ""

	_, err := LoadTLSConfig(cfg)
	if err == nil {
		t.Error("LoadTLSConfig() should fail when no certificate is provided and auto-generation is disabled")
	}
}

func TestLoadTLSConfig_InvalidCertFile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CertFile = "/nonexistent/cert.pem"
	cfg.KeyFile = "/nonexistent/key.pem"

	_, err := LoadTLSConfig(cfg)
	if err == nil {
		t.Error("LoadTLSConfig() should fail with invalid certificate file")
	}
}

func TestVerifyCertificate(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "test.crt")
	keyFile := filepath.Join(tempDir, "test.key")

	// Generate valid certificate
	cfg := DefaultConfig()
	cfg.ValidFor = 24 * time.Hour
	err := GenerateAndSaveCertificate(cfg, certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	// Verify certificate
	err = VerifyCertificate(certFile)
	if err != nil {
		t.Errorf("VerifyCertificate() failed: %v", err)
	}
}

func TestVerifyCertificate_Expired(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "expired.crt")
	keyFile := filepath.Join(tempDir, "expired.key")

	// Generate certificate that expires immediately
	cfg := DefaultConfig()
	cfg.ValidFor = -24 * time.Hour // Already expired
	err := GenerateAndSaveCertificate(cfg, certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	// Verify should fail
	err = VerifyCertificate(certFile)
	if err == nil {
		t.Error("VerifyCertificate() should fail for expired certificate")
	}
}

func TestGetCertificateInfo(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "test.crt")
	keyFile := filepath.Join(tempDir, "test.key")

	// Generate certificate
	cfg := DefaultConfig()
	cfg.Hosts = []string{"localhost", "example.com"}
	cfg.Organization = "Test Org"
	cfg.ValidFor = 365 * 24 * time.Hour
	err := GenerateAndSaveCertificate(cfg, certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to generate certificate: %v", err)
	}

	// Get certificate info
	info, err := GetCertificateInfo(certFile)
	if err != nil {
		t.Fatalf("GetCertificateInfo() failed: %v", err)
	}

	if info.Subject == "" {
		t.Error("Subject is empty")
	}

	if len(info.DNSNames) != 2 {
		t.Errorf("DNSNames count = %d, want 2", len(info.DNSNames))
	}

	if info.IsExpired() {
		t.Error("Certificate should not be expired")
	}

	if info.ExpiresIn() <= 0 {
		t.Error("ExpiresIn should be positive")
	}
}

func TestSecureCipherSuites(t *testing.T) {
	suites := SecureCipherSuites()

	if len(suites) == 0 {
		t.Error("SecureCipherSuites() returned empty list")
	}

	// Check that TLS 1.3 suites are included
	hasTLS13 := false
	for _, suite := range suites {
		if suite == tls.TLS_AES_128_GCM_SHA256 || suite == tls.TLS_AES_256_GCM_SHA384 {
			hasTLS13 = true
			break
		}
	}

	if !hasTLS13 {
		t.Error("SecureCipherSuites() should include TLS 1.3 cipher suites")
	}
}

func TestLoadCAPool(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "ca.crt")
	keyFile := filepath.Join(tempDir, "ca.key")

	// Generate CA certificate
	cfg := DefaultConfig()
	err := GenerateAndSaveCertificate(cfg, certFile, keyFile)
	if err != nil {
		t.Fatalf("Failed to generate CA certificate: %v", err)
	}

	// Load CA pool
	pool, err := LoadCAPool(certFile)
	if err != nil {
		t.Fatalf("LoadCAPool() failed: %v", err)
	}

	if pool == nil {
		t.Error("LoadCAPool() returned nil pool")
	}
}

func TestLoadCAPool_InvalidFile(t *testing.T) {
	_, err := LoadCAPool("/nonexistent/ca.pem")
	if err == nil {
		t.Error("LoadCAPool() should fail with invalid file")
	}
}

func TestLoadCAPool_InvalidPEM(t *testing.T) {
	tempDir := t.TempDir()
	invalidFile := filepath.Join(tempDir, "invalid.pem")

	// Write invalid PEM data
	os.WriteFile(invalidFile, []byte("not a valid PEM"), 0644)

	_, err := LoadCAPool(invalidFile)
	if err == nil {
		t.Error("LoadCAPool() should fail with invalid PEM data")
	}
}

func TestCertificateInfo_IsExpired(t *testing.T) {
	tests := []struct {
		name      string
		notAfter  time.Time
		want      bool
	}{
		{
			name:     "Future expiration",
			notAfter: time.Now().Add(24 * time.Hour),
			want:     false,
		},
		{
			name:     "Past expiration",
			notAfter: time.Now().Add(-24 * time.Hour),
			want:     true,
		},
		{
			name:     "Just barely not expired",
			notAfter: time.Now().Add(1 * time.Second),
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &CertificateInfo{
				NotAfter: tt.notAfter,
			}

			got := info.IsExpired()
			if got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCertificateInfo_ExpiresIn(t *testing.T) {
	info := &CertificateInfo{
		NotAfter: time.Now().Add(24 * time.Hour),
	}

	expiresIn := info.ExpiresIn()
	if expiresIn <= 23*time.Hour || expiresIn > 25*time.Hour {
		t.Errorf("ExpiresIn() = %v, want approximately 24h", expiresIn)
	}
}

func TestTLSConfig_ClientAuth(t *testing.T) {
	tests := []struct {
		name       string
		clientAuth tls.ClientAuthType
	}{
		{"NoClientCert", tls.NoClientCert},
		{"RequestClientCert", tls.RequestClientCert},
		{"RequireAnyClientCert", tls.RequireAnyClientCert},
		{"VerifyClientCertIfGiven", tls.VerifyClientCertIfGiven},
		{"RequireAndVerifyClientCert", tls.RequireAndVerifyClientCert},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Enabled = true
			cfg.AutoGenerate = true
			cfg.ClientAuth = tt.clientAuth

			tlsConfig, err := LoadTLSConfig(cfg)
			if err != nil {
				t.Fatalf("LoadTLSConfig() failed: %v", err)
			}

			if tlsConfig.ClientAuth != tt.clientAuth {
				t.Errorf("ClientAuth = %v, want %v", tlsConfig.ClientAuth, tt.clientAuth)
			}
		})
	}
}

func TestTLSConfig_MinVersion(t *testing.T) {
	tests := []struct {
		name       string
		minVersion uint16
	}{
		{"TLS 1.2", tls.VersionTLS12},
		{"TLS 1.3", tls.VersionTLS13},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Enabled = true
			cfg.AutoGenerate = true
			cfg.MinVersion = tt.minVersion

			tlsConfig, err := LoadTLSConfig(cfg)
			if err != nil {
				t.Fatalf("LoadTLSConfig() failed: %v", err)
			}

			if tlsConfig.MinVersion != tt.minVersion {
				t.Errorf("MinVersion = %v, want %v", tlsConfig.MinVersion, tt.minVersion)
			}
		})
	}
}
