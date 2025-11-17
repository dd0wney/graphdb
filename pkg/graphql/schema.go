package graphql

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// GenerateSchema generates a GraphQL schema from the storage layer
func GenerateSchema(gs *storage.GraphStorage) (graphql.Schema, error) {
	// Discover all node labels and edge types
	labels := gs.GetAllLabels()

	// Create GraphQL types for each node label
	nodeTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		nodeTypes[label] = createNodeType(label)
	}

	// Create Query type with fields for each label
	queryFields := graphql.Fields{
		// Always include a health check query
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return "ok", nil
			},
		},
	}

	// Add singular and plural queries for each label
	for _, label := range labels {
		nodeType := nodeTypes[label]

		// Singular query: person(id: ID!): Person
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

		// Plural query: persons: [Person]
		pluralName := strings.ToLower(label) + "s"
		queryFields[pluralName] = &graphql.Field{
			Type: graphql.NewList(nodeType),
			Resolve: createNodesResolver(gs, label),
		}
	}

	// Create Query type
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

// createNodeType creates a GraphQL Object type for a node label
func createNodeType(label string) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: label,
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return node.ID, nil
					}
					return nil, nil
				},
			},
			"labels": &graphql.Field{
				Type: graphql.NewList(graphql.String),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return node.Labels, nil
					}
					return nil, nil
				},
			},
			// Properties will be dynamically resolved
			"properties": &graphql.Field{
				Type: graphql.String, // JSON string for now
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						// Convert properties to JSON-like string
						props := "{"
						first := true
						for k, v := range node.Properties {
							if !first {
								props += ", "
							}
							first = false

							// Format value based on type
							var valStr string
							switch v.Type {
							case storage.TypeString:
								s, _ := v.AsString()
								valStr = fmt.Sprintf("\"%s\"", s)
							case storage.TypeInt:
								i, _ := v.AsInt()
								valStr = fmt.Sprintf("%d", i)
							case storage.TypeFloat:
								f, _ := v.AsFloat()
								valStr = fmt.Sprintf("%f", f)
							case storage.TypeBool:
								b, _ := v.AsBool()
								valStr = fmt.Sprintf("%t", b)
							default:
								valStr = "null"
							}

							props += fmt.Sprintf("\"%s\": %s", k, valStr)
						}
						props += "}"
						return props, nil
					}
					return nil, nil
				},
			},
		},
	})
}

// createNodeResolver creates a resolver for fetching a single node by ID
func createNodeResolver(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get ID argument
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		// Convert string ID to uint64
		var id uint64
		fmt.Sscanf(idStr, "%d", &id)

		// Fetch node from storage
		node, err := gs.GetNode(id)
		if err != nil {
			return nil, err
		}

		// Check if node has the requested label
		if !node.HasLabel(label) {
			return nil, nil
		}

		return node, nil
	}
}

// createNodesResolver creates a resolver for fetching all nodes with a label
func createNodesResolver(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Query nodes by label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		// Apply pagination if specified
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		// Apply offset
		if offsetOk && offset > 0 {
			if offset >= len(nodes) {
				return []*storage.Node{}, nil
			}
			nodes = nodes[offset:]
		}

		// Apply limit
		if limitOk && limit >= 0 {
			if limit < len(nodes) {
				nodes = nodes[:limit]
			}
		}

		return nodes, nil
	}
}

// GenerateSchemaWithMutations generates a GraphQL schema with query and mutation support
