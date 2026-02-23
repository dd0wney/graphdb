package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
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

	for _, binding := range ctx.results {
		if obj, ok := binding.bindings[item.Expression.Variable]; ok {
			if node, ok := obj.(*storage.Node); ok {
				if item.Expression.Property != "" {
					// Extract specific property
					if prop, exists := node.Properties[item.Expression.Property]; exists {
						// Extract actual value based on type
						val := ac.ExtractValue(prop)
						if val != nil {
							values = append(values, val)
						}
					}
				} else {
					// No property specified - count the node itself
					values = append(values, 1)
				}
			}
		}
	}

	return values
}

// ExtractValue extracts the actual value from storage.Value
func (ac *AggregationComputer) ExtractValue(val storage.Value) any {
	switch val.Type {
	case storage.TypeInt:
		if intVal, err := val.AsInt(); err == nil {
			return intVal
		}
	case storage.TypeFloat:
		if floatVal, err := val.AsFloat(); err == nil {
			return floatVal
		}
	case storage.TypeString:
		if strVal, err := val.AsString(); err == nil {
			return strVal
		}
	case storage.TypeBool:
		if boolVal, err := val.AsBool(); err == nil {
			return boolVal
		}
	case storage.TypeTimestamp:
		if timeVal, err := val.AsTimestamp(); err == nil {
			return timeVal.Unix()
		}
	}
	return nil
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
