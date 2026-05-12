package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// GenerateSchemaWithEdges generates a GraphQL schema with edge
// traversal capabilities (tenant-blind). API callers should use
// GenerateSchemaWithEdgesForTenant per audit A9 (#36).
//
// Masking is disabled (deps = nil).
func GenerateSchemaWithEdges(gs storage.Storage) (graphql.Schema, error) {
	return generateSchemaWithEdgesForLabels(gs, gs.GetAllLabels(), nil)
}

// GenerateSchemaWithEdgesForTenant scopes the schema's type registry
// to one tenant's labels. Audit A9 (2026-05-08) — closes the
// introspection metadata leak.
//
// deps is the F3 masking hookup; nil disables masking.
func GenerateSchemaWithEdgesForTenant(gs storage.Storage, tenantID string, deps *MaskingDeps) (graphql.Schema, error) {
	return generateSchemaWithEdgesForLabels(gs, gs.GetLabelsForTenant(tenantID), deps)
}

// generateSchemaWithEdgesForLabels is the shared body. Caller picks
// the label source.
func generateSchemaWithEdgesForLabels(gs storage.Storage, labels []string, deps *MaskingDeps) (graphql.Schema, error) {
	// Create edge type (shared across schema)
	edgeType := createEdgeType()

	// Create GraphQL types for each node label with edge traversal
	nodeTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		nodeTypes[label] = createNodeTypeWithEdges(label, edgeType, gs, deps)
	}

	// Create Query type
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (any, error) {
				return "ok", nil
			},
		},
		// Edge queries
		"edge": &graphql.Field{
			Type: edgeType,
			Args: graphql.FieldConfigArgument{
				"id": &graphql.ArgumentConfig{
					Type: graphql.NewNonNull(graphql.ID),
				},
			},
			Resolve: createEdgeResolver(gs),
		},
		"edges": &graphql.Field{
			Type: graphql.NewList(edgeType),
			Args: graphql.FieldConfigArgument{
				"limit": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"offset": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
			},
			Resolve: createEdgesResolver(gs),
		},
	}

	// Add singular and plural queries for each label
	for _, label := range labels {
		nodeType := nodeTypes[label]

		// Singular query
		singularName := strings.ToLower(label)
		queryFields[singularName] = &graphql.Field{
			Type: nodeType,
			Args: graphql.FieldConfigArgument{
				"id": &graphql.ArgumentConfig{
					Type: graphql.NewNonNull(graphql.ID),
				},
			},
			Resolve: createNodeResolver(gs, label),
		}

		// Plural query
		pluralName := strings.ToLower(label) + "s"
		queryFields[pluralName] = &graphql.Field{
			Type: graphql.NewList(nodeType),
			Args: graphql.FieldConfigArgument{
				"limit": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"offset": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
			},
			Resolve: createNodesResolver(gs, label),
		}
	}

	// Create Query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	// Mutation type assembled in mutation_type.go so limits.go can reuse it.
	mutationType := buildMutationType(gs, edgeType, deps)

	// Create schema
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query:    queryType,
		Mutation: mutationType,
	})

	if err != nil {
		return graphql.Schema{}, fmt.Errorf("failed to create schema: %w", err)
	}

	return schema, nil
}
