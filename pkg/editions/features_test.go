package editions

import (
	"testing"
)

// TestIsEnabled tests feature availability checking
func TestIsEnabled(t *testing.T) {
	// Save current state
	original := Current

	tests := []struct {
		name    string
		edition Edition
		feature Feature
		want    bool
	}{
		// Community edition
		{"Community has VectorSearch", Community, FeatureVectorSearch, true},
		{"Community has GraphQL", Community, FeatureGraphQL, true},
		{"Community lacks CloudflareVectorize", Community, FeatureCloudflareVectorize, false},
		{"Community lacks R2Backups", Community, FeatureR2Backups, false},
		{"Community lacks CDC", Community, FeatureCDC, false},
		{"Community lacks AdvancedMonitoring", Community, FeatureAdvancedMonitoring, false},
		{"Community lacks MultiRegionReplication", Community, FeatureMultiRegionReplication, false},
		{"Community lacks CustomAuth", Community, FeatureCustomAuth, false},

		// Enterprise edition
		{"Enterprise has VectorSearch", Enterprise, FeatureVectorSearch, true},
		{"Enterprise has GraphQL", Enterprise, FeatureGraphQL, true},
		{"Enterprise has CloudflareVectorize", Enterprise, FeatureCloudflareVectorize, true},
		{"Enterprise has R2Backups", Enterprise, FeatureR2Backups, true},
		{"Enterprise has CDC", Enterprise, FeatureCDC, true},
		{"Enterprise has AdvancedMonitoring", Enterprise, FeatureAdvancedMonitoring, true},
		{"Enterprise has MultiRegionReplication", Enterprise, FeatureMultiRegionReplication, true},
		{"Enterprise has CustomAuth", Enterprise, FeatureCustomAuth, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Current = tt.edition
			got := IsEnabled(tt.feature)

			if got != tt.want {
				t.Errorf("IsEnabled(%q) with %v = %v, want %v", tt.feature, tt.edition, got, tt.want)
			}
		})
	}

	// Restore original state
	Current = original
}

// TestRequireFeature tests feature requirement checking
func TestRequireFeature(t *testing.T) {
	// Save current state
	original := Current

	tests := []struct {
		name      string
		edition   Edition
		feature   Feature
		wantError bool
	}{
		// Community features
		{"Community can use VectorSearch", Community, FeatureVectorSearch, false},
		{"Community can use GraphQL", Community, FeatureGraphQL, false},
		{"Community cannot use R2Backups", Community, FeatureR2Backups, true},
		{"Community cannot use CDC", Community, FeatureCDC, true},

		// Enterprise features
		{"Enterprise can use VectorSearch", Enterprise, FeatureVectorSearch, false},
		{"Enterprise can use GraphQL", Enterprise, FeatureGraphQL, false},
		{"Enterprise can use R2Backups", Enterprise, FeatureR2Backups, false},
		{"Enterprise can use CDC", Enterprise, FeatureCDC, false},
		{"Enterprise can use AdvancedMonitoring", Enterprise, FeatureAdvancedMonitoring, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Current = tt.edition
			err := RequireFeature(tt.feature)

			gotError := err != nil
			if gotError != tt.wantError {
				t.Errorf("RequireFeature(%q) with %v error = %v, wantError %v", tt.feature, tt.edition, err, tt.wantError)
			}
		})
	}

	// Restore original state
	Current = original
}

// TestGetEnabledFeatures tests enabled feature enumeration
func TestGetEnabledFeatures(t *testing.T) {
	// Save current state
	original := Current

	tests := []struct {
		name         string
		edition      Edition
		wantMinCount int
		mustHave     []Feature
		mustNotHave  []Feature
	}{
		{
			name:         "Community features",
			edition:      Community,
			wantMinCount: 2, // At least VectorSearch and GraphQL
			mustHave: []Feature{
				FeatureVectorSearch,
				FeatureGraphQL,
			},
			mustNotHave: []Feature{
				FeatureCloudflareVectorize,
				FeatureR2Backups,
				FeatureCDC,
			},
		},
		{
			name:         "Enterprise features",
			edition:      Enterprise,
			wantMinCount: 8, // All features
			mustHave: []Feature{
				FeatureVectorSearch,
				FeatureGraphQL,
				FeatureCloudflareVectorize,
				FeatureR2Backups,
				FeatureCDC,
				FeatureAdvancedMonitoring,
				FeatureMultiRegionReplication,
				FeatureCustomAuth,
			},
			mustNotHave: []Feature{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Current = tt.edition
			features := GetEnabledFeatures()

			if len(features) < tt.wantMinCount {
				t.Errorf("GetEnabledFeatures() returned %d features, want at least %d", len(features), tt.wantMinCount)
			}

			// Check must-have features
			featureMap := make(map[Feature]bool)
			for _, f := range features {
				featureMap[f] = true
			}

			for _, mustHave := range tt.mustHave {
				if !featureMap[mustHave] {
					t.Errorf("GetEnabledFeatures() missing required feature %q", mustHave)
				}
			}

			// Check must-not-have features
			for _, mustNotHave := range tt.mustNotHave {
				if featureMap[mustNotHave] {
					t.Errorf("GetEnabledFeatures() should not include %q", mustNotHave)
				}
			}
		})
	}

	// Restore original state
	Current = original
}

// TestFeatureSet_Completeness tests that all features are defined
func TestFeatureSet_Completeness(t *testing.T) {
	allFeatures := []Feature{
		FeatureVectorSearch,
		FeatureCloudflareVectorize,
		FeatureR2Backups,
		FeatureCDC,
		FeatureGraphQL,
		FeatureAdvancedMonitoring,
		FeatureMultiRegionReplication,
		FeatureCustomAuth,
	}

	editions := []Edition{Community, Enterprise}

	for _, edition := range editions {
		t.Run(edition.String(), func(t *testing.T) {
			featureMap, ok := FeatureSet[edition]
			if !ok {
				t.Fatalf("FeatureSet missing edition %v", edition)
			}

			// Check all features are defined
			for _, feature := range allFeatures {
				if _, exists := featureMap[feature]; !exists {
					t.Errorf("Feature %q not defined for %v edition", feature, edition)
				}
			}
		})
	}
}

// TestAllFeatures_Metadata tests feature metadata
func TestAllFeatures_Metadata(t *testing.T) {
	if len(AllFeatures) == 0 {
		t.Fatal("AllFeatures is empty")
	}

	// Check each feature has proper metadata
	for _, info := range AllFeatures {
		t.Run(string(info.Name), func(t *testing.T) {
			if info.Name == "" {
				t.Error("Feature name is empty")
			}

			if info.Description == "" {
				t.Error("Feature description is empty")
			}

			if info.Edition == "" {
				t.Error("Feature edition is empty")
			}

			// Edition should be one of: "Community", "Enterprise", "Both"
			validEditions := map[string]bool{
				"Community":  true,
				"Enterprise": true,
				"Both":       true,
			}

			// Check if edition string contains valid keyword
			hasValidEdition := false
			for valid := range validEditions {
				if contains(info.Edition, valid) {
					hasValidEdition = true
					break
				}
			}

			if !hasValidEdition {
				t.Errorf("Feature edition %q should contain 'Community', 'Enterprise', or 'Both'", info.Edition)
			}
		})
	}
}

// TestFeatureGating_Integration tests realistic feature gating scenarios
func TestFeatureGating_Integration(t *testing.T) {
	// Save current state
	original := Current

	t.Run("Community user tries R2 backups", func(t *testing.T) {
		Current = Community

		// Try to use R2 backups
		if err := RequireFeature(FeatureR2Backups); err == nil {
			t.Error("Community user should not be able to use R2 backups")
		} else {
			t.Logf("✓ Correctly blocked R2 backups: %v", err)
		}
	})

	t.Run("Enterprise user accesses all features", func(t *testing.T) {
		Current = Enterprise

		enterpriseFeatures := []Feature{
			FeatureVectorSearch,
			FeatureCloudflareVectorize,
			FeatureR2Backups,
			FeatureCDC,
			FeatureGraphQL,
			FeatureAdvancedMonitoring,
			FeatureMultiRegionReplication,
			FeatureCustomAuth,
		}

		for _, feature := range enterpriseFeatures {
			if err := RequireFeature(feature); err != nil {
				t.Errorf("Enterprise user should have access to %q: %v", feature, err)
			}
		}

		t.Logf("✓ Enterprise user has access to all %d features", len(enterpriseFeatures))
	})

	t.Run("Conditional algorithm selection based on edition", func(t *testing.T) {
		// Simulate choosing vector search backend
		Current = Community
		var backend string
		if IsEnabled(FeatureCloudflareVectorize) {
			backend = "cloudflare-vectorize"
		} else {
			backend = "hnsw"
		}

		if backend != "hnsw" {
			t.Errorf("Community should use HNSW, got %s", backend)
		}

		Current = Enterprise
		if IsEnabled(FeatureCloudflareVectorize) {
			backend = "cloudflare-vectorize"
		} else {
			backend = "hnsw"
		}

		if backend != "cloudflare-vectorize" {
			t.Errorf("Enterprise should use Cloudflare Vectorize, got %s", backend)
		}

		t.Logf("✓ Correctly selected backends based on edition")
	})

	// Restore original state
	Current = original
}

// BenchmarkIsEnabled benchmarks feature checking
func BenchmarkIsEnabled(b *testing.B) {
	Current = Enterprise

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsEnabled(FeatureR2Backups)
	}
}

// BenchmarkGetEnabledFeatures benchmarks feature enumeration
func BenchmarkGetEnabledFeatures(b *testing.B) {
	Current = Enterprise

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetEnabledFeatures()
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
