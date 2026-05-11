package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// GenerateSchemaWithMutations generates a GraphQL schema with
// mutation support (tenant-blind). API callers should use
// GenerateSchemaWithMutationsForTenant per audit A9 (#36).
//
// Masking is disabled (deps = nil).
func GenerateSchemaWithMutations(gs *storage.GraphStorage) (graphql.Schema, error) {
	return generateSchemaWithMutationsForLabels(gs, gs.GetAllLabels(), nil)
}

// GenerateSchemaWithMutationsForTenant scopes the schema to one
// tenant's labels. Audit A9.
//
// deps is the F3 masking hookup; nil disables masking.
func GenerateSchemaWithMutationsForTenant(gs *storage.GraphStorage, tenantID string, deps *MaskingDeps) (graphql.Schema, error) {
	return generateSchemaWithMutationsForLabels(gs, gs.GetLabelsForTenant(tenantID), deps)
}

func generateSchemaWithMutationsForLabels(gs *storage.GraphStorage, labels []string, deps *MaskingDeps) (graphql.Schema, error) {
	// Create GraphQL types for each node label
	nodeTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		nodeTypes[label] = createNodeType(label, deps)
	}

	// Create Query type with fields for each label
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (any, error) {
				return "ok", nil
			},
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
			Type:    graphql.NewList(nodeType),
			Resolve: createNodesResolver(gs, label),
		}
	}

	// Create Query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	// Create shared types for mutations (created once, reused multiple times)
	mutationNodeType := createGenericNodeType(deps)
	deleteResultType := createDeleteResultType()

	// Create Mutation type
	mutationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			"createNode": &graphql.Field{
				Type: mutationNodeType,
				Args: graphql.FieldConfigArgument{
					"labels": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.NewList(graphql.String)),
					},
					"properties": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
				},
				Resolve: createNodeMutationResolver(gs),
			},
			"updateNode": &graphql.Field{
				Type: mutationNodeType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"properties": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
				},
				Resolve: updateNodeMutationResolver(gs),
			},
			"deleteNode": &graphql.Field{
				Type: deleteResultType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
				},
				Resolve: deleteNodeMutationResolver(gs),
			},
		},
	})

	// Create schema with both query and mutation
	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query:    queryType,
		Mutation: mutationType,
	})

	if err != nil {
		return graphql.Schema{}, fmt.Errorf("failed to create schema: %w", err)
	}

	return schema, nil
}
