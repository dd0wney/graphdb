package licensing

import (
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
	tests := []struct {
		name  string
		key   string
		valid bool
	}{
		{"Valid key", "CGDB-ABCD-EFGH-IJKL-MNOP", true},
		{"Too short", "CGDB-ABC", false},
		{"Wrong prefix", "ABCD-EFGH-IJKL-MNOP-QRST", false},
		{"Empty", "", false},
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

// BenchmarkGenerateLicenseKey benchmarks key generation
func BenchmarkGenerateLicenseKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateLicenseKey(LicenseTypeProfessional, "test@example.com")
	}
}
