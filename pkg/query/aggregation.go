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
func (ac *AggregationComputer) ComputeAggregates(ctx *ExecutionContext, returnItems []*ReturnItem) map[string]interface{} {
	result := make(map[string]interface{})

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
		default:
			result[columnName] = nil
		}
	}

	return result
}

// extractValues extracts all values for a given property from the execution context
func (ac *AggregationComputer) extractValues(ctx *ExecutionContext, item *ReturnItem) []interface{} {
	values := make([]interface{}, 0)

	// If no expression, count all bindings (COUNT(*))
	if item.Expression == nil {
		return make([]interface{}, len(ctx.results))
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
func (ac *AggregationComputer) ExtractValue(val storage.Value) interface{} {
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

// sum computes the sum of numeric values
func (ac *AggregationComputer) sum(values []interface{}) interface{} {
	if len(values) == 0 {
		return 0
	}

	var sumInt int64
	var sumFloat float64
	hasFloat := false

	for _, val := range values {
		switch v := val.(type) {
		case int64:
			sumInt += v
		case float64:
			hasFloat = true
			sumFloat += v
		case int:
			sumInt += int64(v)
		}
	}

	if hasFloat {
		return sumFloat + float64(sumInt)
	}
	return sumInt
}

// avg computes the average of numeric values
func (ac *AggregationComputer) avg(values []interface{}) interface{} {
	if len(values) == 0 {
		return nil
	}

	sumVal := ac.sum(values)
	count := float64(len(values))

	switch s := sumVal.(type) {
	case int64:
		return float64(s) / count
	case float64:
		return s / count
	default:
		return nil
	}
}

// min finds the minimum value
func (ac *AggregationComputer) min(values []interface{}) interface{} {
	if len(values) == 0 {
		return nil
	}

	minVal := values[0]

	for i := 1; i < len(values); i++ {
		if ac.compare(values[i], minVal) < 0 {
			minVal = values[i]
		}
	}

	return minVal
}

// max finds the maximum value
func (ac *AggregationComputer) max(values []interface{}) interface{} {
	if len(values) == 0 {
		return nil
	}

	maxVal := values[0]

	for i := 1; i < len(values); i++ {
		if ac.compare(values[i], maxVal) > 0 {
			maxVal = values[i]
		}
	}

	return maxVal
}

// compare compares two values (returns -1, 0, or 1)
// Now delegates to the unified compareValues function
func (ac *AggregationComputer) compare(a, b interface{}) int {
	return compareValues(a, b)
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

// ComputeGroupedAggregates computes aggregates for each group
func (ac *AggregationComputer) ComputeGroupedAggregates(ctx *ExecutionContext, returnItems []*ReturnItem, groupByExprs []*PropertyExpression) []map[string]interface{} {
	// Group results by the specified properties
	groups := ac.groupResults(ctx, groupByExprs)

	// Compute aggregates for each group
	results := make([]map[string]interface{}, 0, len(groups))

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
func (ac *AggregationComputer) parseGroupKey(key string) []interface{} {
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

	// Convert to interface{} slice
	values := make([]interface{}, len(parts))
	for i, part := range parts {
		values[i] = part
	}

	return values
}
