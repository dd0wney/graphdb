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
		}
	case *UnaryExpression:
		if e.Operator == "NOT" {
			operand, err := evalTri(e.Operand, context)
			if err != nil {
				// The operand cannot evaluate as a boolean (for example a
				// bare property). Coerce its value, as NOT always did.
				return triFromBool(!coerceToBool(extractValue(e.Operand, context))), nil
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
