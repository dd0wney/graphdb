package graphql

import (
	"encoding/base64"
	"encoding/json"
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
func GenerateSchemaWithMutations(gs *storage.GraphStorage) (graphql.Schema, error) {
	// Discover all node labels
	labels := gs.GetAllLabels()

	// Create GraphQL types for each node label
	nodeTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		nodeTypes[label] = createNodeType(label)
	}

	// Create Query type with fields for each label
	queryFields := graphql.Fields{
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
			Resolve: createNodesResolver(gs, label),
		}
	}

	// Create Query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	// Create shared types for mutations (created once, reused multiple times)
	mutationNodeType := createGenericNodeType()
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

// createGenericNodeType creates a generic MutationNode type for mutation responses
func createGenericNodeType() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "MutationNode",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return fmt.Sprintf("%d", node.ID), nil
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
			"properties": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						props := "{"
						first := true
						for k, v := range node.Properties {
							if !first {
								props += ", "
							}
							first = false

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

// createDeleteResultType creates a type for delete mutation response
func createDeleteResultType() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "DeleteResult",
		Fields: graphql.Fields{
			"success": &graphql.Field{
				Type: graphql.Boolean,
			},
			"id": &graphql.Field{
				Type: graphql.ID,
			},
		},
	})
}

// createNodeMutationResolver creates a resolver for createNode mutation
func createNodeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get labels argument
		labelsArg, ok := p.Args["labels"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("labels argument is required")
		}

		// Convert to string slice
		labels := make([]string, len(labelsArg))
		for i, label := range labelsArg {
			labels[i] = label.(string)
		}

		// Get properties argument
		propertiesJSON, ok := p.Args["properties"].(string)
		if !ok {
			return nil, fmt.Errorf("properties argument is required")
		}

		// Parse properties JSON
		var propsMap map[string]interface{}
		if err := json.Unmarshal([]byte(propertiesJSON), &propsMap); err != nil {
			return nil, fmt.Errorf("invalid properties JSON: %w", err)
		}

		// Convert to storage.Value map
		properties := make(map[string]storage.Value)
		for k, v := range propsMap {
			properties[k] = convertToStorageValue(v)
		}

		// Create node in storage
		node, err := gs.CreateNode(labels, properties)
		if err != nil {
			return nil, fmt.Errorf("failed to create node: %w", err)
		}

		return node, nil
	}
}

// updateNodeMutationResolver creates a resolver for updateNode mutation
func updateNodeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get ID argument
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		// Convert string ID to uint64
		var id uint64
		fmt.Sscanf(idStr, "%d", &id)

		// Get properties argument
		propertiesJSON, ok := p.Args["properties"].(string)
		if !ok {
			return nil, fmt.Errorf("properties argument is required")
		}

		// Parse properties JSON
		var propsMap map[string]interface{}
		if err := json.Unmarshal([]byte(propertiesJSON), &propsMap); err != nil {
			return nil, fmt.Errorf("invalid properties JSON: %w", err)
		}

		// Convert to storage.Value map
		properties := make(map[string]storage.Value)
		for k, v := range propsMap {
			properties[k] = convertToStorageValue(v)
		}

		// Update node in storage
		if err := gs.UpdateNode(id, properties); err != nil {
			return nil, fmt.Errorf("node not found: %w", err)
		}

		// Fetch and return updated node
		node, err := gs.GetNode(id)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve updated node: %w", err)
		}

		return node, nil
	}
}

// deleteNodeMutationResolver creates a resolver for deleteNode mutation
func deleteNodeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get ID argument
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		// Convert string ID to uint64
		var id uint64
		fmt.Sscanf(idStr, "%d", &id)

		// Delete node from storage
		if err := gs.DeleteNode(id); err != nil {
			return nil, fmt.Errorf("node not found: %w", err)
		}

		// Return success result
		return map[string]interface{}{
			"success": true,
			"id":      idStr,
		}, nil
	}
}

// convertToStorageValue converts a Go interface{} to storage.Value
func convertToStorageValue(v interface{}) storage.Value {
	switch val := v.(type) {
	case string:
		return storage.StringValue(val)
	case int:
		return storage.IntValue(int64(val))
	case int64:
		return storage.IntValue(val)
	case float64:
		// JSON numbers are always float64, but if it's a whole number,
		// store it as an int for better type compatibility
		if val == float64(int64(val)) {
			return storage.IntValue(int64(val))
		}
		return storage.FloatValue(val)
	case bool:
		return storage.BoolValue(val)
	default:
		return storage.StringValue(fmt.Sprintf("%v", val))
	}
}

// GenerateSchemaWithEdges generates a GraphQL schema with query, mutation, and edge support
func GenerateSchemaWithEdges(gs *storage.GraphStorage) (graphql.Schema, error) {
	// Discover all node labels
	labels := gs.GetAllLabels()

	// Create edge type (shared across schema)
	edgeType := createEdgeType()

	// Create GraphQL types for each node label with edge traversal
	nodeTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		nodeTypes[label] = createNodeTypeWithEdges(label, edgeType, gs)
	}

	// Create Query type
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
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

	// Create shared types for mutations
	mutationNodeType := createGenericNodeType()
	deleteResultType := createDeleteResultType()

	// Create Mutation type
	mutationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			// Node mutations
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
			// Edge mutations
			"createEdge": &graphql.Field{
				Type: edgeType,
				Args: graphql.FieldConfigArgument{
					"fromNodeId": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"toNodeId": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"type": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
					"properties": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"weight": &graphql.ArgumentConfig{
						Type: graphql.Float,
					},
				},
				Resolve: createEdgeMutationResolver(gs),
			},
			"updateEdge": &graphql.Field{
				Type: edgeType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"properties": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"weight": &graphql.ArgumentConfig{
						Type: graphql.Float,
					},
				},
				Resolve: updateEdgeMutationResolver(gs),
			},
			"deleteEdge": &graphql.Field{
				Type: deleteResultType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
				},
				Resolve: deleteEdgeMutationResolver(gs),
			},
		},
	})

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

// createEdgeType creates a GraphQL type for edges
func createEdgeType() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Edge",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return fmt.Sprintf("%d", edge.ID), nil
					}
					return nil, nil
				},
			},
			"fromNodeId": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return fmt.Sprintf("%d", edge.FromNodeID), nil
					}
					return nil, nil
				},
			},
			"toNodeId": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return fmt.Sprintf("%d", edge.ToNodeID), nil
					}
					return nil, nil
				},
			},
			"type": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return edge.Type, nil
					}
					return nil, nil
				},
			},
			"weight": &graphql.Field{
				Type: graphql.Float,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return edge.Weight, nil
					}
					return nil, nil
				},
			},
			"properties": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						props := "{"
						first := true
						for k, v := range edge.Properties {
							if !first {
								props += ", "
							}
							first = false

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

// createNodeTypeWithEdges creates a node type with edge traversal fields
func createNodeTypeWithEdges(label string, edgeType *graphql.Object, gs *storage.GraphStorage) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: label,
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return fmt.Sprintf("%d", node.ID), nil
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
			"properties": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						props := "{"
						first := true
						for k, v := range node.Properties {
							if !first {
								props += ", "
							}
							first = false

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
			"outgoingEdges": &graphql.Field{
				Type: graphql.NewList(edgeType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return gs.GetOutgoingEdges(node.ID)
					}
					return nil, nil
				},
			},
			"incomingEdges": &graphql.Field{
				Type: graphql.NewList(edgeType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return gs.GetIncomingEdges(node.ID)
					}
					return nil, nil
				},
			},
		},
	})
}

// createEdgeResolver creates a resolver for fetching a single edge by ID
func createEdgeResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		var id uint64
		fmt.Sscanf(idStr, "%d", &id)

		edge, err := gs.GetEdge(id)
		if err != nil {
			return nil, err
		}

		return edge, nil
	}
}

// createEdgesResolver creates a resolver for fetching all edges
func createEdgesResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		stats := gs.GetStatistics()
		edges := make([]*storage.Edge, 0)

		for edgeID := uint64(1); edgeID <= stats.EdgeCount; edgeID++ {
			edge, err := gs.GetEdge(edgeID)
			if err != nil {
				continue
			}
			edges = append(edges, edge)
		}

		// Apply pagination if specified
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		// Apply offset
		if offsetOk && offset > 0 {
			if offset >= len(edges) {
				return []*storage.Edge{}, nil
			}
			edges = edges[offset:]
		}

		// Apply limit
		if limitOk && limit >= 0 {
			if limit < len(edges) {
				edges = edges[:limit]
			}
		}

		return edges, nil
	}
}

// createEdgeMutationResolver creates a resolver for createEdge mutation
func createEdgeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get arguments
		fromNodeIDStr, ok := p.Args["fromNodeId"].(string)
		if !ok {
			return nil, fmt.Errorf("fromNodeId argument is required")
		}

		toNodeIDStr, ok := p.Args["toNodeId"].(string)
		if !ok {
			return nil, fmt.Errorf("toNodeId argument is required")
		}

		edgeType, ok := p.Args["type"].(string)
		if !ok {
			return nil, fmt.Errorf("type argument is required")
		}

		// Parse node IDs
		var fromNodeID, toNodeID uint64
		fmt.Sscanf(fromNodeIDStr, "%d", &fromNodeID)
		fmt.Sscanf(toNodeIDStr, "%d", &toNodeID)

		// Get optional weight (default to 1.0)
		weight := 1.0
		if w, ok := p.Args["weight"].(float64); ok {
			weight = w
		}

		// Parse properties if provided
		properties := make(map[string]storage.Value)
		if propsJSON, ok := p.Args["properties"].(string); ok && propsJSON != "" {
			var propsMap map[string]interface{}
			if err := json.Unmarshal([]byte(propsJSON), &propsMap); err != nil {
				return nil, fmt.Errorf("invalid properties JSON: %w", err)
			}

			for k, v := range propsMap {
				properties[k] = convertToStorageValue(v)
			}
		}

		// Create edge in storage
		edge, err := gs.CreateEdge(fromNodeID, toNodeID, edgeType, properties, weight)
		if err != nil {
			return nil, fmt.Errorf("failed to create edge: %w", err)
		}

		return edge, nil
	}
}

// updateEdgeMutationResolver creates a resolver for updateEdge mutation
func updateEdgeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get ID argument
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		var id uint64
		fmt.Sscanf(idStr, "%d", &id)

		// Parse properties if provided
		var properties map[string]storage.Value
		if propsJSON, ok := p.Args["properties"].(string); ok && propsJSON != "" {
			var propsMap map[string]interface{}
			if err := json.Unmarshal([]byte(propsJSON), &propsMap); err != nil {
				return nil, fmt.Errorf("invalid properties JSON: %w", err)
			}

			properties = make(map[string]storage.Value)
			for k, v := range propsMap {
				properties[k] = convertToStorageValue(v)
			}
		}

		// Get weight if provided
		var weight *float64
		if w, ok := p.Args["weight"].(float64); ok {
			weight = &w
		}

		// Update edge in storage
		if err := gs.UpdateEdge(id, properties, weight); err != nil {
			return nil, fmt.Errorf("failed to update edge: %w", err)
		}

		// Fetch and return updated edge
		edge, err := gs.GetEdge(id)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve updated edge: %w", err)
		}

		return edge, nil
	}
}

// deleteEdgeMutationResolver creates a resolver for deleteEdge mutation
func deleteEdgeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		var id uint64
		fmt.Sscanf(idStr, "%d", &id)

		if err := gs.DeleteEdge(id); err != nil {
			return nil, fmt.Errorf("edge not found: %w", err)
		}

		return map[string]interface{}{
			"success": true,
			"id":      idStr,
		}, nil
	}
}
// Cursor-based pagination (Relay Connection Specification)

// encodeCursor encodes an index as a base64 cursor
func encodeCursor(index int) string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("cursor:%d", index)))
}

// decodeCursor decodes a base64 cursor to an index
func decodeCursor(cursor string) (int, error) {
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	var index int
	_, err = fmt.Sscanf(string(decoded), "cursor:%d", &index)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor format: %w", err)
	}

	return index, nil
}

// createPageInfoType creates the PageInfo type for connections
func createPageInfoType() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "PageInfo",
		Fields: graphql.Fields{
			"hasNextPage": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"hasPreviousPage": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"startCursor": &graphql.Field{
				Type: graphql.String,
			},
			"endCursor": &graphql.Field{
				Type: graphql.String,
			},
		},
	})
}

// createConnectionEdgeType creates an edge type for connections (cursor + node)
func createConnectionEdgeType(name string, nodeType *graphql.Object) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: name + "Edge",
		Fields: graphql.Fields{
			"cursor": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"node": &graphql.Field{
				Type: nodeType,
			},
		},
	})
}

// createGraphEdgeConnectionType creates an edge type for graph edge connections
func createGraphEdgeConnectionType(edgeType *graphql.Object) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "GraphEdgeEdge",
		Fields: graphql.Fields{
			"cursor": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"node": &graphql.Field{
				Type: edgeType,
			},
		},
	})
}

// createConnectionType creates a connection type (edges + pageInfo)
func createConnectionType(name string, edgeType *graphql.Object, pageInfoType *graphql.Object) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: name + "Connection",
		Fields: graphql.Fields{
			"edges": &graphql.Field{
				Type: graphql.NewList(edgeType),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})
}

// createNodeConnectionResolver creates a resolver for node connections with cursor pagination
func createNodeConnectionResolver(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Fetch all nodes with the label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		// Parse pagination arguments
		first, firstOk := p.Args["first"].(int)
		after, afterOk := p.Args["after"].(string)
		last, lastOk := p.Args["last"].(int)
		before, beforeOk := p.Args["before"].(string)

		// Decode after cursor if provided
		startIndex := 0
		if afterOk {
			afterIndex, err := decodeCursor(after)
			if err != nil {
				return nil, err
			}
			startIndex = afterIndex + 1
		}

		// Decode before cursor if provided
		endIndex := len(nodes)
		if beforeOk {
			beforeIndex, err := decodeCursor(before)
			if err != nil {
				return nil, err
			}
			endIndex = beforeIndex
		}

		// Apply cursors to slice
		if startIndex > len(nodes) {
			startIndex = len(nodes)
		}
		if endIndex > len(nodes) {
			endIndex = len(nodes)
		}
		if startIndex > endIndex {
			startIndex = endIndex
		}

		slicedNodes := nodes[startIndex:endIndex]

		// Apply first (forward pagination)
		if firstOk {
			if first < 0 {
				first = 0
			}
			if first < len(slicedNodes) {
				slicedNodes = slicedNodes[:first]
			}
		}

		// Apply last (backward pagination)
		if lastOk {
			if last < 0 {
				last = 0
			}
			if last < len(slicedNodes) {
				slicedNodes = slicedNodes[len(slicedNodes)-last:]
				startIndex = endIndex - last
				if startIndex < 0 {
					startIndex = 0
				}
			}
		}

		// Build edges with cursors
		edges := make([]map[string]interface{}, len(slicedNodes))
		for i, node := range slicedNodes {
			edges[i] = map[string]interface{}{
				"cursor": encodeCursor(startIndex + i),
				"node":   node,
			}
		}

		// Calculate pageInfo
		var startCursor, endCursor *string

		if len(edges) > 0 {
			start := encodeCursor(startIndex)
			end := encodeCursor(startIndex + len(slicedNodes) - 1)
			startCursor = &start
			endCursor = &end
		}

		// Has next if we didn't reach the end (calculate even if edges is empty)
		hasNextPage := startIndex+len(slicedNodes) < len(nodes)
		// Has previous if we didn't start at the beginning
		hasPreviousPage := startIndex > 0

		pageInfo := map[string]interface{}{
			"hasNextPage":     hasNextPage,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     startCursor,
			"endCursor":       endCursor,
		}

		return map[string]interface{}{
			"edges":    edges,
			"pageInfo": pageInfo,
		}, nil
	}
}

// createEdgeConnectionResolver creates a resolver for edge connections with cursor pagination
func createEdgeConnectionResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Fetch all edges
		stats := gs.GetStatistics()
		edges := make([]*storage.Edge, 0)

		for edgeID := uint64(1); edgeID <= stats.EdgeCount; edgeID++ {
			edge, err := gs.GetEdge(edgeID)
			if err != nil {
				continue
			}
			edges = append(edges, edge)
		}

		// Parse pagination arguments
		first, firstOk := p.Args["first"].(int)
		after, afterOk := p.Args["after"].(string)
		last, lastOk := p.Args["last"].(int)
		before, beforeOk := p.Args["before"].(string)

		// Decode after cursor if provided
		startIndex := 0
		if afterOk {
			afterIndex, err := decodeCursor(after)
			if err != nil {
				return nil, err
			}
			startIndex = afterIndex + 1
		}

		// Decode before cursor if provided
		endIndex := len(edges)
		if beforeOk {
			beforeIndex, err := decodeCursor(before)
			if err != nil {
				return nil, err
			}
			endIndex = beforeIndex
		}

		// Apply cursors to slice
		if startIndex > len(edges) {
			startIndex = len(edges)
		}
		if endIndex > len(edges) {
			endIndex = len(edges)
		}
		if startIndex > endIndex {
			startIndex = endIndex
		}

		slicedEdges := edges[startIndex:endIndex]

		// Apply first (forward pagination)
		if firstOk {
			if first < 0 {
				first = 0
			}
			if first < len(slicedEdges) {
				slicedEdges = slicedEdges[:first]
			}
		}

		// Apply last (backward pagination)
		if lastOk {
			if last < 0 {
				last = 0
			}
			if last < len(slicedEdges) {
				slicedEdges = slicedEdges[len(slicedEdges)-last:]
				startIndex = endIndex - last
				if startIndex < 0 {
					startIndex = 0
				}
			}
		}

		// Build edges with cursors
		connectionEdges := make([]map[string]interface{}, len(slicedEdges))
		for i, edge := range slicedEdges {
			connectionEdges[i] = map[string]interface{}{
				"cursor": encodeCursor(startIndex + i),
				"node":   edge,
			}
		}

		// Calculate pageInfo
		var startCursor, endCursor *string

		if len(connectionEdges) > 0 {
			start := encodeCursor(startIndex)
			end := encodeCursor(startIndex + len(slicedEdges) - 1)
			startCursor = &start
			endCursor = &end
		}

		// Has next if we didn't reach the end (calculate even if edges is empty)
		hasNextPage := startIndex+len(slicedEdges) < len(edges)
		// Has previous if we didn't start at the beginning
		hasPreviousPage := startIndex > 0

		pageInfo := map[string]interface{}{
			"hasNextPage":     hasNextPage,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     startCursor,
			"endCursor":       endCursor,
		}

		return map[string]interface{}{
			"edges":    connectionEdges,
			"pageInfo": pageInfo,
		}, nil
	}
}

// GenerateSchemaWithCursors generates a GraphQL schema with cursor-based pagination
func GenerateSchemaWithCursors(gs *storage.GraphStorage) (graphql.Schema, error) {
	// Discover all node labels
	labels := gs.GetAllLabels()

	// Create shared types
	pageInfoType := createPageInfoType()
	edgeType := createEdgeType()

	// Create GraphQL types for each node label with edge traversal
	nodeTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		nodeTypes[label] = createNodeTypeWithEdges(label, edgeType, gs)
	}

	// Create connection types for each label
	connectionEdgeTypes := make(map[string]*graphql.Object)
	connectionTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		connectionEdgeTypes[label] = createConnectionEdgeType(label, nodeTypes[label])
		connectionTypes[label] = createConnectionType(label, connectionEdgeTypes[label], pageInfoType)
	}

	// Create connection type for edges
	graphEdgeConnectionEdgeType := createGraphEdgeConnectionType(edgeType)
	edgesConnectionType := createConnectionType("Edges", graphEdgeConnectionEdgeType, pageInfoType)

	// Create Query type
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return "ok", nil
			},
		},
		// Edge queries (original)
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
		// Edge connection query (cursor-based)
		"edgesConnection": &graphql.Field{
			Type: edgesConnectionType,
			Args: graphql.FieldConfigArgument{
				"first": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"after": &graphql.ArgumentConfig{
					Type: graphql.String,
				},
				"last": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"before": &graphql.ArgumentConfig{
					Type: graphql.String,
				},
			},
			Resolve: createEdgeConnectionResolver(gs),
		},
	}

	// Add singular, plural, and connection queries for each label
	for _, label := range labels {
		nodeType := nodeTypes[label]
		connectionType := connectionTypes[label]

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

		// Plural query (offset-based pagination)
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

		// Connection query (cursor-based pagination)
		connectionName := strings.ToLower(label) + "sConnection"
		queryFields[connectionName] = &graphql.Field{
			Type: connectionType,
			Args: graphql.FieldConfigArgument{
				"first": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"after": &graphql.ArgumentConfig{
					Type: graphql.String,
				},
				"last": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"before": &graphql.ArgumentConfig{
					Type: graphql.String,
				},
			},
			Resolve: createNodeConnectionResolver(gs, label),
		}
	}

	// Create Query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	// Create shared types for mutations
	mutationNodeType := createGenericNodeType()
	deleteResultType := createDeleteResultType()

	// Create Mutation type
	mutationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			// Node mutations
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
			// Edge mutations
			"createEdge": &graphql.Field{
				Type: edgeType,
				Args: graphql.FieldConfigArgument{
					"fromNodeId": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"toNodeId": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"type": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
					"properties": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"weight": &graphql.ArgumentConfig{
						Type: graphql.Float,
					},
				},
				Resolve: createEdgeMutationResolver(gs),
			},
			"updateEdge": &graphql.Field{
				Type: edgeType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"properties": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"weight": &graphql.ArgumentConfig{
						Type: graphql.Float,
					},
				},
				Resolve: updateEdgeMutationResolver(gs),
			},
			"deleteEdge": &graphql.Field{
				Type: deleteResultType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
				},
				Resolve: deleteEdgeMutationResolver(gs),
			},
		},
	})

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

// Sorting support

// OrderByInput represents sorting configuration
type OrderByInput struct {
	Field     string
	Direction string
}

// createOrderByInputType creates an input type for sorting
func createOrderByInputType() *graphql.InputObject {
	return graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "OrderByInput",
		Fields: graphql.InputObjectConfigFieldMap{
			"field": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(graphql.String),
			},
			"direction": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(graphql.String),
			},
		},
	})
}

// parseOrderBy parses the orderBy argument
func parseOrderBy(args map[string]interface{}) *OrderByInput {
	orderByArg, ok := args["orderBy"]
	if !ok {
		return nil
	}

	orderByMap, ok := orderByArg.(map[string]interface{})
	if !ok {
		return nil
	}

	field, _ := orderByMap["field"].(string)
	direction, _ := orderByMap["direction"].(string)

	if field == "" || (direction != "ASC" && direction != "DESC") {
		return nil
	}

	return &OrderByInput{
		Field:     field,
		Direction: direction,
	}
}

// sortNodes sorts nodes based on OrderByInput
func sortNodes(nodes []*storage.Node, orderBy *OrderByInput) []*storage.Node {
	if orderBy == nil {
		return nodes
	}

	// Create a copy to avoid modifying the original slice
	sorted := make([]*storage.Node, len(nodes))
	copy(sorted, nodes)

	// Sort using bubble sort for simplicity (can be optimized with sort.Slice)
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			val1 := getNodePropertyValue(sorted[j], orderBy.Field)
			val2 := getNodePropertyValue(sorted[j+1], orderBy.Field)

			shouldSwap := false
			if orderBy.Direction == "ASC" {
				shouldSwap = compareValues(val1, val2) > 0
			} else {
				shouldSwap = compareValues(val1, val2) < 0
			}

			if shouldSwap {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	return sorted
}

// sortEdges sorts edges based on OrderByInput
func sortEdges(edges []*storage.Edge, orderBy *OrderByInput) []*storage.Edge {
	if orderBy == nil {
		return edges
	}

	// Create a copy to avoid modifying the original slice
	sorted := make([]*storage.Edge, len(edges))
	copy(sorted, edges)

	// Sort edges (currently only supports weight field)
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			var val1, val2 interface{}

			if orderBy.Field == "weight" {
				val1 = sorted[j].Weight
				val2 = sorted[j+1].Weight
			} else {
				// Try to get property value
				val1 = getEdgePropertyValue(sorted[j], orderBy.Field)
				val2 = getEdgePropertyValue(sorted[j+1], orderBy.Field)
			}

			shouldSwap := false
			if orderBy.Direction == "ASC" {
				shouldSwap = compareValues(val1, val2) > 0
			} else {
				shouldSwap = compareValues(val1, val2) < 0
			}

			if shouldSwap {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	return sorted
}

// getNodePropertyValue extracts a property value from a node
func getNodePropertyValue(node *storage.Node, field string) interface{} {
	if node.Properties == nil {
		return nil
	}

	value, exists := node.Properties[field]
	if !exists {
		return nil
	}

	switch value.Type {
	case storage.TypeString:
		v, _ := value.AsString()
		return v
	case storage.TypeInt:
		v, _ := value.AsInt()
		return v
	case storage.TypeFloat:
		v, _ := value.AsFloat()
		return v
	case storage.TypeBool:
		v, _ := value.AsBool()
		return v
	default:
		return nil
	}
}

// getEdgePropertyValue extracts a property value from an edge
func getEdgePropertyValue(edge *storage.Edge, field string) interface{} {
	if edge.Properties == nil {
		return nil
	}

	value, exists := edge.Properties[field]
	if !exists {
		return nil
	}

	switch value.Type {
	case storage.TypeString:
		v, _ := value.AsString()
		return v
	case storage.TypeInt:
		v, _ := value.AsInt()
		return v
	case storage.TypeFloat:
		v, _ := value.AsFloat()
		return v
	case storage.TypeBool:
		v, _ := value.AsBool()
		return v
	default:
		return nil
	}
}

// compareValues compares two values for sorting
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func compareValues(a, b interface{}) int {
	// Handle nil values
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	// Compare based on type
	switch aVal := a.(type) {
	case string:
		bVal, ok := b.(string)
		if !ok {
			return 0
		}
		if aVal < bVal {
			return -1
		} else if aVal > bVal {
			return 1
		}
		return 0
	case int64:
		bVal, ok := b.(int64)
		if !ok {
			return 0
		}
		if aVal < bVal {
			return -1
		} else if aVal > bVal {
			return 1
		}
		return 0
	case float64:
		bVal, ok := b.(float64)
		if !ok {
			return 0
		}
		if aVal < bVal {
			return -1
		} else if aVal > bVal {
			return 1
		}
		return 0
	case bool:
		bVal, ok := b.(bool)
		if !ok {
			return 0
		}
		if !aVal && bVal {
			return -1
		} else if aVal && !bVal {
			return 1
		}
		return 0
	default:
		return 0
	}
}

// createNodesResolverWithSorting creates a resolver with sorting support
func createNodesResolverWithSorting(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Query nodes by label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		nodes = sortNodes(nodes, orderBy)

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

// createEdgesResolverWithSorting creates an edge resolver with sorting support
func createEdgesResolverWithSorting(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		stats := gs.GetStatistics()
		edges := make([]*storage.Edge, 0)

		for edgeID := uint64(1); edgeID <= stats.EdgeCount; edgeID++ {
			edge, err := gs.GetEdge(edgeID)
			if err != nil {
				continue
			}
			edges = append(edges, edge)
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		edges = sortEdges(edges, orderBy)

		// Apply pagination if specified
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		// Apply offset
		if offsetOk && offset > 0 {
			if offset >= len(edges) {
				return []*storage.Edge{}, nil
			}
			edges = edges[offset:]
		}

		// Apply limit
		if limitOk && limit >= 0 {
			if limit < len(edges) {
				edges = edges[:limit]
			}
		}

		return edges, nil
	}
}

// createNodeConnectionResolverWithSorting creates a connection resolver with sorting
func createNodeConnectionResolverWithSorting(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Fetch all nodes with the label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		nodes = sortNodes(nodes, orderBy)

		// Parse pagination arguments
		first, firstOk := p.Args["first"].(int)
		after, afterOk := p.Args["after"].(string)
		last, lastOk := p.Args["last"].(int)
		before, beforeOk := p.Args["before"].(string)

		// Decode after cursor if provided
		startIndex := 0
		if afterOk {
			afterIndex, err := decodeCursor(after)
			if err != nil {
				return nil, err
			}
			startIndex = afterIndex + 1
		}

		// Decode before cursor if provided
		endIndex := len(nodes)
		if beforeOk {
			beforeIndex, err := decodeCursor(before)
			if err != nil {
				return nil, err
			}
			endIndex = beforeIndex
		}

		// Apply cursors to slice
		if startIndex > len(nodes) {
			startIndex = len(nodes)
		}
		if endIndex > len(nodes) {
			endIndex = len(nodes)
		}
		if startIndex > endIndex {
			startIndex = endIndex
		}

		slicedNodes := nodes[startIndex:endIndex]

		// Apply first (forward pagination)
		if firstOk {
			if first < 0 {
				first = 0
			}
			if first < len(slicedNodes) {
				slicedNodes = slicedNodes[:first]
			}
		}

		// Apply last (backward pagination)
		if lastOk {
			if last < 0 {
				last = 0
			}
			if last < len(slicedNodes) {
				slicedNodes = slicedNodes[len(slicedNodes)-last:]
				startIndex = endIndex - last
				if startIndex < 0 {
					startIndex = 0
				}
			}
		}

		// Build edges with cursors
		edges := make([]map[string]interface{}, len(slicedNodes))
		for i, node := range slicedNodes {
			edges[i] = map[string]interface{}{
				"cursor": encodeCursor(startIndex + i),
				"node":   node,
			}
		}

		// Calculate pageInfo
		var startCursor, endCursor *string

		if len(edges) > 0 {
			start := encodeCursor(startIndex)
			end := encodeCursor(startIndex + len(slicedNodes) - 1)
			startCursor = &start
			endCursor = &end
		}

		// Has next if we didn't reach the end (calculate even if edges is empty)
		hasNextPage := startIndex+len(slicedNodes) < len(nodes)
		// Has previous if we didn't start at the beginning
		hasPreviousPage := startIndex > 0

		pageInfo := map[string]interface{}{
			"hasNextPage":     hasNextPage,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     startCursor,
			"endCursor":       endCursor,
		}

		return map[string]interface{}{
			"edges":    edges,
			"pageInfo": pageInfo,
		}, nil
	}
}

// GenerateSchemaWithSorting generates a GraphQL schema with sorting support
func GenerateSchemaWithSorting(gs *storage.GraphStorage) (graphql.Schema, error) {
	// Discover all node labels
	labels := gs.GetAllLabels()

	// Create shared types
	pageInfoType := createPageInfoType()
	edgeType := createEdgeType()
	orderByInputType := createOrderByInputType()

	// Create GraphQL types for each node label with edge traversal
	nodeTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		nodeTypes[label] = createNodeTypeWithEdges(label, edgeType, gs)
	}

	// Create connection types for each label
	connectionEdgeTypes := make(map[string]*graphql.Object)
	connectionTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		connectionEdgeTypes[label] = createConnectionEdgeType(label, nodeTypes[label])
		connectionTypes[label] = createConnectionType(label, connectionEdgeTypes[label], pageInfoType)
	}

	// Create connection type for edges
	graphEdgeConnectionEdgeType := createGraphEdgeConnectionType(edgeType)
	edgesConnectionType := createConnectionType("Edges", graphEdgeConnectionEdgeType, pageInfoType)

	// Create Query type
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return "ok", nil
			},
		},
		// Edge queries (with sorting)
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
				"orderBy": &graphql.ArgumentConfig{
					Type: orderByInputType,
				},
			},
			Resolve: createEdgesResolverWithSorting(gs),
		},
		// Edge connection query (cursor-based with sorting)
		"edgesConnection": &graphql.Field{
			Type: edgesConnectionType,
			Args: graphql.FieldConfigArgument{
				"first": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"after": &graphql.ArgumentConfig{
					Type: graphql.String,
				},
				"last": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"before": &graphql.ArgumentConfig{
					Type: graphql.String,
				},
			},
			Resolve: createEdgeConnectionResolver(gs),
		},
	}

	// Add singular, plural, and connection queries for each label
	for _, label := range labels {
		nodeType := nodeTypes[label]
		connectionType := connectionTypes[label]

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

		// Plural query (offset-based pagination with sorting)
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
				"orderBy": &graphql.ArgumentConfig{
					Type: orderByInputType,
				},
			},
			Resolve: createNodesResolverWithSorting(gs, label),
		}

		// Connection query (cursor-based pagination with sorting)
		connectionName := strings.ToLower(label) + "sConnection"
		queryFields[connectionName] = &graphql.Field{
			Type: connectionType,
			Args: graphql.FieldConfigArgument{
				"first": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"after": &graphql.ArgumentConfig{
					Type: graphql.String,
				},
				"last": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"before": &graphql.ArgumentConfig{
					Type: graphql.String,
				},
				"orderBy": &graphql.ArgumentConfig{
					Type: orderByInputType,
				},
			},
			Resolve: createNodeConnectionResolverWithSorting(gs, label),
		}
	}

	// Create Query type
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	// Create shared types for mutations
	mutationNodeType := createGenericNodeType()
	deleteResultType := createDeleteResultType()

	// Create Mutation type
	mutationType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			// Node mutations
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
			// Edge mutations
			"createEdge": &graphql.Field{
				Type: edgeType,
				Args: graphql.FieldConfigArgument{
					"fromNodeId": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"toNodeId": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"type": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
					"properties": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"weight": &graphql.ArgumentConfig{
						Type: graphql.Float,
					},
				},
				Resolve: createEdgeMutationResolver(gs),
			},
			"updateEdge": &graphql.Field{
				Type: edgeType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
					"properties": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"weight": &graphql.ArgumentConfig{
						Type: graphql.Float,
					},
				},
				Resolve: updateEdgeMutationResolver(gs),
			},
			"deleteEdge": &graphql.Field{
				Type: deleteResultType,
				Args: graphql.FieldConfigArgument{
					"id": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.ID),
					},
				},
				Resolve: deleteEdgeMutationResolver(gs),
			},
		},
	})

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
			Type: aggregateTypes[label],
			Resolve: createNodeAggregateResolver(gs, label),
		}
	}

	// Add edges aggregate query
	queryFields["edgesAggregate"] = &graphql.Field{
		Type: edgeAggregateType,
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
