package query

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"sort"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// openCypher has two notions of order. Comparability decides <, >, <= and >=:
// a null, or two values of different types, have none, and the operator gives
// null. Orderability decides ORDER BY, MIN and MAX: every pair of values has an
// order, so that a sort gives one answer for any input order. compareOrdered
// is the first, compareTotal the second. compareTotal must never replace
// compareOrdered in a filter, because it would turn "no order" into a result.

// The ranks of the types in the total order, ascending. The first nine follow
// the TCK, ReturnOrderBy1 [11]: map, node, relationship, list, path, string,
// boolean, number, NaN, null. DESC reverses the whole order, so null is first
// there ([12]).
//
// A path has no rank: the parser has no named path, so no path value reaches a
// row. A path ranks between list and string when one is added.
//
// rankBytes and rankOther are not in the TCK. A stored byte slice is a value
// that a row can hold, and it needs a place that no other type shares: it
// would otherwise tie with a string of equal bytes that valuesEqual calls
// unequal. rankOther takes any Go type that this file does not know, so that
// the order stays total and nothing panics.
const (
	rankMap = iota
	rankNode
	rankRelationship
	rankList
	rankBytes
	rankString
	rankBoolean
	rankNumber
	rankNaN
	rankOther
	rankNull
)

// compareTotal orders any two values for a sort, MIN and MAX. It never reports
// "no order", and it is a strict weak order: for all a, b and c, the order is
// transitive, and "equal" is transitive. A result of 0 means the two values
// are equal, or both are null, or both are NaN.
//
// The common pairs, two integers and two strings, return before any rank is
// computed.
func compareTotal(left, right any) int {
	switch l := left.(type) {
	case int64:
		if r, ok := right.(int64); ok {
			return cmpOrdered(l, r)
		}
	case string:
		if r, ok := right.(string); ok {
			return cmpOrdered(l, r)
		}
	}

	lr, rr := totalRank(left), totalRank(right)
	if lr != rr {
		return cmpOrdered(int64(lr), int64(rr))
	}

	switch lr {
	case rankNumber:
		// NaN has its own rank, so compareOrdered always orders a pair here.
		cmp, _ := compareOrdered(left, right)
		return cmp
	case rankString, rankBoolean:
		cmp, _ := compareOrdered(left, right)
		return cmp
	case rankNode:
		return cmpOrdered(left.(*storage.Node).ID, right.(*storage.Node).ID)
	case rankRelationship:
		return cmpOrdered(left.(*storage.Edge).ID, right.(*storage.Edge).ID)
	case rankBytes:
		return bytes.Compare(left.([]byte), right.([]byte))
	case rankList:
		return compareLists(sortList(left), sortList(right))
	case rankMap:
		return compareMaps(asStringMap(left), asStringMap(right))
	case rankOther:
		// Equal only when the type and the printed form agree, which is the
		// weakest test that cannot call two unequal values equal and cannot panic.
		lt, rt := fmt.Sprintf("%T", left), fmt.Sprintf("%T", right)
		if lt != rt {
			return cmpOrdered(lt, rt)
		}
		return cmpOrdered(fmt.Sprintf("%v", left), fmt.Sprintf("%v", right))
	default:
		// null and NaN: every value of the rank is equal to the others.
		return 0
	}
}

// totalRank names the place of a value's type in the order.
func totalRank(v any) int {
	switch x := v.(type) {
	case nil:
		return rankNull
	case string:
		return rankString
	case bool:
		return rankBoolean
	case float64:
		if math.IsNaN(x) {
			return rankNaN
		}
		return rankNumber
	case float32:
		if math.IsNaN(float64(x)) {
			return rankNaN
		}
		return rankNumber
	case int64, int, int32, uint64, uint32, int16, int8, uint, uint16, uint8:
		return rankNumber
	case *storage.Node:
		if x == nil {
			return rankNull
		}
		return rankNode
	case *storage.Edge:
		if x == nil {
			return rankNull
		}
		return rankRelationship
	case []byte:
		return rankBytes
	case []any, []string, []int64, []float64, []bool, []float32, []*storage.Edge:
		return rankList
	case map[string]any:
		return rankMap
	}

	// A named or unusual type. A string-keyed map and a slice are still a map
	// and a list, so a sort over them stays total.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return rankList
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			return rankMap
		}
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return rankNull
		}
	}
	if _, isNum := asNumber(v); isNum {
		return rankNumber
	}
	return rankOther
}

// isNullValue reports a value that MIN and MAX skip: untyped nil, and a typed
// nil pointer, which totalRank also places last.
func isNullValue(v any) bool {
	return totalRank(v) == rankNull
}

// sortList reads a value of rankList as []any. The common case, []any, is not
// copied. It is not asList (equal_tri.go): that one answers "is this a list",
// and refuses an array, where totalRank has already decided the rank here.
func sortList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	rv := reflect.ValueOf(v)
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

// compareLists compares element by element with compareTotal. The first pair
// that is not equal decides. When one list is a prefix of the other, the
// shorter one is first (TCK ReturnOrderBy1 [9]).
func compareLists(l, r []any) int {
	n := min(len(l), len(r))
	for i := 0; i < n; i++ {
		if cmp := compareTotal(l[i], r[i]); cmp != 0 {
			return cmp
		}
	}
	return cmpOrdered(int64(len(l)), int64(len(r)))
}

// asStringMap reads a map value with string keys as map[string]any.
func asStringMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	rv := reflect.ValueOf(v)
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = iter.Value().Interface()
	}
	return out
}

// compareMaps gives two maps a fixed order. The TCK has no scenario with two
// different maps, so the rule here is this code base's own, and its only
// purpose is a total order: compare the sorted key lists first, as lists of
// strings, then the values in key order, each with compareTotal. Two maps are
// equal when they have the same keys and equal values.
func compareMaps(l, r map[string]any) int {
	lk, rk := sortedKeys(l), sortedKeys(r)
	n := min(len(lk), len(rk))
	for i := 0; i < n; i++ {
		if cmp := cmpOrdered(lk[i], rk[i]); cmp != 0 {
			return cmp
		}
	}
	if cmp := cmpOrdered(int64(len(lk)), int64(len(rk))); cmp != 0 {
		return cmp
	}
	for _, k := range lk {
		if cmp := compareTotal(l[k], r[k]); cmp != 0 {
			return cmp
		}
	}
	return 0
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
