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

	case "=", ">", "<", ">=", "<=", "!=", "IS NULL", "IS NOT NULL", "IN",
		"STARTS WITH", "ENDS WITH", "CONTAINS":
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

// FunctionCallExpression represents a function call (e.g., toLower(n.name))
type FunctionCallExpression struct {
	Name string
	Args []Expression
}

// Eval evaluates the function and coerces the result to bool (for WHERE usage)
func (fce *FunctionCallExpression) Eval(context map[string]any) (bool, error) {
	result, err := fce.EvalValue(context)
	if err != nil {
		return false, err
	}
	return coerceToBool(result), nil
}

// EvalValue evaluates the function and returns the raw result
func (fce *FunctionCallExpression) EvalValue(context map[string]any) (any, error) {
	fn, err := GetFunction(fce.Name)
	if err != nil {
		return nil, err
	}

	// Evaluate arguments
	args := make([]any, len(fce.Args))
	for i, arg := range fce.Args {
		args[i] = extractValue(arg, context)
	}

	return fn(args)
}

// ParameterExpression represents a query parameter reference ($name) in expressions
type ParameterExpression struct {
	Name string // "name" from $name
}

func (pe *ParameterExpression) Eval(context map[string]any) (bool, error) {
	val, ok := context["$"+pe.Name]
	if !ok {
		return false, fmt.Errorf("missing parameter: $%s", pe.Name)
	}
	return coerceToBool(val), nil
}

// EvalValue returns the raw parameter value for use in comparisons
func (pe *ParameterExpression) EvalValue(context map[string]any) (any, error) {
	val, ok := context["$"+pe.Name]
	if !ok {
		return nil, fmt.Errorf("missing parameter: $%s", pe.Name)
	}
	return val, nil
}

// CaseExpression represents a CASE expression (both searched and simple forms)
type CaseExpression struct {
	Operand     Expression // non-nil for simple CASE, nil for searched CASE
	WhenClauses []CaseWhen
	ElseResult  Expression // nil if no ELSE
}

// CaseWhen represents a single WHEN/THEN branch
type CaseWhen struct {
	Condition Expression // bool condition (searched) or comparison value (simple)
	Result    Expression
}

// Eval evaluates CASE as a boolean (for WHERE usage) by coercing the result value
func (ce *CaseExpression) Eval(context map[string]any) (bool, error) {
	val, err := ce.EvalValue(context)
	if err != nil {
		return false, err
	}
	return coerceToBool(val), nil
}

// EvalValue returns the raw value of the first matching WHEN branch
func (ce *CaseExpression) EvalValue(context map[string]any) (any, error) {
	if ce.Operand == nil {
		// Searched form: CASE WHEN <bool> THEN <result> ...
		for _, wc := range ce.WhenClauses {
			match, err := wc.Condition.Eval(context)
			if err != nil {
				return nil, err
			}
			if match {
				return extractValue(wc.Result, context), nil
			}
		}
	} else {
		// Simple form: CASE <operand> WHEN <value> THEN <result> ...
		operandVal := extractValue(ce.Operand, context)
		for _, wc := range ce.WhenClauses {
			condVal := extractValue(wc.Condition, context)
			if compareValues(operandVal, condVal) == 0 && operandVal != nil {
				return extractValue(wc.Result, context), nil
			}
		}
	}

	// No match — fall through to ELSE or nil
	if ce.ElseResult != nil {
		return extractValue(ce.ElseResult, context), nil
	}
	return nil, nil
}

// coerceToBool converts an arbitrary value to a boolean
func coerceToBool(val any) bool {
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case int64:
		return v != 0
	case float64:
		return v != 0
	case string:
		return v != ""
	default:
		return true
	}
}
