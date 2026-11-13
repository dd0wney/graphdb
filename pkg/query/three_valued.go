package query

import (
	"fmt"
	"strings"
)

// tri is a three-valued truth value. openCypher gives null to an ordering
// comparison of values that have no order and to = and <> with a null operand,
// and NOT, AND and OR propagate it by the Kleene rules.
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
		return e.Operator == "AND" || e.Operator == "OR" || e.Operator == "IN" || isOrderingOp(e.Operator)
	case *UnaryExpression:
		return e.Operator == "NOT"
	default:
		return false
	}
}

// equalityTri is the one place that decides = and != (the parser reads <> as
// !=). A null operand makes the result unknown, and so does an operand that is
// itself unknown: null = null is unknown, and WHERE removes the row. Only a
// null operand does this. Two non-null values of different types are not
// equal, so 'a' = 1 is false and 'a' <> 1 is true, as in openCypher. Lists and
// maps follow equalTri: a null inside gives unknown unless a length, a key set
// or another element already makes the result false.
func equalityTri(op string, left, right Expression, context map[string]any) (tri, error) {
	l, lUnknown, err := equalityOperand(left, context)
	if err != nil {
		return triFalse, err
	}
	r, rUnknown, err := equalityOperand(right, context)
	if err != nil {
		return triFalse, err
	}
	if lUnknown || rUnknown {
		return triUnknown, nil
	}
	eq := equalTri(l, r)
	if op == "!=" {
		return triNot(eq), nil
	}
	return eq, nil
}

// equalityOperand returns the value of an operand, or unknown=true when the
// operand is a three-valued expression that evaluates to unknown. An operand
// that errors with anything but a refusal reads as nil, as it always did.
func equalityOperand(expr Expression, context map[string]any) (val any, unknown bool, err error) {
	if !isTriExpr(expr) {
		v, err := evalOperand(expr, context)
		return v, false, err
	}
	t, err := evalTri(expr, context)
	if err != nil {
		if isRefusal(err) {
			return nil, false, err
		}
		return nil, false, nil
	}
	if t == triUnknown {
		return nil, true, nil
	}
	return t == triTrue, false, nil
}

// triFromValue is the one rule for a value used as a boolean. null is
// unknown, a boolean is its value, and anything else is a type error that the
// caller can fix. The message names the Cypher type and never the value,
// because a server shows it and the value comes from the graph.
func triFromValue(v any) (tri, error) {
	switch b := v.(type) {
	case nil:
		return triUnknown, nil
	case bool:
		return triFromBool(b), nil
	default:
		return triFalse, refusef("a boolean is necessary here, but the value is %s %s", article(cypherTypeName(v)), cypherTypeName(v))
	}
}

// article gives "an" before the Cypher type words that start with a vowel.
func article(word string) string {
	if strings.ContainsRune("aeiou", rune(word[0])) {
		return "an"
	}
	return "a"
}

// valueTri reads a value-type expression as a boolean through its value. It
// does not call Eval: the Eval of a value type calls evalPredicate, which
// calls evalTri, which would loop.
func valueTri(expr Expression, context map[string]any) (tri, error) {
	v, err := evalOperand(expr, context)
	if err != nil {
		return triFalse, err
	}
	return triFromValue(v)
}

// evalPredicate decides whether a row or a CASE branch is selected. Only true
// selects: unknown and false both give false. An error, a RefusalError
// included, goes to the caller, which decides what a lenient filter does.
//
// A conjunct of the top-level AND chain that is false or unknown excludes the
// row, and then an error in another conjunct is discarded. The reason: the row
// is not in the result under any plan, so the error tells the caller nothing,
// and an index on one conjunct would never have read the row. Without this the
// answer of a query depends on the plan and on the order of the operands. Every
// caller selects on true only, so the rule holds for each of them. When no
// conjunct excludes the row, the first error stays. The walk runs only after
// an error, so a function operand can run twice on a row that fails.
func evalPredicate(expr Expression, context map[string]any) (bool, error) {
	// The row with no error takes the same path as before. Only an error starts
	// the walk, which evaluates the chain again, so the walk costs nothing on
	// the common path.
	t, err := evalTri(expr, context)
	if err == nil {
		return t == triTrue, nil
	}
	var firstErr error
	if excludedByConjunct(expr, context, &firstErr) {
		return false, nil
	}
	if firstErr != nil {
		return false, firstErr
	}
	return true, nil
}

// excludedByConjunct walks the AND chain of expr from left to right, down
// through AND nodes only, as extractIndexableCondition does. It stops at the
// first conjunct that is false or unknown. An error does not stop it: the walk
// records the first error in firstErr and goes on, to look for a conjunct that
// excludes the row.
func excludedByConjunct(expr Expression, context map[string]any, firstErr *error) bool {
	if be, ok := expr.(*BinaryExpression); ok && be.Operator == "AND" {
		return excludedByConjunct(be.Left, context, firstErr) ||
			excludedByConjunct(be.Right, context, firstErr)
	}
	t, err := evalTri(expr, context)
	if err != nil {
		if *firstErr == nil {
			*firstErr = err
		}
		return false
	}
	return t != triTrue
}

// evalTri evaluates expr with three-valued logic. Any expression other than
// NOT, AND, OR and an ordering comparison keeps its two-valued Eval result.
//
// AND and OR keep the short-circuit order of the two-valued code, and only
// where the Kleene rule allows it: false AND x and true OR x skip x, but
// unknown AND x and unknown OR x evaluate x.
//
// An error in one operand of AND or OR is discarded when the other operand
// decides the result: false AND error is false, and true OR error is true. The
// reason: the result is the same whatever the failed operand would have given,
// so the answer must not depend on the operand order or on the plan. In each
// other case an error stays an error, and when both operands fail the error of
// the left operand wins. The second operand runs after an error in the first
// operand only, so the path with no error does the same work as before.
func evalTri(expr Expression, context map[string]any) (tri, error) {
	switch e := expr.(type) {
	case *BinaryExpression:
		switch {
		case e.Operator == "AND":
			return evalAnd(e, context)
		case e.Operator == "OR":
			return evalOr(e, context)
		case isOrderingOp(e.Operator):
			l, err := evalOperand(e.Left, context)
			if err != nil {
				return triFalse, err
			}
			r, err := evalOperand(e.Right, context)
			if err != nil {
				return triFalse, err
			}
			return orderingTri(e.Operator, l, r), nil
		case e.Operator == "=" || e.Operator == "!=":
			return equalityTri(e.Operator, e.Left, e.Right, context)
		case e.Operator == "IN":
			l, err := evalOperand(e.Left, context)
			if err != nil {
				return triFalse, err
			}
			r, err := evalOperand(e.Right, context)
			if err != nil {
				return triFalse, err
			}
			return inTri(l, r)
		}
		// IS NULL and the string predicates are two-valued. This is the
		// only Eval that evalTri calls: the Eval of every other type calls
		// evalPredicate, which calls evalTri.
		b, err := e.Eval(context)
		if err != nil {
			return triFalse, err
		}
		return triFromBool(b), nil
	case *PropertyExpression, *LiteralExpression, *FunctionCallExpression,
		*CaseExpression, *ArithmeticExpression:
		return valueTri(expr, context)
	case *ParameterExpression:
		// evalOperand reads a missing parameter as null, which would make it
		// unknown. It is an error, as it always was.
		if _, ok := context["$"+e.Name]; !ok {
			return triFalse, fmt.Errorf("missing parameter: $%s", e.Name)
		}
		return valueTri(expr, context)
	case *UnaryExpression:
		switch e.Operator {
		case "NOT":
			operand, err := evalTri(e.Operand, context)
			if err != nil {
				return triFalse, err
			}
			return triNot(operand), nil
		case "-":
			return valueTri(expr, context)
		default:
			return triFalse, fmt.Errorf("unknown unary operator: %s", e.Operator)
		}
	}
	// A type with no case above would loop if it reached Eval, because its Eval
	// calls evalPredicate. A stack overflow is fatal and recover cannot stop it,
	// so an unknown type is an error. A new Expression type needs a case here.
	return triFalse, fmt.Errorf("evalTri: no boolean rule for expression type %T", expr)
}

// evalAnd evaluates AND. A false operand fixes the result, and an error in the
// other operand is then discarded. The branches with no error come first and
// have the shape of the code before this rule, because every WHERE runs them.
func evalAnd(e *BinaryExpression, context map[string]any) (tri, error) {
	left, lerr := evalTri(e.Left, context)
	if lerr == nil {
		if left == triFalse {
			return triFalse, nil
		}
		right, rerr := evalTri(e.Right, context)
		if rerr == nil {
			return triAnd(left, right), nil
		}
		return triFalse, rerr
	}
	right, rerr := evalTri(e.Right, context)
	if rerr == nil && right == triFalse {
		return triFalse, nil
	}
	return triFalse, lerr
}

// evalOr evaluates OR. A true operand fixes the result, and an error in the
// other operand is then discarded.
func evalOr(e *BinaryExpression, context map[string]any) (tri, error) {
	left, lerr := evalTri(e.Left, context)
	if lerr == nil {
		if left == triTrue {
			return triTrue, nil
		}
		right, rerr := evalTri(e.Right, context)
		if rerr == nil {
			return triOr(left, right), nil
		}
		return triFalse, rerr
	}
	right, rerr := evalTri(e.Right, context)
	if rerr == nil && right == triTrue {
		return triTrue, nil
	}
	return triFalse, lerr
}
