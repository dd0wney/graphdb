package graphql

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

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

