package licensing

import (
	"testing"
	"time"
)

// TestLicenseInfo_IsValid tests the core validation logic
func TestLicenseInfo_IsValid(t *testing.T) {
	now := time.Now()
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)

	tests := []struct {
		name    string
		license LicenseInfo
		want    bool
	}{
		{
			name: "Valid active license without expiration",
			license: LicenseInfo{
				Valid:     true,
				Tier:      TierPro,
				Status:    StatusActive,
				ExpiresAt: nil,
			},
			want: true,
		},
		{
			name: "Valid active license with future expiration",
			license: LicenseInfo{
				Valid:     true,
				Tier:      TierEnterprise,
				Status:    StatusActive,
				ExpiresAt: &future,
			},
			want: true,
		},
		{
			name: "Invalid flag set to false",
			license: LicenseInfo{
				Valid:     false,
				Tier:      TierPro,
				Status:    StatusActive,
				ExpiresAt: nil,
			},
			want: false,
		},
		{
			name: "Expired license",
			license: LicenseInfo{
				Valid:     true,
				Tier:      TierPro,
				Status:    StatusActive,
				ExpiresAt: &past,
			},
			want: false,
		},
		{
			name: "Suspended license",
			license: LicenseInfo{
				Valid:     true,
				Tier:      TierPro,
				Status:    StatusSuspended,
				ExpiresAt: nil,
			},
			want: false,
		},
		{
			name: "Cancelled license",
			license: LicenseInfo{
				Valid:     true,
				Tier:      TierEnterprise,
				Status:    StatusCancelled,
				ExpiresAt: nil,
			},
			want: false,
		},
		{
			name: "Community tier always valid",
			license: LicenseInfo{
				Valid:     true,
				Tier:      TierCommunity,
				Status:    StatusActive,
				ExpiresAt: nil,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.license.IsValid()
			if got != tt.want {
				t.Errorf("LicenseInfo.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLicenseInfo_IsPro tests Pro tier detection
func TestLicenseInfo_IsPro(t *testing.T) {
	tests := []struct {
		name    string
		license LicenseInfo
		want    bool
	}{
		{
			name: "Pro tier license",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierPro,
				Status: StatusActive,
			},
			want: true,
		},
		{
			name: "Enterprise tier license (includes Pro)",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierEnterprise,
				Status: StatusActive,
			},
			want: true,
		},
		{
			name: "Community tier license",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierCommunity,
				Status: StatusActive,
			},
			want: false,
		},
		{
			name: "Invalid Pro license",
			license: LicenseInfo{
				Valid:  false,
				Tier:   TierPro,
				Status: StatusActive,
			},
			want: false,
		},
		{
			name: "Suspended Pro license",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierPro,
				Status: StatusSuspended,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.license.IsPro()
			if got != tt.want {
				t.Errorf("LicenseInfo.IsPro() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLicenseInfo_IsEnterprise tests Enterprise tier detection
func TestLicenseInfo_IsEnterprise(t *testing.T) {
	tests := []struct {
		name    string
		license LicenseInfo
		want    bool
	}{
		{
			name: "Enterprise tier license",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierEnterprise,
				Status: StatusActive,
			},
			want: true,
		},
		{
			name: "Pro tier license",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierPro,
				Status: StatusActive,
			},
			want: false,
		},
		{
			name: "Community tier license",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierCommunity,
				Status: StatusActive,
			},
			want: false,
		},
		{
			name: "Invalid Enterprise license",
			license: LicenseInfo{
				Valid:  false,
				Tier:   TierEnterprise,
				Status: StatusActive,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.license.IsEnterprise()
			if got != tt.want {
				t.Errorf("LicenseInfo.IsEnterprise() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLicenseInfo_HasFeature tests feature gating logic
func TestLicenseInfo_HasFeature(t *testing.T) {
	tests := []struct {
		name    string
		license LicenseInfo
		feature Feature
		want    bool
	}{
		{
			name: "Valid Pro license has Pro feature",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierPro,
				Status: StatusActive,
			},
			feature: FeaturePageRank,
			want:    true,
		},
		{
			name: "Valid Enterprise license has Enterprise feature",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierEnterprise,
				Status: StatusActive,
			},
			feature: FeatureRBAC,
			want:    true,
		},
		{
			name: "Valid Enterprise license has Pro feature",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierEnterprise,
				Status: StatusActive,
			},
			feature: FeaturePageRank,
			want:    true,
		},
		{
			name: "Valid Enterprise license has Community feature",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierEnterprise,
				Status: StatusActive,
			},
			feature: FeatureBasicQueries,
			want:    true,
		},
		{
			name: "Community license has Community feature",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierCommunity,
				Status: StatusActive,
			},
			feature: FeatureShortestPath,
			want:    true,
		},
		{
			name: "Community license doesn't have Pro feature",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierCommunity,
				Status: StatusActive,
			},
			feature: FeaturePageRank,
			want:    false,
		},
		{
			name: "Pro license doesn't have Enterprise feature",
			license: LicenseInfo{
				Valid:  true,
				Tier:   TierPro,
				Status: StatusActive,
			},
			feature: FeatureRBAC,
			want:    false,
		},
		{
			name: "Invalid license only has Community features",
			license: LicenseInfo{
				Valid:  false,
				Tier:   TierEnterprise,
				Status: StatusCancelled,
			},
			feature: FeatureBasicQueries,
			want:    true,
		},
		{
			name: "Invalid license doesn't have Pro features",
			license: LicenseInfo{
				Valid:  false,
				Tier:   TierEnterprise,
				Status: StatusCancelled,
			},
			feature: FeaturePageRank,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.license.HasFeature(tt.feature)
			if got != tt.want {
				t.Errorf("LicenseInfo.HasFeature(%s) = %v, want %v", tt.feature.Name, got, tt.want)
			}
		})
	}
}

// TestFeaturesByTier tests tier-based feature enumeration
func TestFeaturesByTier(t *testing.T) {
	tests := []struct {
		name         string
		tier         LicenseTier
		wantCount    int
		shouldHave   []Feature
		shouldNotHave []Feature
	}{
		{
			name:      "Community tier",
			tier:      TierCommunity,
			wantCount: 4, // basic_queries, shortest_path, bfs, dfs
			shouldHave: []Feature{
				FeatureBasicQueries,
				FeatureShortestPath,
				FeatureBFS,
				FeatureDFS,
			},
			shouldNotHave: []Feature{
				FeaturePageRank,
				FeatureRBAC,
			},
		},
		{
			name:      "Pro tier",
			tier:      TierPro,
			wantCount: 10, // 4 community + 6 pro
			shouldHave: []Feature{
				FeatureBasicQueries,
				FeaturePageRank,
				FeatureFraudDetection,
				FeatureAuditLogging,
			},
			shouldNotHave: []Feature{
				FeatureRBAC,
				FeatureSSO,
			},
		},
		{
			name:      "Enterprise tier",
			tier:      TierEnterprise,
			wantCount: 14, // all features
			shouldHave: []Feature{
				FeatureBasicQueries,
				FeaturePageRank,
				FeatureRBAC,
				FeatureSSO,
			},
			shouldNotHave: []Feature{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features := FeaturesByTier(tt.tier)

			if len(features) != tt.wantCount {
				t.Errorf("FeaturesByTier(%s) returned %d features, want %d", tt.tier, len(features), tt.wantCount)
			}

			// Check features that should be included
			for _, want := range tt.shouldHave {
				found := false
				for _, got := range features {
					if got.Name == want.Name {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("FeaturesByTier(%s) missing feature %s", tt.tier, want.Name)
				}
			}

			// Check features that should NOT be included
			for _, notWant := range tt.shouldNotHave {
				for _, got := range features {
					if got.Name == notWant.Name {
						t.Errorf("FeaturesByTier(%s) should not include feature %s", tt.tier, notWant.Name)
					}
				}
			}
		})
	}
}

// BenchmarkLicenseInfo_IsValid benchmarks validation
func BenchmarkLicenseInfo_IsValid(b *testing.B) {
	license := LicenseInfo{
		Valid:  true,
		Tier:   TierPro,
		Status: StatusActive,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = license.IsValid()
	}
}

// BenchmarkLicenseInfo_HasFeature benchmarks feature checks
func BenchmarkLicenseInfo_HasFeature(b *testing.B) {
	license := LicenseInfo{
		Valid:  true,
		Tier:   TierEnterprise,
		Status: StatusActive,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = license.HasFeature(FeaturePageRank)
	}
}
