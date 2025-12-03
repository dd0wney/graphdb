package graphql

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// GenerateSchemaWithFiltering generates a GraphQL schema with filtering support
func GenerateSchemaWithFiltering(gs *storage.GraphStorage) (graphql.Schema, error) {
	labels := gs.GetAllLabels()
	nodeTypes := make(map[string]*graphql.Object)

	// Create where input type (we'll use a generic JSON-like structure)
	whereInputType := graphql.NewScalar(graphql.ScalarConfig{
		Name:        "WhereInput",
		Description: "Filter conditions for queries",
		Serialize: func(value any) any {
			return value
		},
	})

	// Create orderBy input type once
	orderByInputType := createOrderByInputType()

	// Create node types
	for _, label := range labels {
		nodeTypes[label] = createNodeType(label)
	}

	// Create edge type
	edgeType := createEdgeType()

	// Create query fields
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (any, error) {
				return "ok", nil
			},
		},
	}

	// Add node queries with filtering
	for _, label := range labels {
		nodeType := nodeTypes[label]

		// Singular query (no filtering needed)
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

		// Plural query with filtering
		pluralName := strings.ToLower(label) + "s"
		queryFields[pluralName] = &graphql.Field{
			Type: graphql.NewList(nodeType),
			Args: graphql.FieldConfigArgument{
				"where": &graphql.ArgumentConfig{
					Type: whereInputType,
				},
				"orderBy": &graphql.ArgumentConfig{
					Type: orderByInputType,
				},
				"limit": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"offset": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
			},
			Resolve: createNodesResolverWithFiltering(gs, label),
		}
	}

	// Add edge queries with filtering
	queryFields["edge"] = &graphql.Field{
		Type: edgeType,
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.ID),
			},
		},
		Resolve: createEdgeResolver(gs),
	}

	queryFields["edges"] = &graphql.Field{
		Type: graphql.NewList(edgeType),
		Args: graphql.FieldConfigArgument{
			"where": &graphql.ArgumentConfig{
				Type: whereInputType,
			},
			"orderBy": &graphql.ArgumentConfig{
				Type: orderByInputType,
			},
			"limit": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
			"offset": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
		},
		Resolve: createEdgesResolverWithFiltering(gs),
	}

	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: queryType,
	})

	if err != nil {
		return graphql.Schema{}, fmt.Errorf("failed to create schema: %w", err)
	}

	return schema, nil
}

// createNodesResolverWithFiltering creates a resolver with filtering support
func createNodesResolverWithFiltering(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Get all nodes with this label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		// Apply filtering
		filterConditions := parseWhere(p.Args)
		var filteredNodes []*storage.Node
		for _, node := range nodes {
			if evaluateFilter(node, filterConditions) {
				filteredNodes = append(filteredNodes, node)
			}
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		filteredNodes = sortNodes(filteredNodes, orderBy)

		// Apply pagination
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		if offsetOk && offset > 0 {
			if offset >= len(filteredNodes) {
				return []*storage.Node{}, nil
			}
			filteredNodes = filteredNodes[offset:]
		}

		if limitOk && limit > 0 {
			if limit < len(filteredNodes) {
				filteredNodes = filteredNodes[:limit]
			}
		}

		return filteredNodes, nil
	}
}

// createEdgesResolverWithFiltering creates an edge resolver with filtering support
func createEdgesResolverWithFiltering(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
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

		// Apply filtering
		filterConditions := parseWhere(p.Args)
		var filteredEdges []*storage.Edge
		for _, edge := range edges {
			if evaluateEdgeFilter(edge, filterConditions) {
				filteredEdges = append(filteredEdges, edge)
			}
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		filteredEdges = sortEdges(filteredEdges, orderBy)

		// Apply pagination
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		if offsetOk && offset > 0 {
			if offset >= len(filteredEdges) {
				return []*storage.Edge{}, nil
			}
			filteredEdges = filteredEdges[offset:]
		}

		if limitOk && limit > 0 {
			if limit < len(filteredEdges) {
				filteredEdges = filteredEdges[:limit]
			}
		}

		return filteredEdges, nil
	}
}
