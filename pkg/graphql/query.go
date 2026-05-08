package graphql

import (
	"context"

	"github.com/graphql-go/graphql"
)

// ExecuteQuery executes a GraphQL query against a schema. The ctx is
// threaded into graphql.Params so resolvers can read it via
// p.Context — used by audit A6c-graphql-resolvers (the next PR) to
// extract the caller's tenant ID.
//
// Audit A6c-graphql-ctx (2026-05-08): pre-fix, this function dropped
// the request context entirely; resolvers ran with
// context.Background(), so JWT-derived tenant scoping was invisible
// to GraphQL.
func ExecuteQuery(ctx context.Context, query string, schema graphql.Schema) *graphql.Result {
	params := graphql.Params{
		Schema:        schema,
		RequestString: query,
		Context:       ctx,
	}

	return graphql.Do(params)
}

// ExecuteQueryWithVariables executes a GraphQL query with variables.
// See ExecuteQuery for the rationale on the ctx parameter.
func ExecuteQueryWithVariables(ctx context.Context, query string, schema graphql.Schema, variables map[string]any) *graphql.Result {
	params := graphql.Params{
		Schema:         schema,
		RequestString:  query,
		VariableValues: variables,
		Context:        ctx,
	}

	return graphql.Do(params)
}
