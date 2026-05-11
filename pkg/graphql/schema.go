package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// GenerateSchema generates a GraphQL schema from the storage layer
// (tenant-blind — discovers labels across every tenant). Used by CLI
// and single-tenant deployments. API callers should use
// GenerateSchemaForTenant so introspection (`__schema`) doesn't leak
// foreign-tenant label names — see audit A9 (#36).
//
// Masking is disabled (deps = nil). CLI-mode builds don't need
// per-tenant masking — single-tenant deployments have no tenant
// boundary to mask across.
func GenerateSchema(gs *storage.GraphStorage) (graphql.Schema, error) {
	return generateSchemaWithLabels(gs, gs.GetAllLabels(), nil)
}

// GenerateSchemaForTenant generates a GraphQL schema scoped to the
// given tenant's labels. Audit A9 (2026-05-08): closes the
// introspection metadata leak where a tenant-A caller running
// `{ __schema { types } }` would see every other tenant's labels.
//
// Resolver closures inside the schema already extract tenantID from
// p.Context via A6c-graphql-resolvers (#24), so query-result
// scoping was already correct; this fix is purely about the
// introspection / type-registry surface.
//
// deps is the per-server masking hookup; nil disables masking.
// Production callers pass non-nil deps; tests pass nil.
func GenerateSchemaForTenant(gs *storage.GraphStorage, tenantID string, deps *MaskingDeps) (graphql.Schema, error) {
	return generateSchemaWithLabels(gs, gs.GetLabelsForTenant(tenantID), deps)
}

// generateSchemaWithLabels is the shared schema-build core. The
// caller picks the label source (tenant-blind GetAllLabels vs
// tenant-scoped GetLabelsForTenant); this function builds the type
// registry and resolvers from that list.
func generateSchemaWithLabels(gs *storage.GraphStorage, labels []string, deps *MaskingDeps) (graphql.Schema, error) {
	// Create GraphQL types for each node label
	nodeTypes := make(map[string]*graphql.Object)
	for _, label := range labels {
		nodeTypes[label] = createNodeType(label, deps)
	}

	// Create Query type with fields for each label
	queryFields := graphql.Fields{
		// Always include a health check query
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
			Type:    graphql.NewList(nodeType),
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

// createNodeType creates a GraphQL Object type for a node label.
//
// deps is the per-server masking-policy hookup. Nil means no masking
// (CLI builds, tests, schema variants that aren't on the production
// API path). The resolver closure captures deps; per-request policy
// lookup happens inside applyMaskingPolicyForGraphQL via the request
// context's tenant.
func createNodeType(label string, deps *MaskingDeps) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: label,
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return node.ID, nil
					}
					return nil, nil
				},
			},
			"labels": &graphql.Field{
				Type: graphql.NewList(graphql.String),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return node.Labels, nil
					}
					return nil, nil
				},
			},
			// Properties will be dynamically resolved
			"properties": &graphql.Field{
				Type: graphql.String, // JSON string for now
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						// F3 masking hook: apply the tenant's masking
						// policy to a copy of node.Properties before
						// serializing. Nil deps / no policy → pass-through.
						maskedProps := applyMaskingPolicyForGraphQL(p.Context, deps, node.Properties)

						// Convert properties to JSON-like string
						props := "{"
						first := true
						for k, v := range maskedProps {
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
	return func(p graphql.ResolveParams) (any, error) {
		// Get ID argument
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		// Convert string ID to uint64
		var id uint64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", idStr, err)
		}

		// Audit A6c-graphql-resolvers: tenant-scoped fetch.
		tenantID := tenant.MustFromContext(p.Context)
		node, err := gs.GetNodeForTenant(id, tenantID)
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
	return func(p graphql.ResolveParams) (any, error) {
		// Audit A6c-graphql-resolvers: tenant-scoped label query.
		tenantID := tenant.MustFromContext(p.Context)
		nodes := gs.GetNodesByLabelForTenant(tenantID, label)

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
