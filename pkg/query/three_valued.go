package query

// tri is a three-valued truth value. openCypher gives null to an ordering
// comparison of values that have no order, and NOT, AND and OR propagate it by
// the Kleene rules. Only the ordering comparisons produce triUnknown so far.
// = and != stay two-valued (coord task v1.4-cypher-three-valued-equality).
type tri int

const (
	triFalse tri = iota
	triTrue
	triUnknown
)

func triFromBool(b bool) tri {
	if b {
		return triTrue
	}
	return triFalse
}

func triNot(v tri) tri {
	switch v {
	case triTrue:
		return triFalse
	case triFalse:
		return triTrue
	default:
		return triUnknown
	}
}

func triAnd(l, r tri) tri {
	if l == triFalse || r == triFalse {
		return triFalse
	}
	if l == triTrue && r == triTrue {
		return triTrue
	}
	return triUnknown
}

func triOr(l, r tri) tri {
	if l == triTrue || r == triTrue {
		return triTrue
	}
	if l == triFalse && r == triFalse {
		return triFalse
	}
	return triUnknown
}

// value is the form RETURN shows: null for unknown.
func (v tri) value() any {
	if v == triUnknown {
		return nil
	}
	return v == triTrue
}

// isOrderingOp reports the four operators whose result can be unknown.
func isOrderingOp(op string) bool {
	switch op {
	case ">", "<", ">=", "<=":
		return true
	default:
		return false
	}
}

// orderingTri is the one place that decides what an ordering comparison gives
// when the two values have no order. evalComparison and evalTri both call it.
func orderingTri(op string, leftVal, rightVal any) tri {
	cmp, ok := compareOrdered(leftVal, rightVal)
	if !ok {
		return triUnknown
	}
	switch op {
	case ">":
		return triFromBool(cmp > 0)
	case "<":
		return triFromBool(cmp < 0)
	case ">=":
		return triFromBool(cmp >= 0)
	default:
		return triFromBool(cmp <= 0)
	}
}

// isTriExpr reports the expression kinds that evalTri treats as three-valued.
func isTriExpr(expr Expression) bool {
	switch e := expr.(type) {
	case *BinaryExpression:
		return e.Operator == "AND" || e.Operator == "OR" || isOrderingOp(e.Operator)
	case *UnaryExpression:
		return e.Operator == "NOT"
	default:
		return false
	}
}

// equalityTri is the one place that decides = and != over a three-valued
// operand. The rule is narrow on purpose: only an operand that is itself an
// ordering comparison, AND, OR or NOT can make the result unknown. = and !=
// of plain values, null included, stay two-valued until coord task
// v1.4-cypher-three-valued-equality changes that.
func equalityTri(op string, left, right Expression, context map[string]any) tri {
	l, lUnknown := equalityOperand(left, context)
	r, rUnknown := equalityOperand(right, context)
	if lUnknown || rUnknown {
		return triUnknown
	}
	eq := valuesEqual(l, r)
	if op == "!=" {
		eq = !eq
	}
	return triFromBool(eq)
}

// equalityOperand returns the value of an operand, or unknown=true when the
// operand is a three-valued expression that evaluates to unknown. An operand
// that errors reads as nil, as extractValue always did.
func equalityOperand(expr Expression, context map[string]any) (val any, unknown bool) {
	if !isTriExpr(expr) {
		return extractValue(expr, context), false
	}
	t, err := evalTri(expr, context)
	if err != nil {
		return nil, false
	}
	if t == triUnknown {
		return nil, true
	}
	return t == triTrue, false
}

// bareTri reads a property, variable, parameter or literal used as a boolean.
// null is unknown, a boolean is its value, and any other value keeps the
// coerceToBool result. Eval refuses a bare property outright, and that error
// made AND and OR of two properties drop every row and NOT read null as true.
func bareTri(expr Expression, context map[string]any) tri {
	v := extractValue(expr, context)
	if v == nil {
		return triUnknown
	}
	return triFromBool(coerceToBool(v))
}

// evalTri evaluates expr with three-valued logic. Any expression other than
// NOT, AND, OR and an ordering comparison keeps its two-valued Eval result.
// AND and OR keep the short-circuit order of the two-valued code, and only
// where the Kleene rule allows it: false AND x and true OR x skip x, but
// unknown AND x and unknown OR x evaluate x.
func evalTri(expr Expression, context map[string]any) (tri, error) {
	switch e := expr.(type) {
	case *BinaryExpression:
		switch {
		case e.Operator == "AND":
			left, err := evalTri(e.Left, context)
			if err != nil || left == triFalse {
				return triFalse, err
			}
			right, err := evalTri(e.Right, context)
			if err != nil {
				return triFalse, err
			}
			return triAnd(left, right), nil
		case e.Operator == "OR":
			left, err := evalTri(e.Left, context)
			if err != nil {
				return triFalse, err
			}
			if left == triTrue {
				return triTrue, nil
			}
			right, err := evalTri(e.Right, context)
			if err != nil {
				return triFalse, err
			}
			return triOr(left, right), nil
		case isOrderingOp(e.Operator):
			return orderingTri(e.Operator, extractValue(e.Left, context), extractValue(e.Right, context)), nil
		case e.Operator == "=" || e.Operator == "!=":
			return equalityTri(e.Operator, e.Left, e.Right, context), nil
		}
	case *PropertyExpression, *LiteralExpression:
		return bareTri(expr, context), nil
	case *ParameterExpression:
		// A missing parameter stays an error, from Eval below.
		if _, ok := context["$"+e.Name]; ok {
			return bareTri(expr, context), nil
		}
	case *UnaryExpression:
		if e.Operator == "NOT" {
			operand, err := evalTri(e.Operand, context)
			if err != nil {
				// The operand still errors after bare operands became
				// three-valued. Coerce its value, as NOT always did, but
				// NOT of null is null in openCypher, not true.
				v := extractValue(e.Operand, context)
				if v == nil {
					return triUnknown, nil
				}
				return triFromBool(!coerceToBool(v)), nil
			}
			return triNot(operand), nil
		}
	}
	b, err := expr.Eval(context)
	if err != nil {
		return triFalse, err
	}
	return triFromBool(b), nil
}
