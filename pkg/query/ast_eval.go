package query

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Helper function to evaluate comparisons
func evalComparison(left, right Expression, op string, context map[string]any) (bool, error) {
	// Extract actual values
	leftVal := extractValue(left, context)
	rightVal := extractValue(right, context)

	switch op {
	case "=":
		return leftVal == rightVal, nil
	case "!=":
		return leftVal != rightVal, nil
	case ">":
		return compareValues(leftVal, rightVal) > 0, nil
	case "<":
		return compareValues(leftVal, rightVal) < 0, nil
	case ">=":
		return compareValues(leftVal, rightVal) >= 0, nil
	case "<=":
		return compareValues(leftVal, rightVal) <= 0, nil
	case "IS NULL":
		return leftVal == nil, nil
	case "IS NOT NULL":
		return leftVal != nil, nil
	case "IN":
		// Cypher: null IN [...] evaluates to null (treated as false in boolean context)
		if leftVal == nil {
			return false, nil
		}
		list, ok := rightVal.([]any)
		if !ok {
			return false, fmt.Errorf("IN requires a list on the right side")
		}
		for _, item := range list {
			if item == nil {
				continue // skip null elements in list
			}
			if leftVal == item {
				return true, nil
			}
			// Handle numeric type coercion (int64 vs float64)
			if compareValues(leftVal, item) == 0 {
				return true, nil
			}
		}
		return false, nil
	case "STARTS WITH":
		if leftVal == nil || rightVal == nil {
			return false, nil
		}
		lStr, lOk := leftVal.(string)
		rStr, rOk := rightVal.(string)
		if !lOk || !rOk {
			return false, nil
		}
		return strings.HasPrefix(lStr, rStr), nil
	case "ENDS WITH":
		if leftVal == nil || rightVal == nil {
			return false, nil
		}
		lStr, lOk := leftVal.(string)
		rStr, rOk := rightVal.(string)
		if !lOk || !rOk {
			return false, nil
		}
		return strings.HasSuffix(lStr, rStr), nil
	case "CONTAINS":
		if leftVal == nil || rightVal == nil {
			return false, nil
		}
		lStr, lOk := leftVal.(string)
		rStr, rOk := rightVal.(string)
		if !lOk || !rOk {
			return false, nil
		}
		return strings.Contains(lStr, rStr), nil
	default:
		return false, fmt.Errorf("unknown comparison operator: %s", op)
	}
}

// Extract actual value from expression
func extractValue(expr Expression, context map[string]any) any {
	switch e := expr.(type) {
	case *PropertyExpression:
		if obj, ok := context[e.Variable]; ok {
			// Handle storage.Node objects
			if node, ok := obj.(*storage.Node); ok {
				if e.Property == "" {
					return node
				}
				if val, found := node.Properties[e.Property]; found {
					return extractStorageValue(val)
				}
				return nil
			}
			// Handle storage.Edge objects
			if edge, ok := obj.(*storage.Edge); ok {
				if e.Property == "" {
					return edge
				}
				if val, found := edge.Properties[e.Property]; found {
					return extractStorageValue(val)
				}
				return nil
			}
			// Fallback to map[string]any for backward compatibility
			if m, ok := obj.(map[string]any); ok {
				if e.Property == "" {
					return m
				}
				return m[e.Property]
			}
			// Raw value binding (e.g., from WITH projections)
			if e.Property == "" {
				return obj
			}
		}
		return nil
	case *LiteralExpression:
		return e.Value
	case *FunctionCallExpression:
		result, err := e.EvalValue(context)
		if err != nil {
			return nil
		}
		return result
	case *ParameterExpression:
		val, ok := context["$"+e.Name]
		if !ok {
			return nil
		}
		return val
	case *CaseExpression:
		result, err := e.EvalValue(context)
		if err != nil {
			return nil
		}
		return result
	case *BinaryExpression:
		result, err := e.Eval(context)
		if err != nil {
			return nil
		}
		return result
	case *ArithmeticExpression:
		result, err := e.EvalValue(context)
		if err != nil {
			return nil
		}
		return result
	case *UnaryExpression:
		result, err := e.EvalValue(context)
		if err != nil {
			return nil
		}
		return result
	default:
		return nil
	}
}

// Compare values (simplified)
func compareValues(left, right any) int {
	// Handle int64
	lInt, lIsInt := left.(int64)
	rInt, rIsInt := right.(int64)
	if lIsInt && rIsInt {
		if lInt < rInt {
			return -1
		} else if lInt > rInt {
			return 1
		}
		return 0
	}

	// Handle float64
	lFloat, lIsFloat := left.(float64)
	rFloat, rIsFloat := right.(float64)
	if lIsFloat && rIsFloat {
		if lFloat < rFloat {
			return -1
		} else if lFloat > rFloat {
			return 1
		}
		return 0
	}

	// Handle mixed int/float (int on left, float on right)
	if lIsInt && rIsFloat {
		lFloat = float64(lInt)
		if lFloat < rFloat {
			return -1
		} else if lFloat > rFloat {
			return 1
		}
		return 0
	}

	// Handle mixed int/float (float on left, int on right)
	if lIsFloat && rIsInt {
		rFloat = float64(rInt)
		if lFloat < rFloat {
			return -1
		} else if lFloat > rFloat {
			return 1
		}
		return 0
	}

	// Handle int (for backwards compatibility)
	if lIntPlain, ok := left.(int); ok {
		if rIntPlain, ok := right.(int); ok {
			return lIntPlain - rIntPlain
		}
	}

	// Handle string
	lStr, lIsStr := left.(string)
	rStr, rIsStr := right.(string)
	if lIsStr && rIsStr {
		if lStr < rStr {
			return -1
		} else if lStr > rStr {
			return 1
		}
		return 0
	}

	// Default: equal
	return 0
}
