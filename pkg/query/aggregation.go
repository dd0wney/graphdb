package query

import (
	"fmt"

	"github.com/dd0wney/graphdb/pkg/storage"
)

const (
	// nullPlaceholder represents null values in group keys
	nullPlaceholder = "<null>"
	// groupKeySeparator separates multiple group-by values in composite keys
	groupKeySeparator = "\x00"
)

// AggregationComputer handles aggregate function computation
type AggregationComputer struct{}

// ComputeAggregates computes all aggregate functions in the return clause
func (ac *AggregationComputer) ComputeAggregates(ctx *ExecutionContext, returnItems []*ReturnItem) map[string]any {
	result := make(map[string]any)

	for _, item := range returnItems {
		if item.Aggregate == "" {
			continue
		}

		// Build column name
		columnName := item.Alias
		if columnName == "" {
			if item.Expression != nil {
				columnName = fmt.Sprintf("%s(%s.%s)", item.Aggregate, item.Expression.Variable, item.Expression.Property)
			} else {
				columnName = fmt.Sprintf("%s(*)", item.Aggregate)
			}
		}

		// Extract values from execution context
		values := ac.extractValues(ctx, item)

		// Compute aggregate
		switch item.Aggregate {
		case "COUNT":
			result[columnName] = len(values)
		case "SUM":
			result[columnName] = ac.sum(values)
		case "AVG":
			result[columnName] = ac.avg(values)
		case "MIN":
			result[columnName] = ac.min(values)
		case "MAX":
			result[columnName] = ac.max(values)
		case "COLLECT":
			result[columnName] = ac.collect(values)
		default:
			result[columnName] = nil
		}
	}

	return result
}

// extractValues extracts all values for a given property from the execution context
func (ac *AggregationComputer) extractValues(ctx *ExecutionContext, item *ReturnItem) []any {
	values := make([]any, 0)

	// If no expression, count all bindings (COUNT(*))
	if item.Expression == nil {
		return make([]any, len(ctx.results))
	}

	// A node and a relationship are read the same way. Reading only nodes made
	// every aggregate over a relationship empty (count(r) was 0), and a bare
	// node was collected as the number 1 instead of the node.
	for _, binding := range ctx.results {
		obj, ok := binding.bindings[item.Expression.Variable]
		if !ok || obj == nil {
			continue // null is not counted or collected
		}
		var props map[string]storage.Value
		switch v := obj.(type) {
		case *storage.Node:
			props = v.Properties
		case *storage.Edge:
			props = v.Properties
		default:
			// A plain value bound by WITH or UNWIND has no properties.
			if item.Expression.Property == "" {
				values = append(values, v)
			}
			continue
		}
		if item.Expression.Property == "" {
			values = append(values, obj)
			continue
		}
		if prop, exists := props[item.Expression.Property]; exists {
			if val := ac.ExtractValue(prop); val != nil {
				values = append(values, val)
			}
		}
	}

	return values
}

// ExtractValue extracts the actual value from storage.Value
func (ac *AggregationComputer) ExtractValue(val storage.Value) any {
	return extractStorageValue(val)
}

// hasAggregates checks if any return item has an aggregate function
func hasAggregates(returnItems []*ReturnItem) bool {
	for _, item := range returnItems {
		if item.Aggregate != "" {
			return true
		}
	}
	return false
}
