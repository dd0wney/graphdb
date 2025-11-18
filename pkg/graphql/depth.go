package graphql

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
)

// DepthLimitedSchema wraps a schema with automatic depth validation
type DepthLimitedSchema struct {
	schema   graphql.Schema
	maxDepth int
}

// GenerateSchemaWithDepthLimit generates a GraphQL schema with query depth limiting
func GenerateSchemaWithDepthLimit(gs *storage.GraphStorage, maxDepth int) (graphql.Schema, error) {
	// Validate depth limit
	if maxDepth <= 0 {
		return graphql.Schema{}, fmt.Errorf("max depth must be greater than 0, got %d", maxDepth)
	}

	// Use existing filtering schema as base
	baseSchema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		return graphql.Schema{}, err
	}

	// Create depth-limited wrapper
	wrapper := &DepthLimitedSchema{
		schema:   baseSchema,
		maxDepth: maxDepth,
	}

	// Store wrapper for Do function interception
	RegisterDepthLimitedSchema(wrapper)

	return baseSchema, nil
}

// Global registry for depth-limited schemas
var depthLimitRegistry = make(map[*graphql.Schema]*DepthLimitedSchema)

// RegisterDepthLimitedSchema registers a depth-limited schema
func RegisterDepthLimitedSchema(wrapper *DepthLimitedSchema) {
	depthLimitRegistry[&wrapper.schema] = wrapper
}

// GetDepthLimit retrieves the depth limit for a schema
func GetDepthLimit(schema *graphql.Schema) (int, bool) {
	if wrapper, ok := depthLimitRegistry[schema]; ok {
		return wrapper.maxDepth, true
	}
	return 0, false
}

// calculateQueryDepth calculates the maximum depth of a GraphQL query
func calculateQueryDepth(document *ast.Document) int {
	maxDepth := 0

	for _, definition := range document.Definitions {
		switch def := definition.(type) {
		case *ast.OperationDefinition:
			depth := calculateSelectionSetDepth(def.SelectionSet, 1)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
	}

	return maxDepth
}

// calculateSelectionSetDepth recursively calculates the depth of a selection set
func calculateSelectionSetDepth(selectionSet *ast.SelectionSet, currentDepth int) int {
	if selectionSet == nil || len(selectionSet.Selections) == 0 {
		return currentDepth
	}

	maxDepth := currentDepth

	for _, selection := range selectionSet.Selections {
		switch sel := selection.(type) {
		case *ast.Field:
			// Skip introspection fields
			if isIntrospectionField(sel.Name.Value) {
				continue
			}

			// Skip scalar/leaf fields (id, properties, labels, etc.)
			if isLeafField(sel.Name.Value) {
				continue
			}

			// Recurse into nested selection set
			if sel.SelectionSet != nil {
				depth := calculateSelectionSetDepth(sel.SelectionSet, currentDepth+1)
				if depth > maxDepth {
					maxDepth = depth
				}
			}

		case *ast.InlineFragment:
			depth := calculateSelectionSetDepth(sel.SelectionSet, currentDepth)
			if depth > maxDepth {
				maxDepth = depth
			}

		case *ast.FragmentSpread:
			// For fragment spreads, we'd need to resolve the fragment definition
			// For now, count it as +1 depth
			if maxDepth < currentDepth+1 {
				maxDepth = currentDepth + 1
			}
		}
	}

	return maxDepth
}

// isIntrospectionField checks if a field is an introspection field
func isIntrospectionField(fieldName string) bool {
	return strings.HasPrefix(fieldName, "__")
}

// isLeafField checks if a field is a leaf/scalar field
func isLeafField(fieldName string) bool {
	leafFields := map[string]bool{
		"id":         true,
		"properties": true,
		"labels":     true,
		"type":       true,
		"weight":     true,
		"fromNodeId": true,
		"toNodeId":   true,
		"cursor":     true,
		"name":       true,
		"count":      true,
	}
	return leafFields[fieldName]
}

// ValidateQueryDepth validates a query against the depth limit
func ValidateQueryDepth(query string, maxDepth int) error {
	// Parse the query
	document, err := parser.Parse(parser.ParseParams{
		Source: query,
	})
	if err != nil {
		return fmt.Errorf("failed to parse query: %w", err)
	}

	// Calculate query depth
	queryDepth := calculateQueryDepth(document)

	// Check against limit
	if queryDepth > maxDepth {
		return fmt.Errorf("query depth %d exceeds maximum allowed depth %d", queryDepth, maxDepth)
	}

	return nil
}

// ExecuteWithDepthLimit executes a GraphQL query with depth validation
func ExecuteWithDepthLimit(schema graphql.Schema, query string, maxDepth int, variableValues map[string]interface{}) *graphql.Result {
	// Validate depth first
	if err := ValidateQueryDepth(query, maxDepth); err != nil {
		return &graphql.Result{
			Errors: []gqlerrors.FormattedError{
				gqlerrors.FormatError(err),
			},
		}
	}

	// Execute the query
	params := graphql.Params{
		Schema:        schema,
		RequestString: query,
	}

	if variableValues != nil {
		params.VariableValues = variableValues
	}

	return graphql.Do(params)
}
