package licensing

import (
	"strings"
	"testing"
)

func TestGenerateFingerprint(t *testing.T) {
	fingerprint, err := GenerateFingerprint()
	if err != nil {
		t.Fatalf("GenerateFingerprint() error = %v", err)
	}

	// Verify fingerprint is not nil
	if fingerprint == nil {
		t.Fatal("GenerateFingerprint() returned nil")
	}

	// Verify hash is not empty
	if fingerprint.Hash == "" {
		t.Error("Fingerprint hash is empty")
	}

	// Verify hash is hex-encoded SHA-256 (64 characters)
	if len(fingerprint.Hash) != 64 {
		t.Errorf("Fingerprint hash length = %d, want 64", len(fingerprint.Hash))
	}

	// Verify all hex characters
	for _, c := range fingerprint.Hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Fingerprint hash contains non-hex character: %c", c)
		}
	}

	// Verify components exist
	if fingerprint.Components == nil {
		t.Fatal("Fingerprint components is nil")
	}

	// Verify expected components
	expectedComponents := []string{"cpu_arch", "cpu_os", "cpu_cores", "hostname", "mac_address"}
	for _, comp := range expectedComponents {
		if _, exists := fingerprint.Components[comp]; !exists {
			t.Errorf("Missing expected component: %s", comp)
		}
	}

	// Verify cpu_cores is a number
	if cores := fingerprint.Components["cpu_cores"]; cores == "" {
		t.Error("cpu_cores is empty")
	}
}

func TestGenerateFingerprint_Deterministic(t *testing.T) {
	// Generate two fingerprints
	fp1, err1 := GenerateFingerprint()
	fp2, err2 := GenerateFingerprint()

	if err1 != nil {
		t.Fatalf("First GenerateFingerprint() error = %v", err1)
	}
	if err2 != nil {
		t.Fatalf("Second GenerateFingerprint() error = %v", err2)
	}

	// Hashes should be identical (deterministic)
	if fp1.Hash != fp2.Hash {
		t.Errorf("Fingerprints are not deterministic: %s != %s", fp1.Hash, fp2.Hash)
	}

	// Components should be identical
	if len(fp1.Components) != len(fp2.Components) {
		t.Error("Component counts differ between fingerprints")
	}

	for key, val1 := range fp1.Components {
		if val2, exists := fp2.Components[key]; !exists || val1 != val2 {
			t.Errorf("Component %s differs: %s != %s", key, val1, val2)
		}
	}
}

func TestVerifyFingerprint(t *testing.T) {
	// Generate current fingerprint
	current, err := GenerateFingerprint()
	if err != nil {
		t.Fatalf("GenerateFingerprint() error = %v", err)
	}

	tests := []struct {
		name         string
		expectedHash string
		want         bool
		wantErr      bool
	}{
		{
			name:         "Valid matching fingerprint",
			expectedHash: current.Hash,
			want:         true,
			wantErr:      false,
		},
		{
			name:         "Invalid non-matching fingerprint",
			expectedHash: "0000000000000000000000000000000000000000000000000000000000000000",
			want:         false,
			wantErr:      false,
		},
		{
			name:         "Different hash",
			expectedHash: strings.Repeat("a", 64),
			want:         false,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := VerifyFingerprint(tt.expectedHash)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyFingerprint() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if match != tt.want {
				t.Errorf("VerifyFingerprint() = %v, want %v", match, tt.want)
			}
		})
	}
}

func TestLicense_BindToFingerprint(t *testing.T) {
	license := &License{
		ID:    "test-id",
		Key:   "CGDB-TEST-KEY0-0000-0000",
		Email: "test@example.com",
		Type:  LicenseTypeProfessional,
	}

	fingerprint, err := GenerateFingerprint()
	if err != nil {
		t.Fatalf("GenerateFingerprint() error = %v", err)
	}

	// Bind fingerprint to license
	license.BindToFingerprint(fingerprint)

	// Verify metadata was created
	if license.Metadata == nil {
		t.Fatal("License metadata is nil after binding")
	}

	// Verify fingerprint was stored
	storedHash, exists := license.Metadata["hardware_fingerprint"]
	if !exists {
		t.Error("hardware_fingerprint not found in metadata")
	}
	if storedHash != fingerprint.Hash {
		t.Errorf("Stored fingerprint = %s, want %s", storedHash, fingerprint.Hash)
	}
}

func TestLicense_BindToFingerprint_PreservesExistingMetadata(t *testing.T) {
	license := &License{
		ID:       "test-id",
		Key:      "CGDB-TEST-KEY0-0000-0000",
		Email:    "test@example.com",
		Type:     LicenseTypeProfessional,
		Metadata: map[string]string{"existing_key": "existing_value"},
	}

	fingerprint, err := GenerateFingerprint()
	if err != nil {
		t.Fatalf("GenerateFingerprint() error = %v", err)
	}

	// Bind fingerprint
	license.BindToFingerprint(fingerprint)

	// Verify existing metadata is preserved
	if val, exists := license.Metadata["existing_key"]; !exists || val != "existing_value" {
		t.Error("Existing metadata was not preserved")
	}

	// Verify fingerprint was added
	if _, exists := license.Metadata["hardware_fingerprint"]; !exists {
		t.Error("hardware_fingerprint not added to metadata")
	}
}

func TestLicense_VerifyHardwareBinding_NoBinding(t *testing.T) {
	tests := []struct {
		name    string
		license *License
		want    bool
		wantErr bool
	}{
		{
			name: "License with nil metadata",
			license: &License{
				ID:       "test-id",
				Key:      "CGDB-TEST-KEY0-0000-0000",
				Email:    "test@example.com",
				Type:     LicenseTypeProfessional,
				Metadata: nil,
			},
			want:    true, // No binding - allow
			wantErr: false,
		},
		{
			name: "License with empty metadata",
			license: &License{
				ID:       "test-id",
				Key:      "CGDB-TEST-KEY0-0000-0000",
				Email:    "test@example.com",
				Type:     LicenseTypeProfessional,
				Metadata: map[string]string{},
			},
			want:    true, // No binding - allow
			wantErr: false,
		},
		{
			name: "License with other metadata but no fingerprint",
			license: &License{
				ID:    "test-id",
				Key:   "CGDB-TEST-KEY0-0000-0000",
				Email: "test@example.com",
				Type:  LicenseTypeProfessional,
				Metadata: map[string]string{
					"some_other_key": "some_value",
				},
			},
			want:    true, // No binding - allow
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := tt.license.VerifyHardwareBinding()
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyHardwareBinding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if valid != tt.want {
				t.Errorf("VerifyHardwareBinding() = %v, want %v", valid, tt.want)
			}
		})
	}
}

func TestLicense_VerifyHardwareBinding_WithBinding(t *testing.T) {
	// Get current hardware fingerprint
	current, err := GenerateFingerprint()
	if err != nil {
		t.Fatalf("GenerateFingerprint() error = %v", err)
	}

	tests := []struct {
		name    string
		license *License
		want    bool
		wantErr bool
	}{
		{
			name: "License bound to current hardware",
			license: &License{
				ID:    "test-id",
				Key:   "CGDB-TEST-KEY0-0000-0000",
				Email: "test@example.com",
				Type:  LicenseTypeProfessional,
				Metadata: map[string]string{
					"hardware_fingerprint": current.Hash,
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "License bound to different hardware",
			license: &License{
				ID:    "test-id",
				Key:   "CGDB-TEST-KEY0-0000-0000",
				Email: "test@example.com",
				Type:  LicenseTypeProfessional,
				Metadata: map[string]string{
					"hardware_fingerprint": "0000000000000000000000000000000000000000000000000000000000000000",
				},
			},
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := tt.license.VerifyHardwareBinding()
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyHardwareBinding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if valid != tt.want {
				t.Errorf("VerifyHardwareBinding() = %v, want %v", valid, tt.want)
			}
		})
	}
}

func TestLicense_GetFingerprint(t *testing.T) {
	tests := []struct {
		name       string
		license    *License
		wantHash   string
		wantExists bool
	}{
		{
			name: "License with fingerprint",
			license: &License{
				ID:    "test-id",
				Key:   "CGDB-TEST-KEY0-0000-0000",
				Email: "test@example.com",
				Type:  LicenseTypeProfessional,
				Metadata: map[string]string{
					"hardware_fingerprint": "test-hash-123",
				},
			},
			wantHash:   "test-hash-123",
			wantExists: true,
		},
		{
			name: "License without fingerprint",
			license: &License{
				ID:    "test-id",
				Key:   "CGDB-TEST-KEY0-0000-0000",
				Email: "test@example.com",
				Type:  LicenseTypeProfessional,
				Metadata: map[string]string{
					"other_key": "other_value",
				},
			},
			wantHash:   "",
			wantExists: false,
		},
		{
			name: "License with nil metadata",
			license: &License{
				ID:       "test-id",
				Key:      "CGDB-TEST-KEY0-0000-0000",
				Email:    "test@example.com",
				Type:     LicenseTypeProfessional,
				Metadata: nil,
			},
			wantHash:   "",
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, exists := tt.license.GetFingerprint()
			if hash != tt.wantHash {
				t.Errorf("GetFingerprint() hash = %v, want %v", hash, tt.wantHash)
			}
			if exists != tt.wantExists {
				t.Errorf("GetFingerprint() exists = %v, want %v", exists, tt.wantExists)
			}
		})
	}
}

func TestHashComponents_Deterministic(t *testing.T) {
	components := map[string]string{
		"cpu_arch":    "amd64",
		"cpu_os":      "linux",
		"cpu_cores":   "8",
		"hostname":    "test-host",
		"mac_address": "aa:bb:cc:dd:ee:ff",
	}

	// Hash multiple times
	hash1 := hashComponents(components)
	hash2 := hashComponents(components)
	hash3 := hashComponents(components)

	// All should be identical
	if hash1 != hash2 || hash2 != hash3 {
		t.Errorf("hashComponents is not deterministic: %s, %s, %s", hash1, hash2, hash3)
	}

	// Verify it's a valid hex string
	if len(hash1) != 64 {
		t.Errorf("Hash length = %d, want 64", len(hash1))
	}
}

func TestHashComponents_DifferentComponents(t *testing.T) {
	components1 := map[string]string{
		"cpu_arch":    "amd64",
		"cpu_os":      "linux",
		"cpu_cores":   "8",
		"hostname":    "host1",
		"mac_address": "aa:bb:cc:dd:ee:ff",
	}

	components2 := map[string]string{
		"cpu_arch":    "amd64",
		"cpu_os":      "linux",
		"cpu_cores":   "8",
		"hostname":    "host2", // Different hostname
		"mac_address": "aa:bb:cc:dd:ee:ff",
	}

	hash1 := hashComponents(components1)
	hash2 := hashComponents(components2)

	// Hashes should be different
	if hash1 == hash2 {
		t.Error("hashComponents produced same hash for different components")
	}
}

func TestHashComponents_OrderIndependent(t *testing.T) {
	// Same components, added in different order
	components1 := map[string]string{
		"a": "value1",
		"b": "value2",
		"c": "value3",
	}

	components2 := map[string]string{
		"c": "value3",
		"a": "value1",
		"b": "value2",
	}

	hash1 := hashComponents(components1)
	hash2 := hashComponents(components2)

	// Hashes should be identical (order-independent)
	if hash1 != hash2 {
		t.Errorf("hashComponents is order-dependent: %s != %s", hash1, hash2)
	}
}

// Benchmark fingerprint operations
func BenchmarkGenerateFingerprint(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GenerateFingerprint()
	}
}

func BenchmarkVerifyFingerprint(b *testing.B) {
	fp, _ := GenerateFingerprint()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		VerifyFingerprint(fp.Hash)
	}
}

func BenchmarkHashComponents(b *testing.B) {
	components := map[string]string{
		"cpu_arch":    "amd64",
		"cpu_os":      "linux",
		"cpu_cores":   "8",
		"hostname":    "test-host",
		"mac_address": "aa:bb:cc:dd:ee:ff",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hashComponents(components)
	}
}
