package query

import (
	"fmt"

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
				if val, found := node.Properties[e.Property]; found {
					// Extract the actual value from storage.Value based on type
					switch val.Type {
					case storage.TypeInt:
						if intVal, err := val.AsInt(); err == nil {
							return intVal
						}
					case storage.TypeString:
						if strVal, err := val.AsString(); err == nil {
							return strVal
						}
					case storage.TypeFloat:
						if floatVal, err := val.AsFloat(); err == nil {
							return floatVal
						}
					case storage.TypeBool:
						if boolVal, err := val.AsBool(); err == nil {
							return boolVal
						}
					}
				}
				return nil
			}
			// Fallback to map[string]any for backward compatibility
			if m, ok := obj.(map[string]any); ok {
				return m[e.Property]
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
