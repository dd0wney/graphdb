package graphql

import (
	"fmt"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
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
						props := "{"
						first := true
						for k, v := range maskedProps {
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
