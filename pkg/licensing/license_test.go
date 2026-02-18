package licensing

import (
	"strings"
	"testing"
)

func TestGenerateLicenseKey(t *testing.T) {
	tests := []struct {
		name        string
		licenseType LicenseType
		email       string
	}{
		{"Professional", LicenseTypeProfessional, "test@example.com"},
		{"Enterprise", LicenseTypeEnterprise, "user@company.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := GenerateLicenseKey(tt.licenseType, tt.email)
			if err != nil {
				t.Fatalf("GenerateLicenseKey() error = %v", err)
			}

			if !ValidateLicenseKey(key) {
				t.Errorf("Generated key failed validation: %s", key)
			}

			// Check format
			if len(key) < 24 {
				t.Errorf("Key too short: %s", key)
			}

			if key[:4] != "CGDB" {
				t.Errorf("Key doesn't start with CGDB: %s", key)
			}
		})
	}
}

func TestValidateLicenseKey(t *testing.T) {
	// First generate a valid key to get a correct checksum
	validKey, _ := GenerateLicenseKey(LicenseTypeProfessional, "test@example.com")

	tests := []struct {
		name  string
		key   string
		valid bool
	}{
		// Legacy format (5 parts, no checksum)
		{"Legacy valid key", "CGDB-ABCD-EFGH-IJKL-MNOP", true},
		{"Too short", "CGDB-ABC", false},
		{"Wrong prefix", "ABCD-EFGH-IJKL-MNOP-QRST", false},
		{"Empty", "", false},

		// New format with checksum (6 parts)
		{"Generated key with checksum", validKey, true},
		{"Invalid checksum", "CGDB-ABCD-EFGH-IJKL-MNOP-ZZ", false},
		{"Wrong checksum hex", "CGDB-ABCD-EFGH-IJKL-MNOP-00", false},
		{"Checksum case insensitive lowercase", "CGDB-ABCD-EFGH-IJKL-MNOP-" + calculateLicenseChecksum("CGDB-ABCD-EFGH-IJKL-MNOP"), true},

		// Edge cases
		{"Too many parts", "CGDB-ABCD-EFGH-IJKL-MNOP-AA-BB", false},
		{"Four parts only", "CGDB-ABCD-EFGH-IJKL", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateLicenseKey(tt.key)
			if result != tt.valid {
				t.Errorf("ValidateLicenseKey(%s) = %v, want %v", tt.key, result, tt.valid)
			}
		})
	}
}

func TestLicenseIsExpired(t *testing.T) {
	tests := []struct {
		name    string
		license *License
		want    bool
	}{
		{
			name: "No expiration",
			license: &License{
				Status:    "active",
				ExpiresAt: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.license.IsExpired(); got != tt.want {
				t.Errorf("License.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLicenseIsActive(t *testing.T) {
	tests := []struct {
		name    string
		license *License
		want    bool
	}{
		{
			name: "Active license",
			license: &License{
				Status:    "active",
				ExpiresAt: nil,
			},
			want: true,
		},
		{
			name: "Cancelled license",
			license: &License{
				Status:    "cancelled",
				ExpiresAt: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.license.IsActive(); got != tt.want {
				t.Errorf("License.IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLicenseKeyChecksum(t *testing.T) {
	t.Run("Generated keys have 6 parts with checksum", func(t *testing.T) {
		key, err := GenerateLicenseKey(LicenseTypeProfessional, "test@example.com")
		if err != nil {
			t.Fatalf("GenerateLicenseKey() error = %v", err)
		}

		parts := strings.Split(key, "-")
		if len(parts) != 6 {
			t.Errorf("Expected 6 parts (with checksum), got %d: %s", len(parts), key)
		}

		// Verify checksum is 2 characters
		checksum := parts[5]
		if len(checksum) != 2 {
			t.Errorf("Checksum should be 2 characters, got %d: %s", len(checksum), checksum)
		}
	})

	t.Run("Checksum is deterministic", func(t *testing.T) {
		keyBase := "CGDB-ABCD-EFGH-IJKL-MNOP"
		checksum1 := calculateLicenseChecksum(keyBase)
		checksum2 := calculateLicenseChecksum(keyBase)

		if checksum1 != checksum2 {
			t.Errorf("Checksum not deterministic: %s != %s", checksum1, checksum2)
		}
	})

	t.Run("Different keys have different checksums", func(t *testing.T) {
		checksum1 := calculateLicenseChecksum("CGDB-AAAA-BBBB-CCCC-DDDD")
		checksum2 := calculateLicenseChecksum("CGDB-EEEE-FFFF-GGGG-HHHH")

		if checksum1 == checksum2 {
			t.Errorf("Different keys should have different checksums: both got %s", checksum1)
		}
	})

	t.Run("Checksum validation is case insensitive", func(t *testing.T) {
		keyBase := "CGDB-TEST-KEYS-WORK-WELL"
		checksum := calculateLicenseChecksum(keyBase)

		// Test uppercase
		keyUpper := keyBase + "-" + strings.ToUpper(checksum)
		if !ValidateLicenseKey(keyUpper) {
			t.Errorf("Uppercase checksum should be valid: %s", keyUpper)
		}

		// Test lowercase
		keyLower := keyBase + "-" + strings.ToLower(checksum)
		if !ValidateLicenseKey(keyLower) {
			t.Errorf("Lowercase checksum should be valid: %s", keyLower)
		}
	})
}

// BenchmarkGenerateLicenseKey benchmarks key generation
func BenchmarkGenerateLicenseKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateLicenseKey(LicenseTypeProfessional, "test@example.com")
	}
}
