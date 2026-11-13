package query

import (
	"fmt"
	"math"
	"reflect"

	"github.com/dd0wney/graphdb/pkg/storage"
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
	leftVal, err := evalOperand(ae.Left, context)
	if err != nil {
		return nil, err
	}
	rightVal, err := evalOperand(ae.Right, context)
	if err != nil {
		return nil, err
	}

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
			return nil, refuseOperands(op, left, right)
		}
	}

	// Numeric operations: coerce to numeric pair
	lNum, rNum, isFloat, err := coerceNumericPair(left, right)
	if err != nil {
		return nil, refuseOperands(op, left, right)
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
			return nil, refusef("division by zero")
		}
		return left / right, nil
	case "%":
		if right == 0 {
			return nil, refusef("modulo by zero")
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
			return nil, refusef("division by zero")
		}
		return left / right, nil
	case "%":
		if right == 0 {
			return nil, refusef("modulo by zero")
		}
		return math.Mod(left, right), nil
	default:
		return nil, fmt.Errorf("unknown arithmetic operator: %s", op)
	}
}

// refuseOperands refuses an operator applied to a pair of types that have no
// arithmetic. The message names the types in Cypher words and never the
// values: a server shows this text to the caller, and a value comes from the
// graph.
func refuseOperands(op string, left, right any) error {
	return refusef("cannot apply %s to %s and %s", op, cypherTypeName(left), cypherTypeName(right))
}

// cypherTypeName names the type of a value in Cypher words, not Go words.
func cypherTypeName(v any) string {
	if n, ok := asNumber(v); ok {
		if n.kind == numFloat {
			return "float"
		}
		return "integer"
	}
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case *storage.Node:
		return "node"
	case *storage.Edge:
		return "relationship"
	}
	if isCollection(v) {
		if reflect.TypeOf(v).Kind() == reflect.Map {
			return "map"
		}
		return "list"
	}
	return "value"
}

// UnaryExpression represents unary operators: NOT, -
type UnaryExpression struct {
	Operator string // "NOT", "-"
	Operand  Expression
}

func (ue *UnaryExpression) Eval(context map[string]any) (bool, error) {
	switch ue.Operator {
	case "NOT":
		// Unknown removes the row, as false does.
		result, err := evalTri(ue, context)
		return result == triTrue, err
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
		result, err := evalTri(ue, context)
		if err != nil {
			return nil, err
		}
		return result.value(), nil
	case "-":
		val, err := evalOperand(ue.Operand, context)
		if err != nil {
			return nil, err
		}
		if val == nil {
			return nil, nil
		}
		switch v := val.(type) {
		case int64:
			return -v, nil
		case float64:
			return -v, nil
		default:
			return nil, refusef("cannot apply unary - to %s", cypherTypeName(val))
		}
	default:
		return nil, fmt.Errorf("unknown unary operator: %s", ue.Operator)
	}
}
