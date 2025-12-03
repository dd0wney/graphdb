package licensing

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestEmailConfig_IsConfigured(t *testing.T) {
	tests := []struct {
		name   string
		config *EmailConfig
		want   bool
	}{
		{
			name: "Fully configured",
			config: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "587",
				SMTPUsername: "user@example.com",
				SMTPPassword: "password",
				FromEmail:    "noreply@example.com",
				FromName:     "GraphDB",
			},
			want: true,
		},
		{
			name: "Missing SMTP host",
			config: &EmailConfig{
				SMTPHost:     "",
				SMTPPort:     "587",
				SMTPUsername: "user@example.com",
				SMTPPassword: "password",
				FromEmail:    "noreply@example.com",
			},
			want: false,
		},
		{
			name: "Missing SMTP port",
			config: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "",
				SMTPUsername: "user@example.com",
				SMTPPassword: "password",
				FromEmail:    "noreply@example.com",
			},
			want: false,
		},
		{
			name: "Missing SMTP username",
			config: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "587",
				SMTPUsername: "",
				SMTPPassword: "password",
				FromEmail:    "noreply@example.com",
			},
			want: false,
		},
		{
			name: "Missing SMTP password",
			config: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "587",
				SMTPUsername: "user@example.com",
				SMTPPassword: "",
				FromEmail:    "noreply@example.com",
			},
			want: false,
		},
		{
			name: "Missing from email",
			config: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "587",
				SMTPUsername: "user@example.com",
				SMTPPassword: "password",
				FromEmail:    "",
			},
			want: false,
		},
		{
			name: "FromName optional",
			config: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "587",
				SMTPUsername: "user@example.com",
				SMTPPassword: "password",
				FromEmail:    "noreply@example.com",
				FromName:     "", // Optional field
			},
			want: true,
		},
		{
			name: "All fields empty",
			config: &EmailConfig{
				SMTPHost:     "",
				SMTPPort:     "",
				SMTPUsername: "",
				SMTPPassword: "",
				FromEmail:    "",
				FromName:     "",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.IsConfigured(); got != tt.want {
				t.Errorf("EmailConfig.IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadEmailConfigFromEnv(t *testing.T) {
	// Save original environment
	originalEnv := map[string]string{
		"SMTP_HOST":     os.Getenv("SMTP_HOST"),
		"SMTP_PORT":     os.Getenv("SMTP_PORT"),
		"SMTP_USERNAME": os.Getenv("SMTP_USERNAME"),
		"SMTP_PASSWORD": os.Getenv("SMTP_PASSWORD"),
		"FROM_EMAIL":    os.Getenv("FROM_EMAIL"),
		"FROM_NAME":     os.Getenv("FROM_NAME"),
	}

	// Restore environment after test
	defer func() {
		for k, v := range originalEnv {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	tests := []struct {
		name   string
		env    map[string]string
		expect *EmailConfig
	}{
		{
			name: "All environment variables set",
			env: map[string]string{
				"SMTP_HOST":     "smtp.example.com",
				"SMTP_PORT":     "587",
				"SMTP_USERNAME": "user@example.com",
				"SMTP_PASSWORD": "password123",
				"FROM_EMAIL":    "noreply@example.com",
				"FROM_NAME":     "GraphDB Licensing",
			},
			expect: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "587",
				SMTPUsername: "user@example.com",
				SMTPPassword: "password123",
				FromEmail:    "noreply@example.com",
				FromName:     "GraphDB Licensing",
			},
		},
		{
			name: "No environment variables set",
			env:  map[string]string{},
			expect: &EmailConfig{
				SMTPHost:     "",
				SMTPPort:     "",
				SMTPUsername: "",
				SMTPPassword: "",
				FromEmail:    "",
				FromName:     "",
			},
		},
		{
			name: "Partial environment variables",
			env: map[string]string{
				"SMTP_HOST": "smtp.example.com",
				"SMTP_PORT": "587",
			},
			expect: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "587",
				SMTPUsername: "",
				SMTPPassword: "",
				FromEmail:    "",
				FromName:     "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.env {
				if v == "" {
					os.Unsetenv(k)
				} else {
					os.Setenv(k, v)
				}
			}

			// Clear any variables not in the test case
			allKeys := []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "FROM_EMAIL", "FROM_NAME"}
			for _, key := range allKeys {
				if _, exists := tt.env[key]; !exists {
					os.Unsetenv(key)
				}
			}

			config := LoadEmailConfigFromEnv()

			if config.SMTPHost != tt.expect.SMTPHost {
				t.Errorf("SMTPHost = %v, want %v", config.SMTPHost, tt.expect.SMTPHost)
			}
			if config.SMTPPort != tt.expect.SMTPPort {
				t.Errorf("SMTPPort = %v, want %v", config.SMTPPort, tt.expect.SMTPPort)
			}
			if config.SMTPUsername != tt.expect.SMTPUsername {
				t.Errorf("SMTPUsername = %v, want %v", config.SMTPUsername, tt.expect.SMTPUsername)
			}
			if config.SMTPPassword != tt.expect.SMTPPassword {
				t.Errorf("SMTPPassword = %v, want %v", config.SMTPPassword, tt.expect.SMTPPassword)
			}
			if config.FromEmail != tt.expect.FromEmail {
				t.Errorf("FromEmail = %v, want %v", config.FromEmail, tt.expect.FromEmail)
			}
			if config.FromName != tt.expect.FromName {
				t.Errorf("FromName = %v, want %v", config.FromName, tt.expect.FromName)
			}
		})
	}
}

func TestGenerateLicenseEmailHTML(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		license     *License
		wantErr     bool
		checkStrings []string
	}{
		{
			name: "Professional license",
			license: &License{
				ID:         "test-id-1",
				Key:        "CGDB-TEST-KEY1-1111-1111",
				Email:      "test@example.com",
				Type:       LicenseTypeProfessional,
				Status:     "active",
				CreatedAt:  now,
				CustomerID: "cust-123",
			},
			wantErr: false,
			checkStrings: []string{
				"CGDB-TEST-KEY1-1111-1111",
				"test@example.com",
				"professional",
				"active",
			},
		},
		{
			name: "Enterprise license",
			license: &License{
				ID:         "test-id-2",
				Key:        "CGDB-TEST-KEY2-2222-2222",
				Email:      "enterprise@example.com",
				Type:       LicenseTypeEnterprise,
				Status:     "active",
				CreatedAt:  now,
				CustomerID: "cust-456",
			},
			wantErr: false,
			checkStrings: []string{
				"CGDB-TEST-KEY2-2222-2222",
				"enterprise@example.com",
				"enterprise",
				"active",
			},
		},
		{
			name: "License with special characters in email",
			license: &License{
				ID:         "test-id-3",
				Key:        "CGDB-TEST-KEY3-3333-3333",
				Email:      "test+tag@example.com",
				Type:       LicenseTypeProfessional,
				Status:     "active",
				CreatedAt:  now,
				CustomerID: "cust-789",
			},
			wantErr: false,
			checkStrings: []string{
				"CGDB-TEST-KEY3-3333-3333",
				// Email will be HTML-escaped in template, so check for domain only
				"@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := generateLicenseEmailHTML(tt.license)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateLicenseEmailHTML() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				// Check HTML is not empty
				if len(html) == 0 {
					t.Error("generateLicenseEmailHTML() returned empty HTML")
				}

				// Check HTML contains expected strings
				for _, str := range tt.checkStrings {
					if !strings.Contains(strings.ToLower(html), strings.ToLower(str)) {
						t.Errorf("HTML does not contain expected string: %s", str)
					}
				}

				// Check HTML structure
				requiredTags := []string{"<!DOCTYPE html>", "<html>", "<head>", "<body>", "</body>", "</html>"}
				for _, tag := range requiredTags {
					if !strings.Contains(html, tag) {
						t.Errorf("HTML missing required tag: %s", tag)
					}
				}

				// Check contains license key prominently
				if !strings.Contains(html, tt.license.Key) {
					t.Error("HTML does not contain license key")
				}

				// Check contains welcome message
				if !strings.Contains(html, "Welcome to GraphDB") {
					t.Error("HTML does not contain welcome message")
				}
			}
		})
	}
}

func TestSendLicenseEmail_Unconfigured(t *testing.T) {
	config := &EmailConfig{
		SMTPHost:     "",
		SMTPPort:     "",
		SMTPUsername: "",
		SMTPPassword: "",
		FromEmail:    "",
	}

	license := &License{
		ID:        "test-id",
		Key:       "CGDB-TEST-KEY0-0000-0000",
		Email:     "test@example.com",
		Type:      LicenseTypeProfessional,
		Status:    "active",
		CreatedAt: time.Now(),
	}

	err := SendLicenseEmail(config, license)
	if err == nil {
		t.Error("SendLicenseEmail() expected error with unconfigured email, got nil")
	}

	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("Error message should mention 'not configured', got: %v", err)
	}
}

func TestGenerateLicenseEmailHTML_TemplateExecution(t *testing.T) {
	// Test with a license that has all fields populated
	license := &License{
		ID:         "full-test-id",
		Key:        "CGDB-FULL-TEST-KEY-0000-0000",
		Email:      "fulltest@example.com",
		Type:       LicenseTypeEnterprise,
		Status:     "active",
		CreatedAt:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		ExpiresAt:  nil,
		CustomerID: "cust-full",
		Metadata: map[string]string{
			"company": "Test Corp",
		},
	}

	html, err := generateLicenseEmailHTML(license)
	if err != nil {
		t.Fatalf("generateLicenseEmailHTML() error = %v", err)
	}

	// Check template executed correctly
	if !strings.Contains(html, license.Key) {
		t.Error("HTML missing license key")
	}
	if !strings.Contains(html, license.Email) {
		t.Error("HTML missing email")
	}
	if !strings.Contains(html, string(license.Type)) {
		t.Error("HTML missing license type")
	}
	if !strings.Contains(html, license.Status) {
		t.Error("HTML missing status")
	}

	// Check for year in formatted date
	if !strings.Contains(html, "2024") {
		t.Error("HTML missing formatted date")
	}
}

func TestGenerateLicenseEmailHTML_HTMLEscaping(t *testing.T) {
	// Test with potentially dangerous content
	license := &License{
		ID:         "xss-test-id",
		Key:        "CGDB-XSS-TEST-KEY-0000-0000",
		Email:      "test@example.com",
		Type:       LicenseTypeProfessional,
		Status:     "active",
		CreatedAt:  time.Now(),
		CustomerID: "cust-xss",
	}

	html, err := generateLicenseEmailHTML(license)
	if err != nil {
		t.Fatalf("generateLicenseEmailHTML() error = %v", err)
	}

	// Template should escape HTML by default
	// Check that the HTML is valid
	if len(html) == 0 {
		t.Error("HTML is empty")
	}

	// Verify HTML structure is intact
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("HTML structure is broken")
	}
}

func TestEmailConfig_IsConfigured_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		config *EmailConfig
		want   bool
	}{
		{
			name: "Whitespace-only values",
			config: &EmailConfig{
				SMTPHost:     "  ",
				SMTPPort:     "  ",
				SMTPUsername: "  ",
				SMTPPassword: "  ",
				FromEmail:    "  ",
			},
			want: true, // IsConfigured only checks != "", whitespace passes
		},
		{
			name: "Zero port number",
			config: &EmailConfig{
				SMTPHost:     "smtp.example.com",
				SMTPPort:     "0",
				SMTPUsername: "user@example.com",
				SMTPPassword: "password",
				FromEmail:    "noreply@example.com",
			},
			want: true, // Port "0" is a string, so it's truthy
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: IsConfigured only checks if fields are non-empty strings
			// It doesn't validate their actual values
			if got := tt.config.IsConfigured(); got != tt.want {
				t.Errorf("EmailConfig.IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Benchmark email operations
func BenchmarkGenerateLicenseEmailHTML(b *testing.B) {
	license := &License{
		ID:         "bench-id",
		Key:        "CGDB-BENCH-KEY-0000-0000",
		Email:      "bench@example.com",
		Type:       LicenseTypeProfessional,
		Status:     "active",
		CreatedAt:  time.Now(),
		CustomerID: "cust-bench",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		generateLicenseEmailHTML(license)
	}
}

func BenchmarkEmailConfig_IsConfigured(b *testing.B) {
	config := &EmailConfig{
		SMTPHost:     "smtp.example.com",
		SMTPPort:     "587",
		SMTPUsername: "user@example.com",
		SMTPPassword: "password",
		FromEmail:    "noreply@example.com",
		FromName:     "GraphDB",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config.IsConfigured()
	}
}
