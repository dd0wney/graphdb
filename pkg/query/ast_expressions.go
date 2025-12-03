package query

import "fmt"

// Expression is an interface for all expression types
type Expression interface {
	Eval(context map[string]any) (bool, error)
}

// BinaryExpression represents binary operations (AND, OR, =, <, >, etc.)
type BinaryExpression struct {
	Left     Expression
	Operator string
	Right    Expression
}

func (be *BinaryExpression) Eval(context map[string]any) (bool, error) {
	switch be.Operator {
	case "AND":
		left, err := be.Left.Eval(context)
		if err != nil || !left {
			return false, err
		}
		return be.Right.Eval(context)

	case "OR":
		left, err := be.Left.Eval(context)
		if err != nil {
			return false, err
		}
		if left {
			return true, nil
		}
		return be.Right.Eval(context)

	case "=", ">", "<", ">=", "<=", "!=":
		// Comparison operators
		return evalComparison(be.Left, be.Right, be.Operator, context)

	default:
		return false, fmt.Errorf("unknown operator: %s", be.Operator)
	}
}

// PropertyExpression represents property access (e.g., n.name)
type PropertyExpression struct {
	Variable string
	Property string
}

func (pe *PropertyExpression) Eval(context map[string]any) (bool, error) {
	// This returns the property value, not a boolean
	// Used in comparisons
	return false, fmt.Errorf("property expression must be used in comparison")
}

// LiteralExpression represents a literal value
type LiteralExpression struct {
	Value any
}

func (le *LiteralExpression) Eval(context map[string]any) (bool, error) {
	// Convert value to boolean
	if b, ok := le.Value.(bool); ok {
		return b, nil
	}
	return false, fmt.Errorf("cannot convert literal to boolean")
}
