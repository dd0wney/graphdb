package query

import (
	"context"
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestCompareOrdered(t *testing.T) {
	big := int64(1) << 62
	tests := []struct {
		name        string
		l, r        any
		wantCmp     int
		wantOrdered bool
	}{
		{"int<int64", int(6), int64(7), -1, true},
		{"int64=int", int64(7), int(7), 0, true},
		{"int8>uint16", int8(9), uint16(3), 1, true},
		{"uint8=int32", uint8(5), int32(5), 0, true},
		{"uint<uint64", uint(1), uint64(2), -1, true},
		{"int16 vs float32 equal", int16(5), float32(5), 0, true},
		{"int vs float64 less", int(5), float64(5.5), -1, true},
		{"float64 vs int64 greater", float64(7.5), int64(7), 1, true},
		{"float32 vs float64", float32(2), float64(3), -1, true},
		{"large int64 differ by one", big, big + 1, -1, true},
		{"large int64 differ by one reversed", big + 1, big, 1, true},
		{"large int vs int64 differ by one", int(big + 1), big, 1, true},
		{"uint64 above int64 max vs int64", uint64(math.MaxInt64) + 1, int64(math.MaxInt64), 1, true},
		{"int64 vs uint64 above int64 max", int64(math.MaxInt64), uint64(math.MaxInt64) + 1, -1, true},
		{"uint64 above max pair", uint64(math.MaxUint64), uint64(math.MaxUint64) - 1, 1, true},
		{"uint64 above max vs float64", uint64(math.MaxUint64), float64(1), 1, true},
		{"negative int vs uint64", int64(-1), uint64(0), -1, true},
		{"string<string", "a", "b", -1, true},
		{"string=string", "a", "a", 0, true},
		{"string>string", "b", "a", 1, true},
		{"false<true", false, true, -1, true},
		{"true>false", true, false, 1, true},
		{"true=true", true, true, 0, true},
		{"nil vs nil", nil, nil, 0, false},
		{"nil vs int", nil, int64(0), 0, false},
		{"int vs nil", int64(0), nil, 0, false},
		{"nil vs string", nil, "a", 0, false},
		{"string vs int", "a", int64(1), 0, false},
		{"int vs string", int64(1), "a", 0, false},
		{"bool vs int", true, int64(1), 0, false},
		{"float vs bool", float64(1), true, 0, false},
		{"string vs bool", "true", true, 0, false},
		{"list vs list", []any{1}, []any{1}, 0, false},
		{"map vs map", map[string]any{}, map[string]any{}, 0, false},
		{"node vs node", &storage.Node{}, &storage.Node{}, 0, false},
		{"edge vs edge", &storage.Edge{}, &storage.Edge{}, 0, false},
		{"NaN vs float", math.NaN(), float64(1), 0, false},
		{"float vs NaN", float64(1), math.NaN(), 0, false},
		{"NaN vs int", math.NaN(), int64(1), 0, false},
		{"NaN vs NaN", math.NaN(), math.NaN(), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmp, ok := compareOrdered(tt.l, tt.r)
			if ok != tt.wantOrdered {
				t.Fatalf("compareOrdered(%#v, %#v) ok = %v, want %v", tt.l, tt.r, ok, tt.wantOrdered)
			}
			if ok && cmp != tt.wantCmp {
				t.Errorf("compareOrdered(%#v, %#v) cmp = %d, want %d", tt.l, tt.r, cmp, tt.wantCmp)
			}
		})
	}
}

func TestCompareOrdered_EveryNumberKindAgainstEveryKind(t *testing.T) {
	kinds := []func(n int) any{
		func(n int) any { return n },
		func(n int) any { return int8(n) },
		func(n int) any { return int16(n) },
		func(n int) any { return int32(n) },
		func(n int) any { return int64(n) },
		func(n int) any { return uint(n) },
		func(n int) any { return uint8(n) },
		func(n int) any { return uint16(n) },
		func(n int) any { return uint32(n) },
		func(n int) any { return uint64(n) },
		func(n int) any { return float32(n) },
		func(n int) any { return float64(n) },
	}
	for i, mkL := range kinds {
		for j, mkR := range kinds {
			for _, c := range []struct{ l, r, want int }{{1, 2, -1}, {2, 2, 0}, {3, 2, 1}} {
				cmp, ok := compareOrdered(mkL(c.l), mkR(c.r))
				if !ok || cmp != c.want {
					t.Errorf("kind %d (%T) vs kind %d (%T), %d vs %d: got (%d, %v), want (%d, true)",
						i, mkL(c.l), j, mkR(c.r), c.l, c.r, cmp, ok, c.want)
				}
			}
		}
	}
}

func TestValuesEqual_NumberKindsAndUnchangedPairs(t *testing.T) {
	tests := []struct {
		name string
		l, r any
		want bool
	}{
		{"int=int64", int(5), int64(5), true},
		{"int=float64", int(5), float64(5), true},
		{"int64=float64", int64(5), float64(5), true},
		{"int!=int64", int(5), int64(6), false},
		{"int!=float64", int(5), float64(5.5), false},
		{"int64!=float64", int64(5), float64(6), false},
		{"uint8=int", uint8(5), int(5), true},
		{"large int64 differ by one", int64(1) << 62, int64(1)<<62 + 1, false},
		{"nil=nil", nil, nil, true},
		{"string=string", "a", "a", true},
		{"string!=string", "a", "b", false},
		{"string vs number", "5", int64(5), false},
		{"number vs string", int64(5), "5", false},
		{"nil vs number", nil, int64(0), false},
		{"bool=bool", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valuesEqual(tt.l, tt.r); got != tt.want {
				t.Errorf("valuesEqual(%#v, %#v) = %v, want %v", tt.l, tt.r, got, tt.want)
			}
		})
	}
}

func setupCompareGraph(t *testing.T) *Executor {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	mk := func(id int64, age *int64, city string, flag bool) {
		props := map[string]storage.Value{
			"id":   storage.IntValue(id),
			"city": storage.StringValue(city),
			"flag": storage.BoolValue(flag),
		}
		if age != nil {
			props["age"] = storage.IntValue(*age)
		}
		if _, err := gs.CreateNode([]string{"Person"}, props); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}
	a20, a30, a40 := int64(20), int64(30), int64(40)
	mk(0, &a20, "Dublin", true)
	mk(1, &a30, "Cork", false)
	mk(2, &a40, "Galway", true)
	mk(3, nil, "Sligo", false)
	return NewExecutor(gs)
}

func idsOf(t *testing.T, rs *ResultSet) []int64 {
	t.Helper()
	out := []int64{}
	for _, row := range rs.Rows {
		v, ok := row["id"].(int64)
		if !ok {
			t.Fatalf("id is %T (%v), want int64", row["id"], row["id"])
		}
		out = append(out, v)
	}
	return out
}

func sortedIDs(t *testing.T, rs *ResultSet) []int64 {
	t.Helper()
	ids := idsOf(t, rs)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func TestCompareOrdered_CypherWhere(t *testing.T) {
	ex := setupCompareGraph(t)
	tests := []struct {
		where string
		want  []int64
	}{
		{"p.nosuch >= 0", []int64{}},
		{"p.nosuch > 0", []int64{}},
		{"p.nosuch < 0", []int64{}},
		{"p.nosuch <= 0", []int64{}},
		{"0 <= p.nosuch", []int64{}},
		{"p.city >= 1", []int64{}},
		{"p.age > 'x'", []int64{}},
		{"p.flag < true", []int64{1, 3}},
		{"p.flag >= true", []int64{0, 2}},
		{"p.age > 25", []int64{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.where, func(t *testing.T) {
			rs := parseAndExecute(t, ex, "MATCH (p:Person) WHERE "+tt.where+" RETURN p.id AS id")
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WHERE %s: ids = %v, want %v", tt.where, got, tt.want)
			}
		})
	}
}

func TestCompareOrdered_ParameterKinds(t *testing.T) {
	ex := setupCompareGraph(t)
	for name, min := range map[string]any{"float64": float64(25), "int": int(25), "int64": int64(25)} {
		t.Run(name, func(t *testing.T) {
			tokens, err := NewLexer("MATCH (p:Person) WHERE p.age > $min RETURN p.id AS id").Tokenize()
			if err != nil {
				t.Fatal(err)
			}
			q, err := NewParser(tokens).Parse()
			if err != nil {
				t.Fatal(err)
			}
			rs, err := ex.ExecuteWithParamsContext(context.Background(), q, map[string]any{"min": min})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := sortedIDs(t, rs), []int64{1, 2}; !reflect.DeepEqual(got, want) {
				t.Errorf("ids = %v, want %v", got, want)
			}
		})
	}
}

func TestCompareOrdered_SimpleCaseStringVsNumber(t *testing.T) {
	ex := setupCompareGraph(t)
	rs := parseAndExecute(t, ex, "MATCH (p:Person) RETURN CASE p.city WHEN 1 THEN 'num' ELSE 'other' END AS k")
	if len(rs.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rs.Rows))
	}
	for _, row := range rs.Rows {
		if row["k"] != "other" {
			t.Errorf("k = %v, want other", row["k"])
		}
	}
}

// The expected order is what the code returned before compareOrdered existed.
// This test pins it so the change cannot move it.
func TestCompareOrdered_OrderByUnchanged(t *testing.T) {
	ex := setupCompareGraph(t)
	rs := parseAndExecute(t, ex, "MATCH (p:Person) RETURN p.id AS id ORDER BY p.age")
	got := idsOf(t, rs)
	want := []int64{0, 1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ORDER BY p.age = %v, want %v", got, want)
	}
}

// KNOWN LIMIT: the executor has no three-valued logic. An ordering comparison
// with null gives false here, so NOT turns it into true. openCypher gives null
// for NOT null, and the WHERE would return zero rows. This test records the
// current result. Do not read it as the correct one.
func TestCompareOrdered_NotNullComparisonKnownLimit(t *testing.T) {
	ex := setupCompareGraph(t)
	rs := parseAndExecute(t, ex, "MATCH (p:Person) WHERE NOT (p.nosuch > 5) RETURN p.id AS id")
	if got, want := sortedIDs(t, rs), []int64{0, 1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("NOT (p.nosuch > 5) ids = %v, want %v (current behaviour, not openCypher)", got, want)
	}
}
