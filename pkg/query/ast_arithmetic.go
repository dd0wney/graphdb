package query

import (
	"fmt"
	"math"
)

// ArithmeticExpression represents binary arithmetic: +, -, *, /, %
// Uses the dual-eval pattern: EvalValue returns the computed value,
// Eval coerces to bool for WHERE context.
type ArithmeticExpression struct {
	Left     Expression
	Operator string // "+", "-", "*", "/", "%"
	Right    Expression
}

func (ae *ArithmeticExpression) Eval(context map[string]any) (bool, error) {
	val, err := ae.EvalValue(context)
	if err != nil {
		return false, err
	}
	return coerceToBool(val), nil
}

func (ae *ArithmeticExpression) EvalValue(context map[string]any) (any, error) {
	leftVal := extractValue(ae.Left, context)
	rightVal := extractValue(ae.Right, context)

	// Null propagation: any nil operand → nil
	if leftVal == nil || rightVal == nil {
		return nil, nil
	}

	return evalArithmetic(leftVal, ae.Operator, rightVal)
}

// evalArithmetic performs the actual arithmetic dispatch on resolved, non-nil values.
func evalArithmetic(left any, op string, right any) (any, error) {
	// String concatenation: string + string
	if op == "+" {
		lStr, lOk := left.(string)
		rStr, rOk := right.(string)
		if lOk && rOk {
			return lStr + rStr, nil
		}
		// One string, one non-string → type error
		if lOk || rOk {
			return nil, fmt.Errorf("cannot use %s with mixed string and %T", op, selectNonString(left, right))
		}
	}

	// Numeric operations: coerce to numeric pair
	lNum, rNum, isFloat, err := coerceNumericPair(left, right)
	if err != nil {
		return nil, fmt.Errorf("cannot use %s with %T and %T", op, left, right)
	}

	if isFloat {
		return evalFloatOp(lNum.f, op, rNum.f)
	}
	return evalIntOp(lNum.i, op, rNum.i)
}

// numericVal holds either an int64 or float64 value.
type numericVal struct {
	i int64
	f float64
}

// coerceNumericPair converts two values to a numeric pair, promoting to float64 if either is float.
func coerceNumericPair(left, right any) (numericVal, numericVal, bool, error) {
	lInt, lIsInt := left.(int64)
	lFloat, lIsFloat := left.(float64)
	rInt, rIsInt := right.(int64)
	rFloat, rIsFloat := right.(float64)

	switch {
	case lIsInt && rIsInt:
		return numericVal{i: lInt}, numericVal{i: rInt}, false, nil
	case lIsFloat && rIsFloat:
		return numericVal{f: lFloat}, numericVal{f: rFloat}, true, nil
	case lIsInt && rIsFloat:
		return numericVal{f: float64(lInt)}, numericVal{f: rFloat}, true, nil
	case lIsFloat && rIsInt:
		return numericVal{f: lFloat}, numericVal{f: float64(rInt)}, true, nil
	default:
		return numericVal{}, numericVal{}, false, fmt.Errorf("non-numeric operands")
	}
}

func evalIntOp(left int64, op string, right int64) (any, error) {
	switch op {
	case "+":
		return left + right, nil
	case "-":
		return left - right, nil
	case "*":
		return left * right, nil
	case "/":
		if right == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return left / right, nil
	case "%":
		if right == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return left % right, nil
	default:
		return nil, fmt.Errorf("unknown arithmetic operator: %s", op)
	}
}

func evalFloatOp(left float64, op string, right float64) (any, error) {
	switch op {
	case "+":
		return left + right, nil
	case "-":
		return left - right, nil
	case "*":
		return left * right, nil
	case "/":
		if right == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return left / right, nil
	case "%":
		if right == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return math.Mod(left, right), nil
	default:
		return nil, fmt.Errorf("unknown arithmetic operator: %s", op)
	}
}

// selectNonString returns whichever value is not a string (for error messages).
func selectNonString(a, b any) any {
	if _, ok := a.(string); !ok {
		return a
	}
	return b
}

// UnaryExpression represents unary operators: NOT, -
type UnaryExpression struct {
	Operator string // "NOT", "-"
	Operand  Expression
}

func (ue *UnaryExpression) Eval(context map[string]any) (bool, error) {
	switch ue.Operator {
	case "NOT":
		result, err := ue.Operand.Eval(context)
		if err != nil {
			// If operand can't eval as bool, extract value and coerce
			val := extractValue(ue.Operand, context)
			return !coerceToBool(val), nil
		}
		return !result, nil
	case "-":
		val, err := ue.EvalValue(context)
		if err != nil {
			return false, err
		}
		return coerceToBool(val), nil
	default:
		return false, fmt.Errorf("unknown unary operator: %s", ue.Operator)
	}
}

func (ue *UnaryExpression) EvalValue(context map[string]any) (any, error) {
	switch ue.Operator {
	case "NOT":
		result, err := ue.Operand.Eval(context)
		if err != nil {
			val := extractValue(ue.Operand, context)
			return !coerceToBool(val), nil
		}
		return !result, nil
	case "-":
		val := extractValue(ue.Operand, context)
		if val == nil {
			return nil, nil
		}
		switch v := val.(type) {
		case int64:
			return -v, nil
		case float64:
			return -v, nil
		default:
			return nil, fmt.Errorf("cannot negate %T", val)
		}
	default:
		return nil, fmt.Errorf("unknown unary operator: %s", ue.Operator)
	}
}
