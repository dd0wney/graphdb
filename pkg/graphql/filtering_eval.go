package graphql

import (
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// evaluateFilter checks if a node matches the filter expression
func evaluateFilter(node *storage.Node, expr *FilterExpression) bool {
	if expr == nil {
		return true // No filter means match all
	}

	return evaluateFilterExpression(node, expr)
}

// evaluateFilterExpression recursively evaluates a filter expression
func evaluateFilterExpression(node *storage.Node, expr *FilterExpression) bool {
	// Evaluate NOT first
	if expr.NOT != nil {
		return !evaluateFilterExpression(node, expr.NOT)
	}

	// Evaluate OR - at least one must match
	if len(expr.OR) > 0 {
		for _, orExpr := range expr.OR {
			if evaluateFilterExpression(node, &orExpr) {
				return true
			}
		}
		return false
	}

	// Evaluate AND - all must match
	if len(expr.AND) > 0 {
		for _, andExpr := range expr.AND {
			if !evaluateFilterExpression(node, &andExpr) {
				return false
			}
		}
		return true
	}

	// Evaluate simple conditions - all must match (implicit AND)
	for _, condition := range expr.Conditions {
		if !evaluateCondition(node, condition) {
			return false
		}
	}

	return true
}

// evaluateCondition checks if a node matches a single filter condition
func evaluateCondition(node *storage.Node, condition FilterCondition) bool {
	// Get the property value from the node
	propertyValue, exists := node.Properties[condition.Field]
	if !exists {
		return false
	}

	// Extract the actual value based on type
	var nodeValue any
	switch propertyValue.Type {
	case storage.TypeString:
		nodeValue, _ = propertyValue.AsString()
	case storage.TypeInt:
		nodeValue, _ = propertyValue.AsInt()
	case storage.TypeFloat:
		nodeValue, _ = propertyValue.AsFloat()
	case storage.TypeBool:
		nodeValue, _ = propertyValue.AsBool()
	default:
		return false
	}

	// Evaluate based on operator
	switch condition.Operator {
	case "eq":
		return evaluateEquals(nodeValue, condition.Value)
	case "gt":
		return evaluateGreaterThan(nodeValue, condition.Value)
	case "lt":
		return evaluateLessThan(nodeValue, condition.Value)
	case "gte":
		return evaluateGreaterThanOrEqual(nodeValue, condition.Value)
	case "lte":
		return evaluateLessThanOrEqual(nodeValue, condition.Value)
	case "contains":
		return evaluateContains(nodeValue, condition.Value)
	case "in":
		return evaluateIn(nodeValue, condition.Value)
	default:
		return false
	}
}

// evaluateEquals checks if two values are equal
func evaluateEquals(nodeValue, filterValue any) bool {
	switch nv := nodeValue.(type) {
	case string:
		fv, ok := filterValue.(string)
		return ok && nv == fv
	case int64:
		// Handle both int and float filter values
		switch fv := filterValue.(type) {
		case int:
			return nv == int64(fv)
		case int64:
			return nv == fv
		case float64:
			return float64(nv) == fv
		}
	case float64:
		switch fv := filterValue.(type) {
		case float64:
			return nv == fv
		case int:
			return nv == float64(fv)
		case int64:
			return nv == float64(fv)
		}
	case bool:
		fv, ok := filterValue.(bool)
		return ok && nv == fv
	}
	return false
}

// evaluateGreaterThan checks if nodeValue > filterValue
func evaluateGreaterThan(nodeValue, filterValue any) bool {
	return compareNumeric(nodeValue, filterValue) > 0
}

// evaluateLessThan checks if nodeValue < filterValue
func evaluateLessThan(nodeValue, filterValue any) bool {
	return compareNumeric(nodeValue, filterValue) < 0
}

// evaluateGreaterThanOrEqual checks if nodeValue >= filterValue
func evaluateGreaterThanOrEqual(nodeValue, filterValue any) bool {
	return compareNumeric(nodeValue, filterValue) >= 0
}

// evaluateLessThanOrEqual checks if nodeValue <= filterValue
func evaluateLessThanOrEqual(nodeValue, filterValue any) bool {
	return compareNumeric(nodeValue, filterValue) <= 0
}

// evaluateContains checks if a string contains a substring
func evaluateContains(nodeValue, filterValue any) bool {
	nv, ok1 := nodeValue.(string)
	fv, ok2 := filterValue.(string)
	if !ok1 || !ok2 {
		return false
	}
	return strings.Contains(nv, fv)
}

// evaluateIn checks if nodeValue is in the filter array
func evaluateIn(nodeValue, filterValue any) bool {
	filterArray, ok := filterValue.([]any)
	if !ok {
		return false
	}

	for _, item := range filterArray {
		if evaluateEquals(nodeValue, item) {
			return true
		}
	}
	return false
}

// compareNumeric compares two numeric values
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func compareNumeric(a, b any) int {
	var aFloat, bFloat float64

	// Convert a to float64
	switch av := a.(type) {
	case int64:
		aFloat = float64(av)
	case float64:
		aFloat = av
	case int:
		aFloat = float64(av)
	default:
		return 0
	}

	// Convert b to float64
	switch bv := b.(type) {
	case int64:
		bFloat = float64(bv)
	case float64:
		bFloat = bv
	case int:
		bFloat = float64(bv)
	default:
		return 0
	}

	if aFloat < bFloat {
		return -1
	} else if aFloat > bFloat {
		return 1
	}
	return 0
}

// evaluateEdgeFilter checks if an edge matches the filter expression
func evaluateEdgeFilter(edge *storage.Edge, expr *FilterExpression) bool {
	if expr == nil {
		return true
	}

	return evaluateEdgeFilterExpression(edge, expr)
}

// evaluateEdgeFilterExpression recursively evaluates a filter expression for edges
func evaluateEdgeFilterExpression(edge *storage.Edge, expr *FilterExpression) bool {
	// Evaluate NOT first
	if expr.NOT != nil {
		return !evaluateEdgeFilterExpression(edge, expr.NOT)
	}

	// Evaluate OR - at least one must match
	if len(expr.OR) > 0 {
		for _, orExpr := range expr.OR {
			if evaluateEdgeFilterExpression(edge, &orExpr) {
				return true
			}
		}
		return false
	}

	// Evaluate AND - all must match
	if len(expr.AND) > 0 {
		for _, andExpr := range expr.AND {
			if !evaluateEdgeFilterExpression(edge, &andExpr) {
				return false
			}
		}
		return true
	}

	// Evaluate simple conditions - all must match (implicit AND)
	for _, condition := range expr.Conditions {
		if !evaluateEdgeCondition(edge, condition) {
			return false
		}
	}

	return true
}

// evaluateEdgeCondition checks if an edge matches a single filter condition
func evaluateEdgeCondition(edge *storage.Edge, condition FilterCondition) bool {
	// Check if filtering by weight
	if condition.Field == "weight" {
		switch condition.Operator {
		case "eq":
			return evaluateEquals(edge.Weight, condition.Value)
		case "gt":
			return evaluateGreaterThan(edge.Weight, condition.Value)
		case "lt":
			return evaluateLessThan(edge.Weight, condition.Value)
		case "gte":
			return evaluateGreaterThanOrEqual(edge.Weight, condition.Value)
		case "lte":
			return evaluateLessThanOrEqual(edge.Weight, condition.Value)
		}
		return false
	}

	// Check edge properties
	propertyValue, exists := edge.Properties[condition.Field]
	if !exists {
		return false
	}

	var edgeValue any
	switch propertyValue.Type {
	case storage.TypeString:
		edgeValue, _ = propertyValue.AsString()
	case storage.TypeInt:
		edgeValue, _ = propertyValue.AsInt()
	case storage.TypeFloat:
		edgeValue, _ = propertyValue.AsFloat()
	case storage.TypeBool:
		edgeValue, _ = propertyValue.AsBool()
	default:
		return false
	}

	switch condition.Operator {
	case "eq":
		return evaluateEquals(edgeValue, condition.Value)
	case "gt":
		return evaluateGreaterThan(edgeValue, condition.Value)
	case "lt":
		return evaluateLessThan(edgeValue, condition.Value)
	case "gte":
		return evaluateGreaterThanOrEqual(edgeValue, condition.Value)
	case "lte":
		return evaluateLessThanOrEqual(edgeValue, condition.Value)
	case "contains":
		return evaluateContains(edgeValue, condition.Value)
	case "in":
		return evaluateIn(edgeValue, condition.Value)
	default:
		return false
	}
}
