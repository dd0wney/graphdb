package graphql

import (
	"encoding/json"
	"fmt"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// GenerateSchemaWithSearch creates a GraphQL schema with full-text search.
//
// deps is the F3 masking hookup; nil disables masking on the search-result
// node properties (the result type contains the matched node's full
// property bag JSON-encoded; masking flows through there).
func GenerateSchemaWithSearch(gs storage.Storage, searchIndex *search.FullTextIndex, deps *MaskingDeps) (graphql.Schema, error) {
	// Create a simple SearchResult type
	searchResultType := graphql.NewObject(graphql.ObjectConfig{
		Name: "SearchResult",
		Fields: graphql.Fields{
			"score": &graphql.Field{
				Type: graphql.Float,
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if result, ok := p.Source.(map[string]any); ok {
						return result["score"], nil
					}
					return 0.0, nil
				},
			},
			"node": &graphql.Field{
				Type: graphql.NewObject(graphql.ObjectConfig{
					Name: "Node",
					Fields: graphql.Fields{
						"id": &graphql.Field{
							Type: graphql.String,
						},
						"labels": &graphql.Field{
							Type: graphql.NewList(graphql.String),
						},
						"properties": &graphql.Field{
							Type: graphql.String, // JSON string
						},
					},
				}),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if result, ok := p.Source.(map[string]any); ok {
						if node, ok := result["node"].(*storage.Node); ok {
							// F3 masking hook: search results respect
							// the same per-tenant policy as direct reads.
							maskedProps := applyMaskingPolicyForGraphQL(p.Context, deps, node.Properties)
							propsJSON, err := json.Marshal(maskedProps)
							if err != nil {
								return nil, fmt.Errorf("failed to marshal node properties: %w", err)
							}
							return map[string]any{
								"id":         fmt.Sprintf("%d", node.ID),
								"labels":     node.Labels,
								"properties": string(propsJSON),
							}, nil
						}
					}
					return nil, nil
				},
			},
		},
	})

	// Create query type with search fields
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"search": &graphql.Field{
				Type: graphql.NewList(searchResultType),
				Args: graphql.FieldConfigArgument{
					"query": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					query, _ := p.Args["query"].(string)
					results, err := searchIndex.Search(query)
					if err != nil {
						return nil, err
					}

					gqlResults := make([]map[string]any, len(results))
					for i, result := range results {
						gqlResults[i] = map[string]any{
							"score": result.Score,
							"node":  result.Node,
						}
					}
					return gqlResults, nil
				},
			},
			"searchPhrase": &graphql.Field{
				Type: graphql.NewList(searchResultType),
				Args: graphql.FieldConfigArgument{
					"phrase": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					phrase, _ := p.Args["phrase"].(string)
					results, err := searchIndex.SearchPhrase(phrase)
					if err != nil {
						return nil, err
					}

					gqlResults := make([]map[string]any, len(results))
					for i, result := range results {
						gqlResults[i] = map[string]any{
							"score": result.Score,
							"node":  result.Node,
						}
					}
					return gqlResults, nil
				},
			},
			"searchBoolean": &graphql.Field{
				Type: graphql.NewList(searchResultType),
				Args: graphql.FieldConfigArgument{
					"query": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.String),
					},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) {
					query, _ := p.Args["query"].(string)
					results, err := searchIndex.SearchBoolean(query)
					if err != nil {
						return nil, err
					}

					gqlResults := make([]map[string]any, len(results))
					for i, result := range results {
						gqlResults[i] = map[string]any{
							"score": result.Score,
							"node":  result.Node,
						}
					}
					return gqlResults, nil
				},
			},
		},
	})

	return graphql.NewSchema(graphql.SchemaConfig{
		Query: queryType,
	})
}
