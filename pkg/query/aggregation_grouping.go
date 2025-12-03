package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ComputeGroupedAggregates computes aggregates for each group
func (ac *AggregationComputer) ComputeGroupedAggregates(ctx *ExecutionContext, returnItems []*ReturnItem, groupByExprs []*PropertyExpression) []map[string]any {
	// Group results by the specified properties
	groups := ac.groupResults(ctx, groupByExprs)

	// Compute aggregates for each group
	results := make([]map[string]any, 0, len(groups))

	for groupKey, groupBindings := range groups {
		// Create a temporary execution context for this group
		groupCtx := &ExecutionContext{
			graph:   ctx.graph,
			results: groupBindings,
		}

		// Compute aggregates for this group
		row := ac.ComputeAggregates(groupCtx, returnItems)

		// Add the group-by values to the row
		groupValues := ac.parseGroupKey(groupKey)
		for i, expr := range groupByExprs {
			if i < len(groupValues) {
				columnName := fmt.Sprintf("%s.%s", expr.Variable, expr.Property)
				row[columnName] = groupValues[i]
			}
		}

		results = append(results, row)
	}

	return results
}

// groupResults groups execution context results by the specified properties
func (ac *AggregationComputer) groupResults(ctx *ExecutionContext, groupByExprs []*PropertyExpression) map[string][]*BindingSet {
	groups := make(map[string][]*BindingSet)

	for _, binding := range ctx.results {
		// Build group key from the property values
		key := ac.buildGroupKey(binding, groupByExprs)
		groups[key] = append(groups[key], binding)
	}

	return groups
}

// buildGroupKey creates a unique key for a group based on property values
func (ac *AggregationComputer) buildGroupKey(binding *BindingSet, groupByExprs []*PropertyExpression) string {
	keyParts := make([]string, 0, len(groupByExprs))

	for _, expr := range groupByExprs {
		if obj, ok := binding.bindings[expr.Variable]; ok {
			if node, ok := obj.(*storage.Node); ok {
				if prop, exists := node.Properties[expr.Property]; exists {
					val := ac.ExtractValue(prop)
					// Use a delimiter that won't appear in values
					keyParts = append(keyParts, fmt.Sprintf("%v", val))
				} else {
					keyParts = append(keyParts, nullPlaceholder)
				}
			}
		}
	}

	// Join parts with separator (null byte) which won't appear in string values
	result := ""
	for i, part := range keyParts {
		if i > 0 {
			result += groupKeySeparator
		}
		result += part
	}
	return result
}

// parseGroupKey extracts individual values from a group key
func (ac *AggregationComputer) parseGroupKey(key string) []any {
	// Split by separator
	parts := make([]string, 0)
	current := ""

	for i := 0; i < len(key); i++ {
		if key[i] == groupKeySeparator[0] {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(key[i])
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	// Convert to any slice
	values := make([]any, len(parts))
	for i, part := range parts {
		values[i] = part
	}

	return values
}
