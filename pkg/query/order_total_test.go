package query

import (
	"fmt"
	"math"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// totalCmp is the comparison under test. The tests go through this name, so the
// same checks ran against compareValues (the function that sortRows used
// before this change) and failed there.
var totalCmp = compareTotal

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// orderValues is the value set for the order properties. It holds each type
// that a row can carry. It leaves out a NaN and an infinity inside a list or a
// map, because valuesEqual compares collections through JSON, which cannot
// encode them, so valuesEqual would call two equal lists unequal. It leaves out
// an integer beyond 2^53 beside a float: compareOrdered compares such a pair
// through float64, so it is not transitive there (see
// TestCompareTotal_IntegerBeyondFloatPrecisionKnownGap).
func orderValues() []any {
	n1, n2, n3 := &storage.Node{ID: 1}, &storage.Node{ID: 2}, &storage.Node{ID: 3}
	e1, e2 := &storage.Edge{ID: 1}, &storage.Edge{ID: 2}
	return []any{
		// null
		nil,
		// numbers, across integer and float kinds
		int64(-5), int64(-1), int64(0), int64(1), int64(2), int64(3), int64(10), int(7), int32(2),
		uint64(math.MaxInt64) + 1, int64(math.MinInt64), int64(math.MaxInt64),
		float64(-1.5), float64(0), float64(0.5), float64(1), float64(1.5), float64(2), float64(999.99),
		float32(0.5), float64(1e300), math.Inf(1), math.Inf(-1),
		// NaN
		math.NaN(), float32(math.NaN()),
		// strings
		"", " ", ".*", "a", "B", "abc", "abc1", "b", "one",
		// booleans
		false, true,
		// lists, including nested and null elements (TCK ReturnOrderBy1 [9])
		[]any{}, []any{"a"}, []any{"a", int64(1)}, []any{int64(1)}, []any{int64(1), "a"},
		[]any{int64(1), nil}, []any{nil, int64(1)}, []any{nil, int64(2)}, []any{nil}, []any{nil, nil},
		[]any{[]any{}}, []any{[]any{int64(1)}}, []any{[]any{int64(1)}, []any{int64(2)}},
		[]any{[]any{nil}, int64(1)}, []any{float64(1)}, []any{true}, []any{map[string]any{"a": int64(1)}},
		[]string{"a", "b"}, []int64{1, 2}, []float64{0.5}, []bool{false, true}, []float32{0.5, 1},
		// maps
		map[string]any{}, map[string]any{"a": "map"}, map[string]any{"a": int64(1)},
		map[string]any{"a": float64(1)}, map[string]any{"a": nil}, map[string]any{"b": int64(1)},
		map[string]any{"a": int64(1), "b": int64(2)}, map[string]any{"a": []any{int64(1), nil}},
		map[string]any{"a": map[string]any{"b": int64(1)}},
		// a node and a relationship as a row holds them, and a list of relationships
		n1, n2, n3, e1, e2, []*storage.Edge{e1, e2}, []*storage.Edge{e1},
		// bytes. An empty slice is left out: valuesEqual encodes it as "" and so
		// calls it equal to the empty string, which compareTotal keeps apart on
		// purpose (a byte slice has its own rank).
		[]byte("a"), []byte("ab"),
	}
}

func isNaNValue(v any) bool {
	switch f := v.(type) {
	case float64:
		return math.IsNaN(f)
	case float32:
		return math.IsNaN(float64(f))
	}
	return false
}

// equalForOrder is the oracle for "compare reports 0": the two values are equal
// by valuesEqual, or both are null, or both are NaN. Two relationship lists
// compare through their ids, which valuesEqual cannot read, so they are
// matched by the same ids here.
func equalForOrder(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if isNaNValue(a) && isNaNValue(b) {
		return true
	}
	if la, ok := a.([]*storage.Edge); ok {
		lb, ok := b.([]*storage.Edge)
		if !ok || len(la) != len(lb) {
			return false
		}
		for i := range la {
			if la[i].ID != lb[i].ID {
				return false
			}
		}
		return true
	}
	return valuesEqual(a, b)
}

func TestCompareTotal_AntisymmetricAndZeroMeansEqual(t *testing.T) {
	vals := orderValues()
	failures := 0
	fail := func(format string, args ...any) {
		failures++
		if failures <= 5 {
			t.Errorf(format, args...)
		}
	}
	for _, a := range vals {
		if c := totalCmp(a, a); c != 0 {
			fail("cmp(%#v, itself) = %d, want 0", a, c)
		}
		for _, b := range vals {
			ab, ba := sign(totalCmp(a, b)), sign(totalCmp(b, a))
			if ab != -ba {
				fail("antisymmetry: cmp(%#v, %#v) = %d but cmp(%#v, %#v) = %d", a, b, ab, b, a, ba)
			}
			if ab == 0 && !equalForOrder(a, b) {
				fail("cmp(%#v, %#v) = 0 but the two values are not equal", a, b)
			}
			if ab != 0 && equalForOrder(a, b) {
				fail("cmp(%#v, %#v) = %d but the two values are equal", a, b, ab)
			}
		}
	}
	if failures > 5 {
		t.Errorf("%d failures in all; the first 5 are shown", failures)
	}
}

// TestCompareTotal_Transitive checks every triple. "Before or equal" must be
// transitive. With antisymmetry this also gives transitivity of "equal".
func TestCompareTotal_Transitive(t *testing.T) {
	vals := orderValues()
	failures := 0
	for _, a := range vals {
		for _, b := range vals {
			if totalCmp(a, b) > 0 {
				continue
			}
			for _, c := range vals {
				if totalCmp(b, c) > 0 || totalCmp(a, c) <= 0 {
					continue
				}
				failures++
				if failures <= 5 {
					t.Errorf("transitivity: %#v <= %#v and %#v <= %#v but %#v > %#v", a, b, b, c, a, c)
				}
			}
		}
	}
	if failures > 5 {
		t.Errorf("%d failing triples in all; the first 5 are shown", failures)
	}
}

// TestCompareTotal_TypeLadder pins the order of types from TCK
// ReturnOrderBy1 [11]: map, node, relationship, list, (path), string,
// boolean, number, NaN, null.
func TestCompareTotal_TypeLadder(t *testing.T) {
	ladder := []struct {
		name string
		v    any
	}{
		{"map", map[string]any{"a": "map"}},
		{"node", &storage.Node{ID: 1}},
		{"relationship", &storage.Edge{ID: 1}},
		{"list", []any{"list"}},
		{"string", "text"},
		{"boolean", false},
		{"number", 1.5},
		{"NaN", math.NaN()},
		{"null", nil},
	}
	for i := range ladder {
		for j := range ladder {
			want := sign(i - j)
			if got := sign(totalCmp(ladder[i].v, ladder[j].v)); got != want {
				t.Errorf("cmp(%s, %s) = %d, want %d", ladder[i].name, ladder[j].name, got, want)
			}
		}
	}
}

// TestCompareTotal_ListsFollowTCK is the table of ReturnOrderBy1 [9].
func TestCompareTotal_ListsFollowTCK(t *testing.T) {
	want := []any{
		[]any{}, []any{"a"}, []any{"a", int64(1)}, []any{int64(1)}, []any{int64(1), "a"},
		[]any{int64(1), nil}, []any{nil, int64(1)}, []any{nil, int64(2)},
	}
	for i := range want {
		for j := range want {
			if got := sign(totalCmp(want[i], want[j])); got != sign(i-j) {
				t.Errorf("cmp(%v, %v) = %d, want %d", want[i], want[j], got, sign(i-j))
			}
		}
	}
}

// TestCompareTotal_SortsToOnePermutationIndependentResult sorts each of the 720
// permutations of a mixed list and requires one result. With the old
// compareValues null was "equal" to 2 and 1 while 2 and 1 differ, so the result
// followed the input order.
func TestCompareTotal_SortsToOnePermutationIndependentResult(t *testing.T) {
	base := []any{int64(2), nil, int64(1), "a", true, 1.5}
	want := "[a true 1 1.5 2 <nil>]"
	seen := map[string][]any{}
	count := 0
	permute(base, func(p []any) {
		count++
		s := append([]any(nil), p...)
		sort.SliceStable(s, func(i, j int) bool { return totalCmp(s[i], s[j]) < 0 })
		seen[fmt.Sprint(s)] = append([]any(nil), p...)
	})
	if count != 720 {
		t.Fatalf("permutations = %d, want 720", count)
	}
	if len(seen) != 1 {
		n := 0
		for out, in := range seen {
			if n < 2 {
				t.Errorf("input %v sorted to %s", in, out)
			}
			n++
		}
		t.Errorf("%d different results from 720 permutations, want 1", len(seen))
		return
	}
	for out := range seen {
		if out != want {
			t.Errorf("sorted = %s, want %s", out, want)
		}
	}
}

func permute(a []any, visit func([]any)) {
	var rec func(k int)
	rec = func(k int) {
		if k == len(a) {
			visit(a)
			return
		}
		for i := k; i < len(a); i++ {
			a[k], a[i] = a[i], a[k]
			rec(k + 1)
			a[k], a[i] = a[i], a[k]
		}
	}
	rec(0)
}

// TestCompareTotal_MapOrderIsFixed pins the order that this code base defines
// for two maps. The TCK has no scenario for it.
func TestCompareTotal_MapOrderIsFixed(t *testing.T) {
	ordered := []any{
		map[string]any{},
		map[string]any{"a": int64(1)},
		map[string]any{"a": int64(2)},
		map[string]any{"a": int64(1), "b": int64(0)},
		map[string]any{"b": int64(0)},
	}
	for i := range ordered {
		for j := range ordered {
			if got := sign(totalCmp(ordered[i], ordered[j])); got != sign(i-j) {
				t.Errorf("cmp(%v, %v) = %d, want %d", ordered[i], ordered[j], got, sign(i-j))
			}
		}
	}
}

func TestCompareTotal_NodesAndRelationshipsOrderByID(t *testing.T) {
	if totalCmp(&storage.Node{ID: 1}, &storage.Node{ID: 2}) >= 0 {
		t.Error("node 1 must be before node 2")
	}
	if totalCmp(&storage.Edge{ID: 9}, &storage.Edge{ID: 3}) <= 0 {
		t.Error("relationship 9 must be after relationship 3")
	}
	if totalCmp(&storage.Node{ID: 5}, &storage.Node{ID: 5}) != 0 {
		t.Error("two nodes with one id must be equal")
	}
}

// TestCompareTotal_IntegerBeyondFloatPrecisionKnownGap records a limit that
// this change does not remove: compareOrdered compares an integer with a float
// through float64, so 2^53+1 equals the float 2^53, which equals the integer
// 2^53, and the two integers differ. The order is not transitive for that
// triple. Rule 6 of the task forbids a change to compareOrdered.
func TestCompareTotal_IntegerBeyondFloatPrecisionKnownGap(t *testing.T) {
	a, f, b := int64(1)<<53, float64(1<<53), int64(1)<<53+1
	if totalCmp(a, f) == 0 && totalCmp(f, b) == 0 && totalCmp(a, b) != 0 {
		t.Skipf("known gap: compareOrdered is not transitive for %d, %v, %d", a, f, b)
	}
}
