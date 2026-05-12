package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// LimitConfig defines limits for query results
type LimitConfig struct {
	DefaultLimit int // Default limit when no limit specified
	MaxLimit     int // Maximum allowed limit
}

// ValidateLimitConfig validates the limit configuration
func ValidateLimitConfig(config *LimitConfig) error {
	if config.MaxLimit <= 0 {
		return fmt.Errorf("max limit must be greater than 0, got %d", config.MaxLimit)
	}
	if config.DefaultLimit > config.MaxLimit {
		return fmt.Errorf("default limit (%d) cannot exceed max limit (%d)", config.DefaultLimit, config.MaxLimit)
	}
	if config.DefaultLimit <= 0 {
		return fmt.Errorf("default limit must be greater than 0, got %d", config.DefaultLimit)
	}
	return nil
}

// applyLimit applies default and max limit constraints to a limit value
func applyLimit(requestedLimit int, config *LimitConfig) int {
	// If no limit specified or negative, use default
	if requestedLimit < 0 {
		return config.DefaultLimit
	}

	// If limit is 0, return 0 (empty results)
	if requestedLimit == 0 {
		return 0
	}

	// Cap at max limit
	if requestedLimit > config.MaxLimit {
		return config.MaxLimit
	}

	return requestedLimit
}

// GenerateSchemaWithLimits generates a GraphQL schema with filtering
// and result limits (tenant-blind). API callers should use
// GenerateSchemaWithLimitsForTenant per audit A9 (#36).
//
// Masking is disabled (deps = nil); use GenerateSchemaWithLimitsForTenant
// for the production path that needs per-tenant masking.
func GenerateSchemaWithLimits(gs storage.Storage, config *LimitConfig) (graphql.Schema, error) {
	if err := ValidateLimitConfig(config); err != nil {
		return graphql.Schema{}, err
	}
	return generateSchemaWithLimitsForLabels(gs, config, gs.GetAllLabels(), nil)
}

// GenerateSchemaWithLimitsForTenant scopes the schema's type
// registry to one tenant's labels. Audit A9 (2026-05-08).
//
// deps is the F3 masking hookup; nil disables masking. The
// pkg/api server passes the server's PolicyStore + Masker.
func GenerateSchemaWithLimitsForTenant(gs storage.Storage, config *LimitConfig, tenantID string, deps *MaskingDeps) (graphql.Schema, error) {
	if err := ValidateLimitConfig(config); err != nil {
		return graphql.Schema{}, err
	}
	return generateSchemaWithLimitsForLabels(gs, config, gs.GetLabelsForTenant(tenantID), deps)
}

func generateSchemaWithLimitsForLabels(gs storage.Storage, config *LimitConfig, labels []string, deps *MaskingDeps) (graphql.Schema, error) {
	nodeTypes := make(map[string]*graphql.Object)

	// Create where input type
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
		nodeTypes[label] = createNodeType(label, deps)
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

	// Add node queries with filtering and limits
	for _, label := range labels {
		nodeType := nodeTypes[label]

		// Singular query (no filtering/limits needed)
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

		// Plural query with filtering and limits
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
			Resolve: createNodesResolverWithLimits(gs, label, config),
		}
	}

	// Add edge queries with filtering and limits
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
		Resolve: createEdgesResolverWithLimits(gs, config),
	}

	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	// Mount the same mutation set the edges_schema.go uses. Without
	// this, cmd/server's GraphQL endpoint exposes only queries, which
	// makes the createNode/createEdge resolvers — and the B-lite
	// :Claim uniqueness check that piggy-backs on createNode —
	// unreachable from the live server. (H4.2.)
	mutationType := buildMutationType(gs, edgeType, deps)

	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query:    queryType,
		Mutation: mutationType,
	})

	if err != nil {
		return graphql.Schema{}, fmt.Errorf("failed to create schema: %w", err)
	}

	return schema, nil
}

// createNodesResolverWithLimits creates a resolver with filtering and limit enforcement
func createNodesResolverWithLimits(gs storage.Storage, label string, config *LimitConfig) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Audit A6c-graphql-resolvers: tenant-scoped label query.
		tenantID := tenant.MustFromContext(p.Context)
		nodes := gs.GetNodesByLabelForTenant(tenantID, label)

		// Apply filtering
		filterExpr := parseWhere(p.Args)
		var filteredNodes []*storage.Node
		for _, node := range nodes {
			if evaluateFilter(node, filterExpr) {
				filteredNodes = append(filteredNodes, node)
			}
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		filteredNodes = sortNodes(filteredNodes, orderBy)

		// Apply offset
		offset, offsetOk := p.Args["offset"].(int)
		if offsetOk && offset > 0 {
			if offset >= len(filteredNodes) {
				return []*storage.Node{}, nil
			}
			filteredNodes = filteredNodes[offset:]
		}

		// Apply limit with enforcement
		requestedLimit := -1 // Default to no limit specified
		if limit, ok := p.Args["limit"].(int); ok {
			requestedLimit = limit
		}

		effectiveLimit := applyLimit(requestedLimit, config)

		if effectiveLimit == 0 {
			return []*storage.Node{}, nil
		}

		if effectiveLimit > 0 && effectiveLimit < len(filteredNodes) {
			filteredNodes = filteredNodes[:effectiveLimit]
		}

		return filteredNodes, nil
	}
}

// createEdgesResolverWithLimits creates an edge resolver with filtering and limit enforcement
func createEdgesResolverWithLimits(gs storage.Storage, config *LimitConfig) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Audit A6c-graphql-resolvers: tenant-scoped enumeration.
		tenantID := tenant.MustFromContext(p.Context)
		edges := gs.GetAllEdgesForTenant(tenantID)

		// Apply filtering
		filterExpr := parseWhere(p.Args)
		var filteredEdges []*storage.Edge
		for _, edge := range edges {
			if evaluateEdgeFilter(edge, filterExpr) {
				filteredEdges = append(filteredEdges, edge)
			}
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		filteredEdges = sortEdges(filteredEdges, orderBy)

		// Apply offset
		offset, offsetOk := p.Args["offset"].(int)
		if offsetOk && offset > 0 {
			if offset >= len(filteredEdges) {
				return []*storage.Edge{}, nil
			}
			filteredEdges = filteredEdges[offset:]
		}

		// Apply limit with enforcement
		requestedLimit := -1 // Default to no limit specified
		if limit, ok := p.Args["limit"].(int); ok {
			requestedLimit = limit
		}

		effectiveLimit := applyLimit(requestedLimit, config)

		if effectiveLimit == 0 {
			return []*storage.Edge{}, nil
		}

		if effectiveLimit > 0 && effectiveLimit < len(filteredEdges) {
			filteredEdges = filteredEdges[:effectiveLimit]
		}

		return filteredEdges, nil
	}
}
