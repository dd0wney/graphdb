package graphql

import (
	"encoding/json"
	"fmt"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// createGenericNodeType creates a generic MutationNode type for mutation responses.
//
// deps is the F3 masking hookup; nil disables masking on the "properties"
// resolver. The mutation result reflects the just-created/updated node back
// to the caller, so it must respect the same per-tenant masking the read
// path applies.
func createGenericNodeType(deps *MaskingDeps) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "MutationNode",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return fmt.Sprintf("%d", node.ID), nil
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
			"properties": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						// F3 masking hook: mutation responses respect
						// the same per-tenant policy as reads.
						maskedProps := applyMaskingPolicyForGraphQL(p.Context, deps, node.Properties)
						b, err := json.Marshal(storage.PropertiesToJSON(maskedProps))
						if err != nil {
							return nil, fmt.Errorf("marshal node properties: %w", err)
						}
						return string(b), nil
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
