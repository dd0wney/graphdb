package query

import "fmt"

// Expression is an interface for all expression types.
// EvalValue returns the raw evaluation result (used for projection, SET RHS,
// CALL args); Eval coerces to bool (used for WHERE / AND / OR / comparison).
// All concrete implementations satisfy both — the dual-eval pattern.
type Expression interface {
	Eval(context map[string]any) (bool, error)
	EvalValue(context map[string]any) (any, error)
}

// BinaryExpression represents binary operations (AND, OR, =, <, >, etc.)
type BinaryExpression struct {
	Left     Expression
	Operator string
	Right    Expression
}

func (be *BinaryExpression) Eval(context map[string]any) (bool, error) {
	switch be.Operator {
	case "AND", "OR":
		// Unknown removes the row, as false does.
		result, err := evalTri(be, context)
		return result == triTrue, err

	case "=", ">", "<", ">=", "<=", "!=", "IS NULL", "IS NOT NULL", "IN",
		"STARTS WITH", "ENDS WITH", "CONTAINS":
		// Comparison operators
		return evalComparison(be.Left, be.Right, be.Operator, context)

	default:
		return false, fmt.Errorf("unknown operator: %s", be.Operator)
	}
}

// EvalValue gives null where the result is unknown: AND, OR and an ordering
// comparison, and = and != over such an operand. Every other operator is
// two-valued.
func (be *BinaryExpression) EvalValue(context map[string]any) (any, error) {
	if be.Operator == "AND" || be.Operator == "OR" || be.Operator == "=" || be.Operator == "!=" || isOrderingOp(be.Operator) {
		result, err := evalTri(be, context)
		if err != nil {
			return nil, err
		}
		return result.value(), nil
	}
	return be.Eval(context)
}

// PropertyExpression represents property access (e.g., n.name)
type PropertyExpression struct {
	Variable string
	Property string
}

// Eval reads the property as a predicate. It goes through evalPredicate so that
// a filter and Eval cannot disagree about a bare boolean.
func (pe *PropertyExpression) Eval(context map[string]any) (bool, error) {
	return evalPredicate(pe, context)
}

func (pe *PropertyExpression) EvalValue(context map[string]any) (any, error) {
	return propertyValue(pe, context), nil
}

// LiteralExpression represents a literal value
type LiteralExpression struct {
	Value any
}

func (le *LiteralExpression) Eval(context map[string]any) (bool, error) {
	return evalPredicate(le, context)
}

func (le *LiteralExpression) EvalValue(context map[string]any) (any, error) {
	return le.Value, nil
}

// FunctionCallExpression represents a function call (e.g., toLower(n.name))
type FunctionCallExpression struct {
	Name string
	Args []Expression
}

// Eval reads the function result as a predicate: null and false do not
// select, and a non-boolean result is refused.
func (fce *FunctionCallExpression) Eval(context map[string]any) (bool, error) {
	return evalPredicate(fce, context)
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
		v, err := evalOperand(arg, context)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}

	return fn(args)
}

// ParameterExpression represents a query parameter reference ($name) in expressions
type ParameterExpression struct {
	Name string // "name" from $name
}

func (pe *ParameterExpression) Eval(context map[string]any) (bool, error) {
	return evalPredicate(pe, context)
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

// Eval reads the CASE result as a predicate.
func (ce *CaseExpression) Eval(context map[string]any) (bool, error) {
	return evalPredicate(ce, context)
}

// EvalValue returns the raw value of the first matching WHEN branch
func (ce *CaseExpression) EvalValue(context map[string]any) (any, error) {
	if ce.Operand == nil {
		// Searched form: CASE WHEN <bool> THEN <result> ...
		for _, wc := range ce.WhenClauses {
			// Unknown selects no branch, so it falls to ELSE.
			match, err := evalPredicate(wc.Condition, context)
			if err != nil {
				return nil, err
			}
			if match {
				return evalOperand(wc.Result, context)
			}
		}
	} else {
		// Simple form: CASE <operand> WHEN <value> THEN <result> ...
		operandVal, err := evalOperand(ce.Operand, context)
		if err != nil {
			return nil, err
		}
		for _, wc := range ce.WhenClauses {
			condVal, err := evalOperand(wc.Condition, context)
			if err != nil {
				return nil, err
			}
			if valuesEqual(operandVal, condVal) && operandVal != nil {
				return evalOperand(wc.Result, context)
			}
		}
	}

	// No match — fall through to ELSE or nil
	if ce.ElseResult != nil {
		return evalOperand(ce.ElseResult, context)
	}
	return nil, nil
}
