package licensing

import (
	"testing"
)

// TestManager_GetLicense tests license retrieval
func TestManager_GetLicense(t *testing.T) {
	// Initialize with community tier (no license key)
	InitGlobal("", "")
	manager := Global()

	license := manager.GetLicense()

	if license == nil {
		t.Fatal("GetLicense() returned nil")
	}

	if license.Tier != TierCommunity {
		t.Errorf("Expected community tier, got %s", license.Tier)
	}

	if !license.IsValid() {
		t.Error("Community license should be valid")
	}
}

// TestManager_GetTier tests tier retrieval
func TestManager_GetTier(t *testing.T) {
	InitGlobal("", "")
	manager := Global()

	tier := manager.GetTier()

	if tier != TierCommunity {
		t.Errorf("GetTier() = %s, want %s", tier, TierCommunity)
	}
}

// TestManager_IsValid tests validity check
func TestManager_IsValid(t *testing.T) {
	InitGlobal("", "")
	manager := Global()

	if !manager.IsValid() {
		t.Error("Community license should be valid")
	}
}

// TestManager_IsPro tests Pro tier detection
func TestManager_IsPro(t *testing.T) {
	InitGlobal("", "")
	manager := Global()

	if manager.IsPro() {
		t.Error("Community tier should not be Pro")
	}
}

// TestManager_IsEnterprise tests Enterprise tier detection
func TestManager_IsEnterprise(t *testing.T) {
	InitGlobal("", "")
	manager := Global()

	if manager.IsEnterprise() {
		t.Error("Community tier should not be Enterprise")
	}
}

// TestManager_HasFeature tests feature availability checking
func TestManager_HasFeature(t *testing.T) {
	InitGlobal("", "")
	manager := Global()

	tests := []struct {
		name    string
		feature Feature
		want    bool
	}{
		{
			name:    "Community feature available",
			feature: FeatureBasicQueries,
			want:    true,
		},
		{
			name:    "Shortest path available in community",
			feature: FeatureShortestPath,
			want:    true,
		},
		{
			name:    "Pro feature not available in community",
			feature: FeaturePageRank,
			want:    false,
		},
		{
			name:    "Enterprise feature not available in community",
			feature: FeatureRBAC,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.HasFeature(tt.feature)
			if got != tt.want {
				t.Errorf("HasFeature(%s) = %v, want %v", tt.feature.Name, got, tt.want)
			}
		})
	}
}

// TestManager_CheckFeature tests feature checking with errors
func TestManager_CheckFeature(t *testing.T) {
	InitGlobal("", "")
	manager := Global()

	tests := []struct {
		name      string
		feature   Feature
		wantError bool
	}{
		{
			name:      "Community feature check succeeds",
			feature:   FeatureBasicQueries,
			wantError: false,
		},
		{
			name:      "Pro feature check fails in community",
			feature:   FeaturePageRank,
			wantError: true,
		},
		{
			name:      "Enterprise feature check fails in community",
			feature:   FeatureRBAC,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.CheckFeature(tt.feature)
			gotError := err != nil

			if gotError != tt.wantError {
				t.Errorf("CheckFeature(%s) error = %v, wantError %v", tt.feature.Name, err, tt.wantError)
			}

			// Verify error type
			if gotError {
				if _, ok := err.(*FeatureNotAvailableError); !ok {
					t.Errorf("Expected FeatureNotAvailableError, got %T", err)
				}
			}
		})
	}
}

// TestFeatureNotAvailableError_Message tests error message format
func TestFeatureNotAvailableError_Message(t *testing.T) {
	err := &FeatureNotAvailableError{
		Feature:      FeaturePageRank,
		CurrentTier:  TierCommunity,
		RequiredTier: TierPro,
	}

	msg := err.Error()

	// Check message contains key information
	if msg == "" {
		t.Error("Error message is empty")
	}

	// Message should mention feature name
	if len(msg) < 10 {
		t.Errorf("Error message too short: %s", msg)
	}

	t.Logf("Error message: %s", msg)
}

// TestManager_Global_Singleton tests that Global() returns singleton
func TestManager_Global_Singleton(t *testing.T) {
	InitGlobal("", "")

	m1 := Global()
	m2 := Global()

	if m1 != m2 {
		t.Error("Global() should return the same singleton instance")
	}
}

// TestManager_ConcurrentAccess tests thread-safe access
func TestManager_ConcurrentAccess(t *testing.T) {
	InitGlobal("", "")
	manager := Global()

	// Launch concurrent goroutines accessing the manager
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func() {
			defer func() { done <- true }()

			// Read operations that should be thread-safe
			_ = manager.GetLicense()
			_ = manager.GetTier()
			_ = manager.IsValid()
			_ = manager.HasFeature(FeaturePageRank)
			_ = manager.CheckFeature(FeatureBasicQueries)
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}
}

// TestManager_FeatureGating_Integration tests realistic feature gating scenarios
func TestManager_FeatureGating_Integration(t *testing.T) {
	InitGlobal("", "")
	manager := Global()

	t.Run("API handler simulation - PageRank", func(t *testing.T) {
		// Simulate API handler checking for PageRank feature
		if err := manager.CheckFeature(FeaturePageRank); err != nil {
			// Expected: should fail in community tier
			if _, ok := err.(*FeatureNotAvailableError); !ok {
				t.Errorf("Expected FeatureNotAvailableError, got %T", err)
			}
			t.Logf("✓ Correctly blocked PageRank in community tier")
		} else {
			t.Error("PageRank should not be available in community tier")
		}
	})

	t.Run("API handler simulation - Basic queries", func(t *testing.T) {
		// Simulate API handler checking for basic queries
		if err := manager.CheckFeature(FeatureBasicQueries); err != nil {
			t.Errorf("Basic queries should be available: %v", err)
		} else {
			t.Logf("✓ Basic queries available in community tier")
		}
	})

	t.Run("Conditional algorithm selection", func(t *testing.T) {
		// Simulate choosing algorithm based on tier
		var algorithm string
		if manager.HasFeature(FeatureCommunityDetection) {
			algorithm = "louvain"
		} else {
			algorithm = "simple-clustering"
		}

		if algorithm != "simple-clustering" {
			t.Errorf("Expected simple clustering in community tier, got %s", algorithm)
		}
		t.Logf("✓ Correctly selected %s algorithm for community tier", algorithm)
	})
}

// BenchmarkManager_GetLicense benchmarks license retrieval
func BenchmarkManager_GetLicense(b *testing.B) {
	InitGlobal("", "")
	manager := Global()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.GetLicense()
	}
}

// BenchmarkManager_HasFeature benchmarks feature checking
func BenchmarkManager_HasFeature(b *testing.B) {
	InitGlobal("", "")
	manager := Global()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.HasFeature(FeaturePageRank)
	}
}

// BenchmarkManager_CheckFeature benchmarks feature checking with error
func BenchmarkManager_CheckFeature(b *testing.B) {
	InitGlobal("", "")
	manager := Global()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.CheckFeature(FeaturePageRank)
	}
}

// BenchmarkManager_ConcurrentReads benchmarks concurrent read access
func BenchmarkManager_ConcurrentReads(b *testing.B) {
	InitGlobal("", "")
	manager := Global()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = manager.GetLicense()
			_ = manager.HasFeature(FeaturePageRank)
		}
	})
}
