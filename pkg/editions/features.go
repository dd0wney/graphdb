package editions

// Feature represents a feature that may be edition-specific
type Feature string

const (
	// FeatureVectorSearch enables vector search capabilities
	FeatureVectorSearch Feature = "vector_search"

	// FeatureCloudflareVectorize enables Cloudflare Vectorize integration (Enterprise only)
	FeatureCloudflareVectorize Feature = "cloudflare_vectorize"

	// FeatureR2Backups enables Cloudflare R2 backup integration
	FeatureR2Backups Feature = "r2_backups"

	// FeatureCDC enables Change Data Capture with Cloudflare Queues
	FeatureCDC Feature = "cdc"

	// FeatureGraphQL enables GraphQL API endpoint
	FeatureGraphQL Feature = "graphql"

	// FeatureAdvancedMonitoring enables advanced monitoring and metrics
	FeatureAdvancedMonitoring Feature = "advanced_monitoring"

	// FeatureMultiRegionReplication enables multi-region replication
	FeatureMultiRegionReplication Feature = "multi_region_replication"

	// FeatureCustomAuth enables custom authentication providers
	FeatureCustomAuth Feature = "custom_auth"
)

// FeatureSet defines which features are available for each edition
var FeatureSet = map[Edition]map[Feature]bool{
	Community: {
		FeatureVectorSearch:           true,  // Custom HNSW implementation
		FeatureCloudflareVectorize:    false, // Enterprise only
		FeatureR2Backups:              false, // Can add manual S3 backups
		FeatureCDC:                    false, // Enterprise only
		FeatureGraphQL:                true,  // Available in both
		FeatureAdvancedMonitoring:     false, // Enterprise only
		FeatureMultiRegionReplication: false, // Enterprise only
		FeatureCustomAuth:             false, // Enterprise only
	},
	Enterprise: {
		FeatureVectorSearch:           true, // Via Cloudflare Vectorize
		FeatureCloudflareVectorize:    true,
		FeatureR2Backups:              true,
		FeatureCDC:                    true,
		FeatureGraphQL:                true,
		FeatureAdvancedMonitoring:     true,
		FeatureMultiRegionReplication: true,
		FeatureCustomAuth:             true,
	},
}

// IsEnabled checks if a feature is enabled for the current edition
func IsEnabled(feature Feature) bool {
	if features, ok := FeatureSet[Current]; ok {
		return features[feature]
	}
	return false
}

// RequireFeature returns an error if the feature is not enabled
func RequireFeature(feature Feature) error {
	if !IsEnabled(feature) {
		return RequireEnterprise(string(feature))
	}
	return nil
}

// GetEnabledFeatures returns all enabled features for the current edition
func GetEnabledFeatures() []Feature {
	features := []Feature{}
	if featureMap, ok := FeatureSet[Current]; ok {
		for feature, enabled := range featureMap {
			if enabled {
				features = append(features, feature)
			}
		}
	}
	return features
}

// FeatureInfo provides metadata about a feature
type FeatureInfo struct {
	Name        Feature
	Description string
	Edition     string // "Community", "Enterprise", "Both"
}

// AllFeatures returns information about all available features
var AllFeatures = []FeatureInfo{
	{
		Name:        FeatureVectorSearch,
		Description: "Vector search with approximate nearest neighbor",
		Edition:     "Both (Community: HNSW, Enterprise: Vectorize)",
	},
	{
		Name:        FeatureCloudflareVectorize,
		Description: "Cloudflare Vectorize integration for 5M+ dimensions",
		Edition:     "Enterprise",
	},
	{
		Name:        FeatureR2Backups,
		Description: "Automated backups to Cloudflare R2 with zero egress",
		Edition:     "Enterprise",
	},
	{
		Name:        FeatureCDC,
		Description: "Change Data Capture streaming to Cloudflare Queues",
		Edition:     "Enterprise",
	},
	{
		Name:        FeatureGraphQL,
		Description: "GraphQL API endpoint with edge caching",
		Edition:     "Both",
	},
	{
		Name:        FeatureAdvancedMonitoring,
		Description: "Advanced metrics, tracing, and monitoring",
		Edition:     "Enterprise",
	},
	{
		Name:        FeatureMultiRegionReplication,
		Description: "Multi-region replication with automatic failover",
		Edition:     "Enterprise",
	},
	{
		Name:        FeatureCustomAuth,
		Description: "Custom authentication providers (SAML, OIDC, etc.)",
		Edition:     "Enterprise",
	},
}
