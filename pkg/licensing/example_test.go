package licensing_test

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dd0wney/cluso-graphdb/pkg/licensing"
)

// Example: Basic feature check in an API handler
func ExampleManager_HasFeature() {
	// Initialize the global license manager (typically done in main.go)
	licensing.InitGlobal("", "") // Community tier (no license key)

	// In your API handler, check if a feature is available
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Check if PageRank is available (Pro+ feature)
		if !licensing.Global().HasFeature(licensing.FeaturePageRank) {
			http.Error(w, "PageRank requires Pro or Enterprise tier", http.StatusForbidden)
			return
		}

		// Feature is available, proceed with request
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   []float64{0.15, 0.23, 0.42}, // PageRank results
		})
	}

	_ = handler // Use the handler in your HTTP router
}

// Example: Graceful feature gating with error messages
func ExampleManager_CheckFeature() {
	licensing.InitGlobal("", "") // Community tier

	handler := func(w http.ResponseWriter, r *http.Request) {
		// CheckFeature returns a helpful error with upgrade link
		if err := licensing.Global().CheckFeature(licensing.FeatureFraudDetection); err != nil {
			// Error message includes current tier, required tier, and upgrade URL
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		// Feature is available
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"fraudRings": []string{"ring-1", "ring-2"},
		})
	}

	_ = handler
}

// Example: Check license tier for conditional features
func ExampleManager_GetTier() {
	licensing.InitGlobal("", "")

	handler := func(w http.ResponseWriter, r *http.Request) {
		tier := licensing.Global().GetTier()

		// Return tier-appropriate response
		response := map[string]any{
			"tier": tier,
		}

		switch tier {
		case licensing.TierEnterprise:
			response["features"] = []string{"all", "rbac", "sso", "multi-region"}
		case licensing.TierPro:
			response["features"] = []string{"pagerank", "fraud-detection", "audit-logging"}
		case licensing.TierCommunity:
			response["features"] = []string{"basic-queries", "shortest-path"}
		}

		json.NewEncoder(w).Encode(response)
	}

	_ = handler
}

// Example: Multiple feature checks with fallback behavior
func ExampleManager_HasFeature_fallback() {
	licensing.InitGlobal("", "")

	handler := func(w http.ResponseWriter, r *http.Request) {
		var results any

		// Try enterprise algorithm first
		if licensing.Global().HasFeature(licensing.FeatureCommunityDetection) {
			results = runAdvancedCommunityDetection()
		} else {
			// Fall back to basic algorithm
			results = runBasicClustering()
		}

		json.NewEncoder(w).Encode(map[string]any{
			"results": results,
			"tier":    licensing.Global().GetTier(),
		})
	}

	_ = handler
}

// Example: License info endpoint
func ExampleManager_GetLicense() {
	licensing.InitGlobal("", "")

	handler := func(w http.ResponseWriter, r *http.Request) {
		license := licensing.Global().GetLicense()

		response := map[string]any{
			"valid":  license.IsValid(),
			"tier":   license.Tier,
			"status": license.Status,
		}

		if license.ExpiresAt != nil {
			response["expiresAt"] = license.ExpiresAt
		}
		if license.MaxNodes != nil {
			response["maxNodes"] = license.MaxNodes
		}

		json.NewEncoder(w).Encode(response)
	}

	_ = handler
}

// Example: Feature-based route registration
func ExampleFeaturesByTier() {
	licensing.InitGlobal("", "")

	// Get available features for current tier
	currentTier := licensing.Global().GetTier()
	features := licensing.FeaturesByTier(currentTier)

	fmt.Printf("Available features for %s tier:\n", currentTier)
	for _, feature := range features {
		fmt.Printf("  - %s: %s\n", feature.Name, feature.Description)
	}

	// Example output for Community tier:
	// Available features for community tier:
	//   - basic_queries: Basic graph queries
	//   - shortest_path: Shortest path algorithm
	//   - bfs: Breadth-first search
	//   - dfs: Depth-first search
}

// Mock functions for examples
func runAdvancedCommunityDetection() any {
	return map[string]any{"algorithm": "louvain", "communities": 5}
}

func runBasicClustering() any {
	return map[string]any{"algorithm": "simple", "clusters": 3}
}
