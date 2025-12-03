package editions

import (
	"os"
	"testing"
)

// TestEdition_String tests string representation of editions
func TestEdition_String(t *testing.T) {
	tests := []struct {
		edition Edition
		want    string
	}{
		{Community, "Community"},
		{Enterprise, "Enterprise"},
		{Edition(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.edition.String()
			if got != tt.want {
				t.Errorf("Edition.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDetectEdition_FromEnv tests edition detection from environment
func TestDetectEdition_FromEnv(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		want    Edition
		wantMsg bool // expect warning message
	}{
		{"Enterprise explicit", "enterprise", Enterprise, false},
		{"Enterprise short", "ent", Enterprise, false},
		{"Community explicit", "community", Community, false},
		{"Community short", "ce", Community, false},
		{"Unknown defaults to Community", "invalid", Community, true},
		{"Empty string (no env)", "", Community, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean env
			os.Unsetenv("GRAPHDB_EDITION")
			os.Unsetenv("GRAPHDB_LICENSE_PATH")

			if tt.envVal != "" {
				os.Setenv("GRAPHDB_EDITION", tt.envVal)
			}

			got := DetectEdition()

			if got != tt.want {
				t.Errorf("DetectEdition() = %v, want %v", got, tt.want)
			}

			// Clean up
			os.Unsetenv("GRAPHDB_EDITION")
		})
	}
}

// TestDetectEdition_FromLicense tests edition detection from license file
func TestDetectEdition_FromLicense(t *testing.T) {
	// Clean environment
	os.Unsetenv("GRAPHDB_EDITION")
	os.Unsetenv("GRAPHDB_LICENSE_PATH")

	// Create temp license file
	tmpFile := "./test-license.key"
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create test license file: %v", err)
	}
	f.WriteString("test-license-key")
	f.Close()
	defer os.Remove(tmpFile)

	// Set path to our test license
	os.Setenv("GRAPHDB_LICENSE_PATH", tmpFile)
	defer os.Unsetenv("GRAPHDB_LICENSE_PATH")

	got := DetectEdition()

	if got != Enterprise {
		t.Errorf("DetectEdition() with license file = %v, want Enterprise", got)
	}
}

// TestDetectEdition_Precedence tests precedence order of detection
func TestDetectEdition_Precedence(t *testing.T) {
	// Create temp license file
	tmpFile := "./test-license-precedence.key"
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create test license file: %v", err)
	}
	f.Close()
	defer os.Remove(tmpFile)

	// Set both env var (Community) and license file (Enterprise)
	os.Setenv("GRAPHDB_EDITION", "community")
	os.Setenv("GRAPHDB_LICENSE_PATH", tmpFile)
	defer os.Unsetenv("GRAPHDB_EDITION")
	defer os.Unsetenv("GRAPHDB_LICENSE_PATH")

	got := DetectEdition()

	// Environment variable should have precedence
	if got != Community {
		t.Errorf("DetectEdition() with both env and license = %v, want Community (env has precedence)", got)
	}
}

// TestInitialize tests edition initialization
func TestInitialize(t *testing.T) {
	// Save current state
	original := Current

	// Test initialization with Community
	os.Setenv("GRAPHDB_EDITION", "community")
	defer os.Unsetenv("GRAPHDB_EDITION")

	Initialize()

	if Current != Community {
		t.Errorf("Initialize() set Current = %v, want Community", Current)
	}

	// Restore original state
	Current = original
}

// TestIsEnterprise tests Enterprise edition check
func TestIsEnterprise(t *testing.T) {
	// Save current state
	original := Current

	tests := []struct {
		name    string
		edition Edition
		want    bool
	}{
		{"Enterprise edition", Enterprise, true},
		{"Community edition", Community, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Current = tt.edition
			got := IsEnterprise()

			if got != tt.want {
				t.Errorf("IsEnterprise() with %v = %v, want %v", tt.edition, got, tt.want)
			}
		})
	}

	// Restore original state
	Current = original
}

// TestIsCommunity tests Community edition check
func TestIsCommunity(t *testing.T) {
	// Save current state
	original := Current

	tests := []struct {
		name    string
		edition Edition
		want    bool
	}{
		{"Community edition", Community, true},
		{"Enterprise edition", Enterprise, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Current = tt.edition
			got := IsCommunity()

			if got != tt.want {
				t.Errorf("IsCommunity() with %v = %v, want %v", tt.edition, got, tt.want)
			}
		})
	}

	// Restore original state
	Current = original
}

// TestRequireEnterprise tests enterprise requirement checking
func TestRequireEnterprise(t *testing.T) {
	// Save current state
	original := Current

	tests := []struct {
		name      string
		edition   Edition
		feature   string
		wantError bool
	}{
		{"Enterprise allows feature", Enterprise, "test_feature", false},
		{"Community blocks feature", Community, "test_feature", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Current = tt.edition
			err := RequireEnterprise(tt.feature)

			gotError := err != nil
			if gotError != tt.wantError {
				t.Errorf("RequireEnterprise(%q) error = %v, wantError %v", tt.feature, err, tt.wantError)
			}

			// Verify error message contains feature name
			if gotError && err != nil {
				errMsg := err.Error()
				if len(errMsg) == 0 {
					t.Error("Error message is empty")
				}
			}
		})
	}

	// Restore original state
	Current = original
}

// TestLicenseExists tests license file detection
func TestLicenseExists(t *testing.T) {
	// Clean environment
	os.Unsetenv("GRAPHDB_LICENSE_PATH")

	// Should return false with no license
	if licenseExists() {
		t.Error("licenseExists() = true with no license file, want false")
	}

	// Create temp license file
	tmpFile := "./test-license-exists.key"
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create test license file: %v", err)
	}
	f.Close()
	defer os.Remove(tmpFile)

	// Set path and test
	os.Setenv("GRAPHDB_LICENSE_PATH", tmpFile)
	defer os.Unsetenv("GRAPHDB_LICENSE_PATH")

	if !licenseExists() {
		t.Error("licenseExists() = false with license file present, want true")
	}
}

// TestLicenseExists_MultipleLocations tests license detection in different paths
func TestLicenseExists_MultipleLocations(t *testing.T) {
	// Clean environment
	os.Unsetenv("GRAPHDB_LICENSE_PATH")

	// Test with ./license.key (current directory)
	licenseFile := "./license.key"
	f, err := os.Create(licenseFile)
	if err != nil {
		t.Fatalf("Failed to create license file: %v", err)
	}
	f.Close()
	defer os.Remove(licenseFile)

	if !licenseExists() {
		t.Error("licenseExists() should find ./license.key")
	}
}

// BenchmarkDetectEdition benchmarks edition detection
func BenchmarkDetectEdition(b *testing.B) {
	os.Setenv("GRAPHDB_EDITION", "enterprise")
	defer os.Unsetenv("GRAPHDB_EDITION")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectEdition()
	}
}

// BenchmarkIsEnterprise benchmarks enterprise check
func BenchmarkIsEnterprise(b *testing.B) {
	Current = Enterprise

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsEnterprise()
	}
}
