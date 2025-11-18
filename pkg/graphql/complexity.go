package graphql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
)

// ComplexityConfig defines configuration for query complexity analysis
type ComplexityConfig struct {
	MaxComplexity    int // Maximum allowed complexity score
	ListMultiplier   int // Multiplier for list fields (default: 10)
	DefaultListLimit int // Default limit for lists without explicit limit (default: 100)
}

// ValidateComplexityConfig validates the complexity configuration
func ValidateComplexityConfig(config *ComplexityConfig) error {
	if config.MaxComplexity <= 0 {
		return fmt.Errorf("max complexity must be greater than 0, got %d", config.MaxComplexity)
	}
	if config.ListMultiplier <= 0 {
		config.ListMultiplier = 10 // Default
	}
	if config.DefaultListLimit <= 0 {
		config.DefaultListLimit = 100 // Default
	}
	return nil
}

// GenerateSchemaWithComplexity generates a GraphQL schema with complexity analysis
func GenerateSchemaWithComplexity(gs *storage.GraphStorage, config *ComplexityConfig) (graphql.Schema, error) {
	// Validate config
	if err := ValidateComplexityConfig(config); err != nil {
		return graphql.Schema{}, err
	}

	// Use existing filtering schema as base
	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		return graphql.Schema{}, err
	}

	return schema, nil
}

// calculateQueryComplexity calculates the complexity score of a GraphQL query
func calculateQueryComplexity(document *ast.Document, config *ComplexityConfig, variableValues map[string]interface{}) int {
	totalComplexity := 0

	for _, definition := range document.Definitions {
		switch def := definition.(type) {
		case *ast.OperationDefinition:
			complexity := calculateSelectionSetComplexity(def.SelectionSet, config, variableValues, 1)
			totalComplexity += complexity
		}
	}

	return totalComplexity
}

// calculateSelectionSetComplexity recursively calculates complexity of a selection set
func calculateSelectionSetComplexity(selectionSet *ast.SelectionSet, config *ComplexityConfig, variableValues map[string]interface{}, multiplier int) int {
	if selectionSet == nil || len(selectionSet.Selections) == 0 {
		return 0
	}

	complexity := 0

	for _, selection := range selectionSet.Selections {
		switch sel := selection.(type) {
		case *ast.Field:
			// Skip introspection fields (they have fixed low cost)
			if isIntrospectionField(sel.Name.Value) {
				complexity += 1
				continue
			}

			// Determine if this is a list field
			fieldMultiplier := multiplier
			if isListField(sel.Name.Value) {
				// Check if there's a limit argument
				limit := extractLimitFromArguments(sel.Arguments, variableValues, config.DefaultListLimit)
				fieldMultiplier = multiplier * limit
			}

			// Leaf fields add cost but don't recurse
			if isLeafField(sel.Name.Value) {
				complexity += multiplier
				continue
			}

			// Recurse into nested selection set
			if sel.SelectionSet != nil {
				nestedComplexity := calculateSelectionSetComplexity(sel.SelectionSet, config, variableValues, fieldMultiplier)
				complexity += nestedComplexity
			} else {
				// Non-leaf field without selection set (shouldn't happen in valid queries)
				complexity += fieldMultiplier
			}

		case *ast.InlineFragment:
			complexity += calculateSelectionSetComplexity(sel.SelectionSet, config, variableValues, multiplier)

		case *ast.FragmentSpread:
			// Fragment spreads add minimal complexity
			complexity += multiplier
		}
	}

	return complexity
}

// isListField checks if a field returns a list/collection
func isListField(fieldName string) bool {
	// Plural field names or known list fields
	listFields := map[string]bool{
		"persons":           true,
		"edges":             true,
		"personsConnection": true,
		"edgesConnection":   true,
		"outgoingEdges":     true,
		"incomingEdges":     true,
	}

	// Check known list fields
	if listFields[fieldName] {
		return true
	}

	// Don't apply heuristic to known leaf/scalar fields
	if isLeafField(fieldName) {
		return false
	}

	// Heuristic: fields ending in 's' or 'Connection' are likely lists
	return strings.HasSuffix(fieldName, "s") || strings.HasSuffix(fieldName, "Connection")
}

// extractLimitFromArguments extracts the limit value from field arguments
func extractLimitFromArguments(arguments []*ast.Argument, variableValues map[string]interface{}, defaultLimit int) int {
	for _, arg := range arguments {
		if arg.Name.Value == "limit" || arg.Name.Value == "first" || arg.Name.Value == "last" {
			// Try to extract the value
			switch value := arg.Value.(type) {
			case *ast.IntValue:
				// Direct integer value - GetValue() returns a string
				if limitStr, ok := value.GetValue().(string); ok {
					if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
						return limit
					}
				}
			case *ast.Variable:
				// Variable reference
				if variableValues != nil {
					if limit, ok := variableValues[value.Name.Value].(int); ok && limit > 0 {
						return limit
					}
				}
			}
		}
	}

	return defaultLimit
}

// ValidateQueryComplexity validates a query against the complexity limit
func ValidateQueryComplexity(query string, config *ComplexityConfig, variableValues map[string]interface{}) (int, error) {
	// Parse the query
	document, err := parser.Parse(parser.ParseParams{
		Source: query,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to parse query: %w", err)
	}

	// Calculate query complexity
	queryComplexity := calculateQueryComplexity(document, config, variableValues)

	// Check against limit
	if queryComplexity > config.MaxComplexity {
		return queryComplexity, fmt.Errorf("query complexity %d exceeds maximum allowed complexity %d", queryComplexity, config.MaxComplexity)
	}

	return queryComplexity, nil
}

// ExecuteWithComplexity executes a GraphQL query with complexity validation
func ExecuteWithComplexity(schema graphql.Schema, query string, maxComplexity int, variableValues map[string]interface{}) *graphql.Result {
	// Create config for validation
	config := &ComplexityConfig{
		MaxComplexity:    maxComplexity,
		ListMultiplier:   10,
		DefaultListLimit: 100,
	}

	// Validate complexity first
	_, err := ValidateQueryComplexity(query, config, variableValues)
	if err != nil {
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
