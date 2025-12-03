package graphql

import (
	"github.com/graphql-go/graphql"
)

// ExecuteQuery executes a GraphQL query against a schema
func ExecuteQuery(query string, schema graphql.Schema) *graphql.Result {
	params := graphql.Params{
		Schema:        schema,
		RequestString: query,
	}

	result := graphql.Do(params)
	return result
}

// ExecuteQueryWithVariables executes a GraphQL query with variables
func ExecuteQueryWithVariables(query string, schema graphql.Schema, variables map[string]any) *graphql.Result {
	params := graphql.Params{
		Schema:         schema,
		RequestString:  query,
		VariableValues: variables,
	}

	result := graphql.Do(params)
	return result
}
