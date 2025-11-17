package graphql

import (
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
			Type:    graphql.NewList(edgeType),
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
			Type:    graphql.NewList(nodeType),
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
