package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// nodeSampler returns nodes carrying the given label, used during schema
// generation to discover which property keys become aggregate fields. It is
// injected so the tenant-scoping decision lives at the entry point where the
// tenant is known, and the generator never samples across tenant boundaries.
type nodeSampler func(label string) []*storage.Node

// GenerateSchemaWithAggregation generates a GraphQL schema with
// aggregation support (tenant-blind). API callers should use
// GenerateSchemaWithAggregationForTenant per audit A9 (#36).
//
// Masking is disabled (deps = nil). Property discovery samples across all
// tenants, which is correct for this tenant-blind / single-tenant schema.
func GenerateSchemaWithAggregation(gs *storage.GraphStorage) (graphql.Schema, error) {
	sample := func(label string) []*storage.Node {
		nodes, _ := gs.FindNodesByLabelAcrossTenants(label)
		return nodes
	}
	return generateSchemaWithAggregationForLabels(gs, gs.GetAllLabels(), nil, sample)
}

// GenerateSchemaWithAggregationForTenant scopes the schema to one
// tenant's labels. Audit A9.
//
// deps is the F3 masking hookup; nil disables masking. Property discovery is
// scoped to the requesting tenant's own nodes so another tenant's property-key
// names never surface in this tenant's schema introspection (the schema-side
// counterpart of the A6c resolver-scoping).
func GenerateSchemaWithAggregationForTenant(gs *storage.GraphStorage, tenantID string, deps *MaskingDeps) (graphql.Schema, error) {
	sample := func(label string) []*storage.Node {
		return gs.GetNodesByLabelForTenant(tenantID, label)
	}
	return generateSchemaWithAggregationForLabels(gs, gs.GetLabelsForTenant(tenantID), deps, sample)
}

func generateSchemaWithAggregationForLabels(gs *storage.GraphStorage, labels []string, deps *MaskingDeps, sample nodeSampler) (graphql.Schema, error) {
	nodeTypes := make(map[string]*graphql.Object)
	aggregateTypes := make(map[string]*graphql.Object)

	// Create node types and aggregate types for each label
	for _, label := range labels {
		nodeType, aggregateType := buildNodeAggregateTypes(label, deps, sample)
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
			Resolve: createNodeAggregateResolver(gs, label, deps),
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
