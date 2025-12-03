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
		nodeType, aggregateType := buildNodeAggregateTypes(gs, label)
		nodeTypes[label] = nodeType
		aggregateTypes[label] = aggregateType
	}

	// Create edge aggregate type
	edgeAggregateType := buildEdgeAggregateType()

	// Create query fields
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (any, error) {
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
