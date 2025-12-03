package licensing

// Feature represents a GraphDB feature with licensing requirements
type Feature struct {
	Name         string
	Description  string
	RequiredTier LicenseTier
}

// GraphDB feature definitions
var (
	// Community features (always available)
	FeatureBasicQueries = Feature{
		Name:         "basic_queries",
		Description:  "Basic graph queries (nodes, edges, traversal)",
		RequiredTier: TierCommunity,
	}

	FeatureShortestPath = Feature{
		Name:         "shortest_path",
		Description:  "Shortest path algorithm",
		RequiredTier: TierCommunity,
	}

	FeatureBFS = Feature{
		Name:         "bfs",
		Description:  "Breadth-first search",
		RequiredTier: TierCommunity,
	}

	FeatureDFS = Feature{
		Name:         "dfs",
		Description:  "Depth-first search",
		RequiredTier: TierCommunity,
	}

	// Pro features
	FeaturePageRank = Feature{
		Name:         "pagerank",
		Description:  "PageRank algorithm",
		RequiredTier: TierPro,
	}

	FeatureCommunityDetection = Feature{
		Name:         "community_detection",
		Description:  "Community detection algorithms",
		RequiredTier: TierPro,
	}

	FeatureTrustScoring = Feature{
		Name:         "trust_scoring",
		Description:  "Trust and reputation scoring",
		RequiredTier: TierPro,
	}

	FeatureFraudDetection = Feature{
		Name:         "fraud_detection",
		Description:  "Fraud ring detection",
		RequiredTier: TierPro,
	}

	FeatureTemporalGraphs = Feature{
		Name:         "temporal_graphs",
		Description:  "Time-based graph queries",
		RequiredTier: TierPro,
	}

	FeatureAuditLogging = Feature{
		Name:         "audit_logging",
		Description:  "Comprehensive audit logging",
		RequiredTier: TierPro,
	}

	// Enterprise features
	FeatureRBAC = Feature{
		Name:         "rbac",
		Description:  "Role-based access control",
		RequiredTier: TierEnterprise,
	}

	FeatureSSO = Feature{
		Name:         "sso",
		Description:  "Single sign-on (SAML/OAuth)",
		RequiredTier: TierEnterprise,
	}

	FeaturePrioritySupport = Feature{
		Name:         "priority_support",
		Description:  "Priority email support with 24h SLA",
		RequiredTier: TierEnterprise,
	}

	FeatureMultiRegion = Feature{
		Name:         "multi_region",
		Description:  "Multi-region replication",
		RequiredTier: TierEnterprise,
	}
)

// AllFeatures returns a list of all GraphDB features
func AllFeatures() []Feature {
	return []Feature{
		// Community
		FeatureBasicQueries,
		FeatureShortestPath,
		FeatureBFS,
		FeatureDFS,

		// Pro
		FeaturePageRank,
		FeatureCommunityDetection,
		FeatureTrustScoring,
		FeatureFraudDetection,
		FeatureTemporalGraphs,
		FeatureAuditLogging,

		// Enterprise
		FeatureRBAC,
		FeatureSSO,
		FeaturePrioritySupport,
		FeatureMultiRegion,
	}
}

// FeaturesByTier returns features available for a given tier
func FeaturesByTier(tier LicenseTier) []Feature {
	features := []Feature{}

	for _, feature := range AllFeatures() {
		switch tier {
		case TierEnterprise:
			features = append(features, feature)
		case TierPro:
			if feature.RequiredTier == TierCommunity || feature.RequiredTier == TierPro {
				features = append(features, feature)
			}
		case TierCommunity:
			if feature.RequiredTier == TierCommunity {
				features = append(features, feature)
			}
		}
	}

	return features
}
