package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Helper function to evaluate comparisons
func evalComparison(left, right Expression, op string, context map[string]any) (bool, error) {
	// Extract actual values
	leftVal := extractValue(left, context)
	rightVal := extractValue(right, context)

	switch op {
	case "=":
		return valuesEqual(leftVal, rightVal), nil
	case "!=":
		return !valuesEqual(leftVal, rightVal), nil
	case ">", "<", ">=", "<=":
		// No order (null, mixed types) is null in openCypher, and a null
		// in WHERE removes the row. It is not an error.
		cmp, ok := compareOrdered(leftVal, rightVal)
		if !ok {
			return false, nil
		}
		switch op {
		case ">":
			return cmp > 0, nil
		case "<":
			return cmp < 0, nil
		case ">=":
			return cmp >= 0, nil
		default:
			return cmp <= 0, nil
		}
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
			// valuesEqual coerces int64 against float64, compares lists and
			// maps by value, and never reaches == with an uncomparable
			// type. compareValues was the fallback here, and it reports 0
			// for any two types it does not know, so a list or a string
			// matched a number.
			if valuesEqual(leftVal, item) {
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

// valuesEqual implements equality for the "=" / "!=" operators. It exists
// because a raw Go `==` on two `any` values is type-strict: int64(5) == float64(5)
// is false because the dynamic types differ. That silently broke `WHERE id(n) = $p`,
// since id() returns int64 while JSON-decoded query parameters arrive as float64
// (encoding/json unmarshals every JSON number into float64). The ordering
// operators (>, <, ...) already coerced numerics; equality did
// not. For numeric operands we route through compareOrdered (which handles every
// Go number kind); all other operands keep the previous strict `==` behavior,
// including nil == nil and string/bool comparison.
func valuesEqual(left, right any) bool {
	if isNumericValue(left) && isNumericValue(right) {
		cmp, ok := compareOrdered(left, right)
		return ok && cmp == 0
	}
	if isCollection(left) || isCollection(right) {
		return collectionsEqual(left, right)
	}
	return left == right
}

// isCollection reports a list or a map. == on two interface values holding the
// same such type panics at run time, so valuesEqual must not reach it with one.
func isCollection(v any) bool {
	if v == nil {
		return false
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Slice, reflect.Map:
		return true
	}
	return false
}

// collectionsEqual compares lists and maps by their JSON encoding, so the
// []string a stored list reads back as equals the []any a client bound, and
// map key order does not matter (encoding/json sorts map keys). Element order
// in a list does matter, as in Cypher.
func collectionsEqual(left, right any) bool {
	l, errL := json.Marshal(left)
	r, errR := json.Marshal(right)
	return errL == nil && errR == nil && bytes.Equal(l, r)
}

// isNumericValue reports whether v is a Go integer or float kind, the same set
// compareOrdered orders. valuesEqual relies on it: if it named a kind that
// compareOrdered cannot order, two such values would never be equal.
func isNumericValue(v any) bool {
	_, ok := asNumber(v)
	return ok
}

// numberKind says how a number is held for comparison.
type numberKind int

const (
	numInt     numberKind = iota // fits in int64, held exactly
	numBigUint                   // a uint64 above math.MaxInt64, held exactly
	numFloat
)

// number is a Go numeric value reduced to one of three exact forms, so that
// two integers never pass through float64.
type number struct {
	kind numberKind
	i    int64
	u    uint64
	f    float64
}

// asNumber reduces any Go integer or float kind to a number. A type switch is
// used because the set is closed and reflection costs an allocation per call.
func asNumber(v any) (number, bool) {
	switch n := v.(type) {
	case int:
		return number{kind: numInt, i: int64(n)}, true
	case int8:
		return number{kind: numInt, i: int64(n)}, true
	case int16:
		return number{kind: numInt, i: int64(n)}, true
	case int32:
		return number{kind: numInt, i: int64(n)}, true
	case int64:
		return number{kind: numInt, i: n}, true
	case uint:
		return fromUint64(uint64(n)), true
	case uint8:
		return number{kind: numInt, i: int64(n)}, true
	case uint16:
		return number{kind: numInt, i: int64(n)}, true
	case uint32:
		return number{kind: numInt, i: int64(n)}, true
	case uint64:
		return fromUint64(n), true
	case float32:
		return number{kind: numFloat, f: float64(n)}, true
	case float64:
		return number{kind: numFloat, f: n}, true
	default:
		return number{}, false
	}
}

func fromUint64(u uint64) number {
	if u <= math.MaxInt64 {
		return number{kind: numInt, i: int64(u)}
	}
	return number{kind: numBigUint, u: u}
}

// asFloat converts for a comparison where the other side is a float. Beyond
// 2^53 an integer loses precision here, which is the cost of comparing it with
// a float at all.
func (n number) asFloat() float64 {
	switch n.kind {
	case numInt:
		return float64(n.i)
	case numBigUint:
		return float64(n.u)
	default:
		return n.f
	}
}

func cmpOrdered[T int64 | uint64 | float64 | string](a, b T) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compareNumbers orders two numbers. Two integers compare exactly. A uint64
// above math.MaxInt64 is greater than every int64 and compares with another
// such value as a uint64, so no integer pair goes through float64. If either
// side is a float, both go through float64, and NaN has no order.
func compareNumbers(l, r number) (int, bool) {
	if l.kind == numFloat || r.kind == numFloat {
		lf, rf := l.asFloat(), r.asFloat()
		if math.IsNaN(lf) || math.IsNaN(rf) {
			return 0, false
		}
		return cmpOrdered(lf, rf), true
	}
	switch {
	case l.kind == numInt && r.kind == numInt:
		return cmpOrdered(l.i, r.i), true
	case l.kind == numBigUint && r.kind == numBigUint:
		return cmpOrdered(l.u, r.u), true
	case l.kind == numBigUint:
		return 1, true
	default:
		return -1, true
	}
}

// compareOrdered reports the order of two values and whether they have one.
// Numbers order with numbers, strings with strings, and false before true.
// Every other pair has no order: null on either side, a string against a
// number, a boolean against a number, a list, a map, a node, an edge, and NaN.
// openCypher gives null for such a comparison, so a caller that filters must
// treat ok == false as "no match".
func compareOrdered(left, right any) (cmp int, ok bool) {
	if ln, isNum := asNumber(left); isNum {
		rn, rIsNum := asNumber(right)
		if !rIsNum {
			return 0, false
		}
		return compareNumbers(ln, rn)
	}
	switch l := left.(type) {
	case string:
		r, isStr := right.(string)
		if !isStr {
			return 0, false
		}
		return cmpOrdered(l, r), true
	case bool:
		r, isBool := right.(bool)
		if !isBool {
			return 0, false
		}
		switch {
		case l == r:
			return 0, true
		case !l:
			return -1, true
		default:
			return 1, true
		}
	default:
		return 0, false
	}
}

// compareValues returns an ordering for every pair, so that a sort and
// MIN/MAX have something to work with. A result of 0 can mean "equal" or "no
// order between these two values". For that reason only a sort and MIN/MAX may
// call it. A filter must call compareOrdered, or an unordered pair passes.
func compareValues(left, right any) int {
	cmp, ok := compareOrdered(left, right)
	if !ok {
		return 0
	}
	return cmp
}
