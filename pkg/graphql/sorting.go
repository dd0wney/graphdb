package graphql

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// OrderByInput represents sorting criteria
type OrderByInput struct {
	Field     string
	Direction string
}

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
