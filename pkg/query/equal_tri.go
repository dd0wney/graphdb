package query

import (
	"bytes"
	"errors"
	"reflect"
)

// equalTri is the one place that decides the three-valued equality of two
// values: the = and <> operators, IN, and the simple CASE all call it. It is
// recursive, because a null inside a list or a map makes the result unknown
// only when nothing else already makes it false (openCypher TCK Comparison1
// scenarios 6 and 7, List3, List5).
//
// valuesEqual stays two-valued for the callers that compare a stored value with
// a pattern (the MATCH property map, MERGE, compareStorageValue). Those are not
// the = operator, and a stored null must still match a pattern null there.
func equalTri(left, right any) tri {
	if left == nil || right == nil {
		return triUnknown
	}
	// The common scalar pairs are the hot path of every WHERE filter, so they
	// are decided by a type switch before any reflection runs.
	if t, ok := scalarEqualTri(left, right); ok {
		return t
	}
	if isNumericValue(left) && isNumericValue(right) {
		return numericEqualTri(left, right)
	}
	// []byte is a scalar: a bytes property is not a list of numbers.
	if lb, ok := left.([]byte); ok {
		rb, ok := right.([]byte)
		return triFromBool(ok && bytes.Equal(lb, rb))
	}
	if ll, ok := asList(left); ok {
		rl, ok := asList(right)
		if !ok {
			return triFalse
		}
		return listEqualTri(ll, rl)
	}
	if _, ok := asList(right); ok {
		return triFalse
	}
	if lm, ok := asMap(left); ok {
		rm, ok := asMap(right)
		if !ok {
			return triFalse
		}
		return mapEqualTri(lm, rm)
	}
	if _, ok := asMap(right); ok {
		return triFalse
	}
	// Two values of one uncomparable dynamic type would panic on ==.
	if lt := reflect.TypeOf(left); lt == reflect.TypeOf(right) && !lt.Comparable() {
		return triFalse
	}
	return triFromBool(left == right)
}

// scalarEqualTri decides two values of the common scalar types without
// reflection. ok is false when either value is not one of those types, which
// sends it to the general path.
func scalarEqualTri(left, right any) (t tri, ok bool) {
	switch l := left.(type) {
	case string:
		switch r := right.(type) {
		case string:
			return triFromBool(l == r), true
		case int64, int, float64, bool:
			return triFalse, true
		}
	case bool:
		switch r := right.(type) {
		case bool:
			return triFromBool(l == r), true
		case int64, int, float64, string:
			return triFalse, true
		}
	case int64:
		switch r := right.(type) {
		case int64:
			return triFromBool(l == r), true
		case int, float64:
			return numericEqualTri(left, right), true
		case string, bool:
			return triFalse, true
		}
	case int:
		switch right.(type) {
		case int64, int, float64:
			return numericEqualTri(left, right), true
		case string, bool:
			return triFalse, true
		}
	case float64:
		switch r := right.(type) {
		case float64:
			// == is false for NaN, as the rule needs.
			return triFromBool(l == r), true
		case int64, int:
			return numericEqualTri(left, right), true
		case string, bool:
			return triFalse, true
		}
	}
	return triFalse, false
}

// numericEqualTri compares two numbers by value. A float32 on either side is
// the element of a stored vector, which holds float32 precision, so the other
// number is rounded to float32 before the comparison: a JSON 0.1 is the
// float64 nearest 0.1, and widening the stored float32 would never equal it.
func numericEqualTri(left, right any) tri {
	l32, lIs32 := left.(float32)
	r32, rIs32 := right.(float32)
	if lIs32 || rIs32 {
		if !lIs32 {
			ln, _ := asNumber(left)
			l32 = float32(ln.asFloat())
		}
		if !rIs32 {
			rn, _ := asNumber(right)
			r32 = float32(rn.asFloat())
		}
		return triFromBool(l32 == r32)
	}
	cmp, ok := compareOrdered(left, right)
	return triFromBool(ok && cmp == 0)
}

// listEqualTri compares two lists. A length difference is false before any
// element is read, so [1] = [1, null] is false and not unknown.
func listEqualTri(l, r []any) tri {
	if len(l) != len(r) {
		return triFalse
	}
	result := triTrue
	for i := range l {
		switch equalTri(l[i], r[i]) {
		case triFalse:
			return triFalse
		case triUnknown:
			result = triUnknown
		}
	}
	return result
}

// mapEqualTri compares two maps. The key sets must be equal before any value
// is read, so {k: 1} = {k: 1, l: null} is false.
func mapEqualTri(l, r map[string]any) tri {
	if len(l) != len(r) {
		return triFalse
	}
	result := triTrue
	for k, lv := range l {
		rv, ok := r[k]
		if !ok {
			return triFalse
		}
		switch equalTri(lv, rv) {
		case triFalse:
			return triFalse
		case triUnknown:
			result = triUnknown
		}
	}
	return result
}

// asList reads a list. A stored array reads back as a typed slice such as
// []int64 or []string, and a bound parameter is a []any, so both are lists.
// []byte is not: it is a bytes value.
func asList(v any) ([]any, bool) {
	if l, ok := v.([]any); ok {
		return l, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// asMap reads a map with string keys. Any other key type is not a Cypher map.
func asMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out, true
}

var errInNeedsList = errors.New("IN requires a list on the right side")

// inTri is x IN list. A null list is unknown. An empty list is false even for
// a null x, because no element exists that x could equal (TCK List5 scenarios
// 35 to 40). Otherwise one true element makes it true, and failing that, one
// unknown element makes it unknown.
func inTri(left, right any) (tri, error) {
	if right == nil {
		return triUnknown, nil
	}
	list, ok := asList(right)
	if !ok {
		return triFalse, errInNeedsList
	}
	result := triFalse
	for _, item := range list {
		switch equalTri(left, item) {
		case triTrue:
			return triTrue, nil
		case triUnknown:
			result = triUnknown
		}
	}
	return result, nil
}
