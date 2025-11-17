package graphql

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// GenerateSchemaWithAggregation generates a GraphQL schema with aggregation support
func GenerateSchemaWithAggregation(gs *storage.GraphStorage) (graphql.Schema, error) {
	// Get all unique labels
	labels := gs.GetAllLabels()

	nodeTypes := make(map[string]*graphql.Object)
	aggregateTypes := make(map[string]*graphql.Object)

	// Create node types and aggregate types for each label
	for _, label := range labels {
		// Create regular node type
		nodeTypes[label] = graphql.NewObject(graphql.ObjectConfig{
			Name: label,
			Fields: graphql.Fields{
				"id":         &graphql.Field{Type: graphql.String},
				"labels":     &graphql.Field{Type: graphql.NewList(graphql.String)},
				"properties": &graphql.Field{Type: graphql.String},
			},
		})

		// Get sample nodes to discover properties
		sampleNodes, _ := gs.FindNodesByLabel(label)
		propertyFields := graphql.Fields{}

		// Build a map of property keys to their types
		if len(sampleNodes) > 0 {
			propertyTypes := make(map[string]storage.ValueType)
			for _, node := range sampleNodes {
				for key, value := range node.Properties {
					// Store the first type we see for each property
					if _, exists := propertyTypes[key]; !exists {
						propertyTypes[key] = value.Type
					}
				}
			}

			// Create fields for min/max/sum (preserve original types)
			minMaxSumFields := graphql.Fields{}
			// Create fields for avg (always float)
			avgFields := graphql.Fields{}

			for key := range propertyTypes {
				// Use Float for all numeric types to preserve int64 precision
				minMaxSumFields[key] = &graphql.Field{Type: graphql.Float}
				avgFields[key] = &graphql.Field{Type: graphql.Float}
			}

			// If no properties found, add a dummy field to avoid empty object
			if len(minMaxSumFields) == 0 {
				minMaxSumFields["_"] = &graphql.Field{Type: graphql.String}
				avgFields["_"] = &graphql.Field{Type: graphql.String}
			}

			propertyFields = minMaxSumFields
		} else {
			// If no properties found, add a dummy field to avoid empty object
			propertyFields["_"] = &graphql.Field{Type: graphql.String}
		}

		// Create separate aggregate field types
		minFieldsType := graphql.NewObject(graphql.ObjectConfig{
			Name:   label + "AggregateMinFields",
			Fields: propertyFields,
		})
		maxFieldsType := graphql.NewObject(graphql.ObjectConfig{
			Name:   label + "AggregateMaxFields",
			Fields: propertyFields,
		})
		sumFieldsType := graphql.NewObject(graphql.ObjectConfig{
			Name:   label + "AggregateSumFields",
			Fields: propertyFields,
		})

		// Create avg fields with float type
		avgPropertyFields := graphql.Fields{}
		if len(sampleNodes) > 0 {
			propertyTypes := make(map[string]storage.ValueType)
			for _, node := range sampleNodes {
				for key, value := range node.Properties {
					if _, exists := propertyTypes[key]; !exists {
						propertyTypes[key] = value.Type
					}
				}
			}
			for key := range propertyTypes {
				avgPropertyFields[key] = &graphql.Field{Type: graphql.Float}
			}
		}
		if len(avgPropertyFields) == 0 {
			avgPropertyFields["_"] = &graphql.Field{Type: graphql.String}
		}
		avgFieldsType := graphql.NewObject(graphql.ObjectConfig{
			Name:   label + "AggregateAvgFields",
			Fields: avgPropertyFields,
		})

		// Create aggregate type
		aggregateTypes[label] = graphql.NewObject(graphql.ObjectConfig{
			Name: label + "Aggregate",
			Fields: graphql.Fields{
				"count": &graphql.Field{
					Type: graphql.Int,
				},
				"min": &graphql.Field{
					Type: minFieldsType,
				},
				"max": &graphql.Field{
					Type: maxFieldsType,
				},
				"avg": &graphql.Field{
					Type: avgFieldsType,
				},
				"sum": &graphql.Field{
					Type: sumFieldsType,
				},
			},
		})
	}

	// Create edge aggregate fields type
	edgeAggregateFieldsType := graphql.NewObject(graphql.ObjectConfig{
		Name: "EdgeAggregateFields",
		Fields: graphql.Fields{
			"weight": &graphql.Field{Type: graphql.Float},
		},
	})

	// Create edge aggregate type
	edgeAggregateType := graphql.NewObject(graphql.ObjectConfig{
		Name: "EdgeAggregate",
		Fields: graphql.Fields{
			"count": &graphql.Field{Type: graphql.Int},
			"min":   &graphql.Field{Type: edgeAggregateFieldsType},
			"max":   &graphql.Field{Type: edgeAggregateFieldsType},
			"avg":   &graphql.Field{Type: edgeAggregateFieldsType},
			"sum":   &graphql.Field{Type: edgeAggregateFieldsType},
		},
	})

	// Create query fields
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return "ok", nil
			},
		},
	}

	// Add aggregate queries for each label
	for _, label := range labels {
		lowerLabel := strings.ToLower(label)
		queryFields[lowerLabel+"sAggregate"] = &graphql.Field{
			Type:    aggregateTypes[label],
			Resolve: createNodeAggregateResolver(gs, label),
		}
	}

	// Add edges aggregate query
	queryFields["edgesAggregate"] = &graphql.Field{
		Type:    edgeAggregateType,
		Resolve: createEdgeAggregateResolver(gs),
	}

	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	// Create schema
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: queryType,
	})

	if err != nil {
		return graphql.Schema{}, fmt.Errorf("failed to create schema: %w", err)
	}

	return schema, nil
}

// createNodeAggregateResolver creates a resolver for node aggregations
func createNodeAggregateResolver(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get all nodes with this label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		result := make(map[string]interface{})

		// Count
		result["count"] = len(nodes)

		// For min, max, avg, sum - we need to aggregate over properties
		minValues := make(map[string]interface{})
		maxValues := make(map[string]interface{})
		avgValues := make(map[string]interface{})
		sumValues := make(map[string]interface{})

		if len(nodes) > 0 {
			// Track sums and counts for averaging
			sums := make(map[string]float64)
			counts := make(map[string]int)
			propertyTypes := make(map[string]storage.ValueType)

			for _, node := range nodes {
				for key, value := range node.Properties {
					var numValue float64
					var isNumeric bool

					switch value.Type {
					case storage.TypeInt:
						if intVal, err := value.AsInt(); err == nil {
							numValue = float64(intVal)
							isNumeric = true
							propertyTypes[key] = storage.TypeInt
						}
					case storage.TypeFloat:
						if floatVal, err := value.AsFloat(); err == nil {
							numValue = floatVal
							isNumeric = true
							propertyTypes[key] = storage.TypeFloat
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
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get all edges
		stats := gs.GetStatistics()
		edges := make([]*storage.Edge, 0)

		for edgeID := uint64(1); edgeID <= stats.EdgeCount; edgeID++ {
			edge, err := gs.GetEdge(edgeID)
			if err != nil {
				continue
			}
			edges = append(edges, edge)
		}

		result := make(map[string]interface{})

		// Count
		result["count"] = len(edges)

		// Aggregate weight
		minWeight := make(map[string]interface{})
		maxWeight := make(map[string]interface{})
		avgWeight := make(map[string]interface{})
		sumWeight := make(map[string]interface{})

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
