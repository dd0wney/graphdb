package graphql

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// createEdgeType creates a GraphQL type for edges
func createEdgeType() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Edge",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return fmt.Sprintf("%d", edge.ID), nil
					}
					return nil, nil
				},
			},
			"fromNodeId": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return fmt.Sprintf("%d", edge.FromNodeID), nil
					}
					return nil, nil
				},
			},
			"toNodeId": &graphql.Field{
				Type: graphql.NewNonNull(graphql.ID),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return fmt.Sprintf("%d", edge.ToNodeID), nil
					}
					return nil, nil
				},
			},
			"type": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return edge.Type, nil
					}
					return nil, nil
				},
			},
			"weight": &graphql.Field{
				Type: graphql.Float,
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if edge, ok := p.Source.(*storage.Edge); ok {
						return edge.Weight, nil
					}
					return nil, nil
				},
			},
			"properties": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (any, error) {
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
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return gs.GetOutgoingEdges(node.ID)
					}
					return nil, nil
				},
			},
			"incomingEdges": &graphql.Field{
				Type: graphql.NewList(edgeType),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					if node, ok := p.Source.(*storage.Node); ok {
						return gs.GetIncomingEdges(node.ID)
					}
					return nil, nil
				},
			},
		},
	})
}
