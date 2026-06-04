package graphql

import (
	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// createNodeAggregateResolver creates a resolver for node aggregations.
//
// deps is the F3 masking hookup. When a tenant policy masks a numeric
// property, the masked value becomes a TypeString (per
// Policy.ApplyToStorageValues) and the numeric switch below skips it —
// the operator-named-mask semantics propagate to aggregates without
// special-casing. Nil deps = no masking, aggregates over raw values.
func createNodeAggregateResolver(gs *storage.GraphStorage, label string, deps *MaskingDeps) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Audit A6c-graphql-resolvers: tenant-scoped label lookup.
		tenantID := tenant.MustFromContext(p.Context)
		nodes := gs.GetNodesByLabelForTenant(tenantID, label)

		result := make(map[string]any)

		// Count
		result["count"] = len(nodes)

		// For min, max, avg, sum - we need to aggregate over properties
		minValues := make(map[string]any)
		maxValues := make(map[string]any)
		avgValues := make(map[string]any)
		sumValues := make(map[string]any)

		if len(nodes) > 0 {
			// Track sums and counts for averaging
			sums := make(map[string]float64)
			counts := make(map[string]int)

			for _, node := range nodes {
				// F3 masking hook: aggregate over the masked view of
				// properties. Masked numeric properties become
				// TypeString and naturally fall out of the numeric
				// branches below (no contribution to sum/min/max/avg).
				maskedProps := applyMaskingPolicyForGraphQL(p.Context, deps, node.Properties)
				for key, value := range maskedProps {
					var numValue float64
					var isNumeric bool

					switch value.Type {
					case storage.TypeInt:
						if intVal, err := value.AsInt(); err == nil {
							numValue = float64(intVal)
							isNumeric = true
						}
					case storage.TypeFloat:
						if floatVal, err := value.AsFloat(); err == nil {
							numValue = floatVal
							isNumeric = true
						}
					}

					if isNumeric {
						// Update min
						if minVal, exists := minValues[key]; !exists {
							minValues[key] = numValue
						} else {
							if numValue < minVal.(float64) {
								minValues[key] = numValue
							}
						}

						// Update max
						if maxVal, exists := maxValues[key]; !exists {
							maxValues[key] = numValue
						} else {
							if numValue > maxVal.(float64) {
								maxValues[key] = numValue
							}
						}

						// Update sum for avg and sum
						sums[key] += numValue
						counts[key]++
					}
				}
			}

			// Calculate averages and set sums
			for key, sum := range sums {
				avgValues[key] = sum / float64(counts[key])
				sumValues[key] = sum
			}
			// Note: All values are kept as float64 for GraphQL Float type compatibility
		}

		result["min"] = minValues
		result["max"] = maxValues
		result["avg"] = avgValues
		result["sum"] = sumValues

		return result, nil
	}
}

// createEdgeAggregateResolver creates a resolver for edge aggregations
func createEdgeAggregateResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Audit A6c-graphql-resolvers: tenant-scoped edge enumeration
		// (replaces "1..stats.EdgeCount via GetEdge" cross-tenant scan).
		tenantID := tenant.MustFromContext(p.Context)
		edges := gs.GetAllEdgesForTenant(tenantID)

		result := make(map[string]any)

		// Count
		result["count"] = len(edges)

		// Aggregate weight
		minWeight := make(map[string]any)
		maxWeight := make(map[string]any)
		avgWeight := make(map[string]any)
		sumWeight := make(map[string]any)

		if len(edges) > 0 {
			var min, max, sum float64
			min = edges[0].Weight
			max = edges[0].Weight
			sum = 0.0

			for _, edge := range edges {
				if edge.Weight < min {
					min = edge.Weight
				}
				if edge.Weight > max {
					max = edge.Weight
				}
				sum += edge.Weight
			}

			minWeight["weight"] = min
			maxWeight["weight"] = max
			avgWeight["weight"] = sum / float64(len(edges))
			sumWeight["weight"] = sum
		}

		result["min"] = minWeight
		result["max"] = maxWeight
		result["avg"] = avgWeight
		result["sum"] = sumWeight

		return result, nil
	}
}
