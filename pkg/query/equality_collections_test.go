package query

import (
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

type (
	list = []any
	dict = map[string]any
)

// TCK Comparison1 scenarios 6 and 7 and the equality scenarios of List3, as
// the three-valued equality of two Go values.
func TestEqualTri_TCK(t *testing.T) {
	tests := []struct {
		name        string
		left, right any
		want        tri
	}{
		{"Comparison1[6] [1,2]=[1]", list{int64(1), int64(2)}, list{int64(1)}, triFalse},
		{"Comparison1[6] [null]=[1]", list{nil}, list{int64(1)}, triUnknown},
		{"Comparison1[6] ['a']=[1]", list{"a"}, list{int64(1)}, triFalse},
		{"Comparison1[6] [[1]]=[[1],[null]]", list{list{int64(1)}}, list{list{int64(1)}, list{nil}}, triFalse},
		{"Comparison1[6] [[1],[2]]=[[1],[null]]", list{list{int64(1)}, list{int64(2)}}, list{list{int64(1)}, list{nil}}, triUnknown},
		{"Comparison1[6] [[1],[2,3]]=[[1],[null]]", list{list{int64(1)}, list{int64(2), int64(3)}}, list{list{int64(1)}, list{nil}}, triFalse},

		{"Comparison1[7] {}={}", dict{}, dict{}, triTrue},
		{"Comparison1[7] {k:true}={k:true}", dict{"k": true}, dict{"k": true}, triTrue},
		{"Comparison1[7] {k:1}={k:1}", dict{"k": int64(1)}, dict{"k": int64(1)}, triTrue},
		{"Comparison1[7] {k:1.0}={k:1.0}", dict{"k": 1.0}, dict{"k": 1.0}, triTrue},
		{"Comparison1[7] {k:'abc'}={k:'abc'}", dict{"k": "abc"}, dict{"k": "abc"}, triTrue},
		{"Comparison1[7] {k:'a',l:2}={k:'a',l:2}", dict{"k": "a", "l": int64(2)}, dict{"k": "a", "l": int64(2)}, triTrue},
		{"Comparison1[7] {}={k:null}", dict{}, dict{"k": nil}, triFalse},
		{"Comparison1[7] {k:null}={}", dict{"k": nil}, dict{}, triFalse},
		{"Comparison1[7] {k:1}={k:1,l:null}", dict{"k": int64(1)}, dict{"k": int64(1), "l": nil}, triFalse},
		{"Comparison1[7] {k:null,l:1}={l:1}", dict{"k": nil, "l": int64(1)}, dict{"l": int64(1)}, triFalse},
		{"Comparison1[7] {k:null}={k:null,l:null}", dict{"k": nil}, dict{"k": nil, "l": nil}, triFalse},
		{"Comparison1[7] {k:null}={k:null}", dict{"k": nil}, dict{"k": nil}, triUnknown},
		{"Comparison1[7] {k:1}={k:null}", dict{"k": int64(1)}, dict{"k": nil}, triUnknown},
		{"Comparison1[7] {k:1,l:null}={k:null,l:null}", dict{"k": int64(1), "l": nil}, dict{"k": nil, "l": nil}, triUnknown},
		{"Comparison1[7] {k:1,l:null}={k:null,l:1}", dict{"k": int64(1), "l": nil}, dict{"k": nil, "l": int64(1)}, triUnknown},
		{"Comparison1[7] {k:1,l:null}={k:1,l:1}", dict{"k": int64(1), "l": nil}, dict{"k": int64(1), "l": int64(1)}, triUnknown},

		{"List3[1] [1,2]='foo'", list{int64(1), int64(2)}, "foo", triFalse},
		{"List3[2] [1]=[1,null]", list{int64(1)}, list{int64(1), nil}, triFalse},
		{"List3[3] [1,2]=[null,'foo']", list{int64(1), int64(2)}, list{nil, "foo"}, triFalse},
		{"List3[4] [1,2]=[null,2]", list{int64(1), int64(2)}, list{nil, int64(2)}, triUnknown},
		{"List3[5] [[1]]=[[1],[null]]", list{list{int64(1)}}, list{list{int64(1)}, list{nil}}, triFalse},
		{"List3[6] nested differ", list{list{int64(1), int64(2)}, list{int64(1), int64(3)}}, list{list{int64(1), int64(2)}, list{nil, "foo"}}, triFalse},
		{"List3[7] nested almost equal", list{list{int64(1), int64(2)}, list{"foo", "bar"}}, list{list{int64(1), int64(2)}, list{nil, "bar"}}, triUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalTri(tt.left, tt.right); got != tt.want {
				t.Errorf("equalTri(%#v, %#v) = %v, want %v", tt.left, tt.right, got, tt.want)
			}
			// Equality is symmetric, so a one-sided bug shows on one of the two.
			if got := equalTri(tt.right, tt.left); got != tt.want {
				t.Errorf("equalTri reversed = %v, want %v", got, tt.want)
			}
		})
	}
}

// The rules that the TCK rows do not reach: scalars, a list against a map, the
// typed slices that a stored array reads back as, and a false pair that beats
// an unknown pair wherever it sits.
func TestEqualTri_Rules(t *testing.T) {
	tests := []struct {
		name        string
		left, right any
		want        tri
	}{
		{"null=null", nil, nil, triUnknown},
		{"null=1", nil, int64(1), triUnknown},
		{"null=list", nil, list{}, triUnknown},
		{"int=float", int64(1), 1.0, triTrue},
		{"NaN=NaN", math.NaN(), math.NaN(), triFalse},
		{"string=int", "1", int64(1), triFalse},
		{"bool=bool", true, true, triTrue},
		{"list=map", list{}, dict{}, triFalse},
		{"map=int", dict{}, int64(1), triFalse},
		{"[]=[]", list{}, list{}, triTrue},
		{"[]=[null]", list{}, list{nil}, triFalse},
		{"unknown before false", list{nil, int64(1)}, list{int64(1), int64(2)}, triFalse},
		{"false before unknown", list{int64(1), nil}, list{int64(2), int64(1)}, triFalse},
		{"[[null]]=[[null]]", list{list{nil}}, list{list{nil}}, triUnknown},
		{"[1,2]=[1,2.0]", list{int64(1), int64(2)}, list{int64(1), 2.0}, triTrue},
		{"[]int64 = []any", []int64{1, 2}, list{int64(1), 2.0}, triTrue},
		{"[]string = []any", []string{"a"}, list{"a"}, triTrue},
		{"[]string differs", []string{"a"}, list{"b"}, triFalse},
		{"[]int64 with null", []int64{1, 2}, list{int64(1), nil}, triUnknown},
		{"bytes equal", []byte("ab"), []byte("ab"), triTrue},
		{"bytes differ", []byte("ab"), []byte("ac"), triFalse},
		{"map[string]string", map[string]string{"k": "v"}, dict{"k": "v"}, triTrue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalTri(tt.left, tt.right); got != tt.want {
				t.Errorf("equalTri(%#v, %#v) = %v, want %v", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

// inSkipped lists the List5 scenarios that this suite cannot run, each with
// the reason. The parser is not changed for them.
//
// The parser refuses a list literal inside a list literal. Each of these 27
// scenarios has one, and each error is the one that the parser printed. The
// same 27 rows run with Go values in TestInTri_TCK_List5_Values.
var inSkipped = map[int]string{
	6:  "parser: invalid list element: expected value, got '[' at line 1, column 22",
	9:  "parser: invalid list element: expected value, got '[' at line 1, column 22",
	10: "parser: invalid list element: expected value, got '[' at line 1, column 22",
	11: "parser: invalid list element: expected value, got '[' at line 1, column 22",
	12: "parser: invalid list element: expected value, got '[' at line 1, column 22",
	13: "parser: invalid list element: expected value, got '[' at line 1, column 22",
	14: "parser: invalid list element: expected value, got '[' at line 1, column 9",
	15: "parser: invalid list element: expected value, got '[' at line 1, column 9",
	16: "parser: invalid list element: expected value, got '[' at line 1, column 9",
	17: "parser: invalid list element: expected value, got '[' at line 1, column 9",
	18: "parser: invalid list element: expected value, got '[' at line 1, column 9",
	19: "parser: invalid list element: expected value, got '[' at line 1, column 9",
	21: "parser: invalid list element: expected value, got '[' at line 1, column 19",
	23: "parser: invalid list element: expected value, got '[' at line 1, column 16",
	26: "parser: invalid list element: expected value, got '[' at line 1, column 19",
	27: "parser: invalid list element: expected value, got '[' at line 1, column 22",
	28: "parser: invalid list element: expected value, got '[' at line 1, column 19",
	29: "parser: invalid list element: expected value, got '[' at line 1, column 19",
	30: "parser: invalid list element: expected value, got '[' at line 1, column 22",
	31: "parser: invalid list element: expected value, got '[' at line 1, column 28",
	32: "parser: invalid list element: expected value, got '[' at line 1, column 19",
	33: "parser: invalid list element: expected value, got '[' at line 1, column 9",
	34: "parser: invalid list element: expected value, got '[' at line 1, column 19",
	35: "parser: invalid list element: expected value, got '[' at line 1, column 15",
	37: "parser: invalid list element: expected value, got '[' at line 1, column 18",
	39: "parser: invalid list element: expected value, got '[' at line 1, column 9",
	41: "parser: invalid list element: expected value, got '[' at line 1, column 9",
}

type inScenario struct {
	n     int
	query string
	want  any
}

// Every query is the text of the TCK scenario, List5.feature scenarios 5 to 41.
var inScenarios = []inScenario{
	{5, `RETURN 1 IN ['1', 2] AS res`, false},
	{6, `RETURN [1, 2] IN [1, [1, '2']] AS res`, false},
	{7, `RETURN [1] IN [1, 2] AS res`, false},
	{8, `RETURN [1, 2] IN [1, 2] AS res`, false},
	{9, `RETURN [1] IN [1, 2, [1]] AS res`, true},
	{10, `RETURN [1, 2] IN [1, [1, 2]] AS res`, true},
	{11, `RETURN [1, 2] IN [1, [2, 1]] AS res`, false},
	{12, `RETURN [1, 2] IN [1, [1, 2, 3]] AS res`, false},
	{13, `RETURN [1, 2] IN [1, [[1, 2]]] AS res`, false},
	{14, `RETURN [[1, 2], [3, 4]] IN [5, [[1, 2], [3, 4]]] AS res`, true},
	{15, `RETURN [[1, 2], 3] IN [1, [[1, 2], 3]] AS res`, true},
	{16, `RETURN [[1]] IN [2, [[1]]] AS res`, true},
	{17, `RETURN [[1, 3]] IN [2, [[1, 3]]] AS res`, true},
	{18, `RETURN [[1]] IN [2, [1]] AS res`, false},
	{19, `RETURN [[1, 3]] IN [2, [1, 3]] AS res`, false},
	{20, `RETURN null IN [null] AS res`, nil},
	{21, `RETURN [null] IN [[null]] AS res`, nil},
	{22, `RETURN [null] IN [null] AS res`, nil},
	{23, `RETURN [1] IN [[1, null]] AS res`, false},
	{24, `RETURN 3 IN [1, null, 3] AS res`, true},
	{25, `RETURN 4 IN [1, null, 3] AS res`, nil},
	{26, `RETURN [1, 2] IN [[null, 'foo'], [1, 2]] AS res`, true},
	{27, `RETURN [1, 2] IN [1, [1, 2], null] AS res`, true},
	{28, `RETURN [1, 2] IN [[null, 'foo']] AS res`, false},
	{29, `RETURN [1, 2] IN [[null, 2]] AS res`, nil},
	{30, `RETURN [1, 2] IN [1, [1, 2, null]] AS res`, false},
	{31, `RETURN [1, 2, null] IN [1, [1, 2, null]] AS res`, nil},
	{32, `RETURN [1, 2] IN [[null, 2], [1, 2]] AS res`, true},
	{33, `RETURN [[1, 2], [3, 4]] IN [5, [[1, 2], [3, 4], null]] AS res`, false},
	{34, `RETURN [1, 2] IN [[null, 2], [1, 3]] AS res`, nil},
	{35, `RETURN [] IN [[]] AS res`, true},
	{36, `RETURN [] IN [] AS res`, false},
	{37, `RETURN [] IN [1, []] AS res`, true},
	{38, `RETURN [] IN [1, 2] AS res`, false},
	{39, `RETURN [[]] IN [1, [[]]] AS res`, true},
	{40, `RETURN [] IN [1, 2, null] AS res`, nil},
	{41, `RETURN [[], []] IN [1, [[], []]] AS res`, true},
}

// parseError returns the parse error of a query, so a scenario that this
// parser does not accept is named instead of failing the run.
func parseError(text string) error {
	tokens, err := NewLexer(text).Tokenize()
	if err != nil {
		return err
	}
	_, err = NewParser(tokens).Parse()
	return err
}

func TestInTri_TCK_List5(t *testing.T) {
	for _, sc := range inScenarios {
		t.Run("List5["+strconv.Itoa(sc.n)+"]", func(t *testing.T) {
			if reason, skip := inSkipped[sc.n]; skip {
				t.Skipf("recorded skip: %s", reason)
			}
			ex := equalityNullStore(t, false)
			rs, err := runWithParams(t, ex, sc.query, nil)
			if err != nil {
				t.Fatalf("%s: %v", sc.query, err)
			}
			if len(rs.Rows) != 1 {
				t.Fatalf("%s: rows = %d, want 1", sc.query, len(rs.Rows))
			}
			if got := rs.Rows[0]["res"]; got != sc.want {
				t.Errorf("%s = %#v, want %#v", sc.query, got, sc.want)
			}
		})
	}
}

// List5 scenarios 5 to 41 with Go values, so the nested lists that the parser
// refuses still run against inTri.
func TestInTri_TCK_List5_Values(t *testing.T) {
	i := func(n int64) any { return n }
	tests := []struct {
		n           int
		left, right any
		want        tri
	}{
		{5, i(1), list{"1", i(2)}, triFalse},
		{6, list{i(1), i(2)}, list{i(1), list{i(1), "2"}}, triFalse},
		{7, list{i(1)}, list{i(1), i(2)}, triFalse},
		{8, list{i(1), i(2)}, list{i(1), i(2)}, triFalse},
		{9, list{i(1)}, list{i(1), i(2), list{i(1)}}, triTrue},
		{10, list{i(1), i(2)}, list{i(1), list{i(1), i(2)}}, triTrue},
		{11, list{i(1), i(2)}, list{i(1), list{i(2), i(1)}}, triFalse},
		{12, list{i(1), i(2)}, list{i(1), list{i(1), i(2), i(3)}}, triFalse},
		{13, list{i(1), i(2)}, list{i(1), list{list{i(1), i(2)}}}, triFalse},
		{14, list{list{i(1), i(2)}, list{i(3), i(4)}}, list{i(5), list{list{i(1), i(2)}, list{i(3), i(4)}}}, triTrue},
		{15, list{list{i(1), i(2)}, i(3)}, list{i(1), list{list{i(1), i(2)}, i(3)}}, triTrue},
		{16, list{list{i(1)}}, list{i(2), list{list{i(1)}}}, triTrue},
		{17, list{list{i(1), i(3)}}, list{i(2), list{list{i(1), i(3)}}}, triTrue},
		{18, list{list{i(1)}}, list{i(2), list{i(1)}}, triFalse},
		{19, list{list{i(1), i(3)}}, list{i(2), list{i(1), i(3)}}, triFalse},
		{20, nil, list{nil}, triUnknown},
		{21, list{nil}, list{list{nil}}, triUnknown},
		{22, list{nil}, list{nil}, triUnknown},
		{23, list{i(1)}, list{list{i(1), nil}}, triFalse},
		{24, i(3), list{i(1), nil, i(3)}, triTrue},
		{25, i(4), list{i(1), nil, i(3)}, triUnknown},
		{26, list{i(1), i(2)}, list{list{nil, "foo"}, list{i(1), i(2)}}, triTrue},
		{27, list{i(1), i(2)}, list{i(1), list{i(1), i(2)}, nil}, triTrue},
		{28, list{i(1), i(2)}, list{list{nil, "foo"}}, triFalse},
		{29, list{i(1), i(2)}, list{list{nil, i(2)}}, triUnknown},
		{30, list{i(1), i(2)}, list{i(1), list{i(1), i(2), nil}}, triFalse},
		{31, list{i(1), i(2), nil}, list{i(1), list{i(1), i(2), nil}}, triUnknown},
		{32, list{i(1), i(2)}, list{list{nil, i(2)}, list{i(1), i(2)}}, triTrue},
		{33, list{list{i(1), i(2)}, list{i(3), i(4)}}, list{i(5), list{list{i(1), i(2)}, list{i(3), i(4)}, nil}}, triFalse},
		{34, list{i(1), i(2)}, list{list{nil, i(2)}, list{i(1), i(3)}}, triUnknown},
		{35, list{}, list{list{}}, triTrue},
		{36, list{}, list{}, triFalse},
		{37, list{}, list{i(1), list{}}, triTrue},
		{38, list{}, list{i(1), i(2)}, triFalse},
		{39, list{list{}}, list{i(1), list{list{}}}, triTrue},
		{40, list{}, list{i(1), i(2), nil}, triUnknown},
		{41, list{list{}, list{}}, list{i(1), list{list{}, list{}}}, triTrue},
	}
	for _, tt := range tests {
		t.Run("List5["+strconv.Itoa(tt.n)+"]", func(t *testing.T) {
			got, err := inTri(tt.left, tt.right)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("inTri(%#v, %#v) = %v, want %v", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestInTri_Rules(t *testing.T) {
	tests := []struct {
		name        string
		left, right any
		want        tri
	}{
		{"null IN null", nil, nil, triUnknown},
		{"1 IN null", int64(1), nil, triUnknown},
		{"null IN []", nil, list{}, triFalse},
		{"null IN [1]", nil, list{int64(1)}, triUnknown},
		{"1 IN []int64", int64(1), []int64{1, 2}, triTrue},
		{"3 IN []int64", int64(3), []int64{1, 2}, triFalse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inTri(tt.left, tt.right)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("inTri(%#v, %#v) = %v, want %v", tt.left, tt.right, got, tt.want)
			}
		})
	}
	if _, err := inTri(int64(1), "abc"); err == nil {
		t.Error("1 IN 'abc': want an error, got none")
	}
}

// A scenario in inSkipped must still fail to parse, so the list cannot go
// stale when the parser improves.
func TestInTri_SkipListIsHonest(t *testing.T) {
	for _, sc := range inScenarios {
		if _, skip := inSkipped[sc.n]; !skip {
			continue
		}
		if err := parseError(sc.query); err == nil {
			t.Errorf("List5[%d] is in inSkipped but its query now parses: remove it", sc.n)
		}
	}
}

func TestEqualityLists_Return(t *testing.T) {
	tests := []struct {
		expr string
		want any
	}{
		{"[1, null] = [1, null]", nil},
		{"[1, 2] = [1]", false},
		{"[null] = [1]", nil},
		{"[1, 2] <> [1, null]", nil},
		{"[1, 2] <> [1, 3]", true},
		{"[1, 2] = [1, 2]", true},
		{"[1, null] <> [2, null]", true},
		{"[1, 2] <> [1]", true},
		{"NOT ([1, null] = [1, null])", nil},
		{"null IN [1]", nil},
		{"1 IN [null]", nil},
		{"1 IN [1, null]", true},
		{"null IN []", false},
		{"1 IN null", nil},
		{"NOT (1 IN [null])", nil},
		{"(1 IN [null]) IS NULL", true},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			ex := equalityNullStore(t, false)
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE p.id = 4 RETURN "+tt.expr+" AS x", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := rs.Rows[0]["x"]; got != tt.want {
				t.Errorf("RETURN %s = %#v, want %#v", tt.expr, got, tt.want)
			}
		})
	}
}

// listStore holds four T nodes. Nodes 1 to 3 have a = 1, 2, 3 and node 4 has
// no a. Nodes 1 and 2 have an integer list l.
func listStore(t *testing.T) *Executor {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	for _, props := range []map[string]storage.Value{
		{"id": storage.IntValue(1), "a": storage.IntValue(1), "l": storage.IntArrayValue([]int64{1, 2})},
		{"id": storage.IntValue(2), "a": storage.IntValue(2), "l": storage.IntArrayValue([]int64{1, 3})},
		{"id": storage.IntValue(3), "a": storage.IntValue(3)},
		{"id": storage.IntValue(4)},
	} {
		if _, err := gs.CreateNode([]string{"T"}, props); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}
	return NewExecutor(gs)
}

func TestInTri_Where(t *testing.T) {
	tests := []struct {
		name  string
		where string
		list  any
		want  []int64
	}{
		{"in [1,null]", "p.a IN $list", list{int64(1), nil}, []int64{1}},
		{"in [null]", "p.a IN $list", list{nil}, []int64{}},
		{"in []", "p.a IN $list", list{}, []int64{}},
		{"in nil", "p.a IN $list", nil, []int64{}},
		{"not in [2,null]", "NOT (p.a IN $list)", list{int64(2), nil}, []int64{}},
		{"not in [2,3]", "NOT (p.a IN $list)", list{int64(2), int64(3)}, []int64{1}},
		{"not in [null]", "NOT (p.a IN $list)", list{nil}, []int64{}},
		// x IN [] is false even for a null x (TCK List5 scenarios 35 to 40),
		// so NOT of it is true for node 4, whose a is missing.
		{"not in []", "NOT (p.a IN $list)", list{}, []int64{1, 2, 3, 4}},
		{"not in nil", "NOT (p.a IN $list)", nil, []int64{}},
		{"NOT IN [2,null]", "p.a NOT IN $list", list{int64(2), nil}, []int64{}},
		{"not in [2,null] or a=1", "NOT (p.a IN $list) OR p.a = 1", list{int64(2), nil}, []int64{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := listStore(t)
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE "+tt.where+" RETURN p.id AS id", map[string]any{"list": tt.list})
			if err != nil {
				t.Fatal(err)
			}
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WHERE %s with %#v: ids = %v, want %v", tt.where, tt.list, got, tt.want)
			}
		})
	}
}

// A stored list compared with a bound list that holds a null.
func TestEqualityLists_WhereNodeProperty(t *testing.T) {
	tests := []struct {
		where string
		param any
		want  []int64
	}{
		{"p.l = $v", list{int64(1), int64(2)}, []int64{1}},
		{"p.l = $v", list{int64(1), nil}, []int64{}},
		{"p.l <> $v", list{int64(1), nil}, []int64{}},
		{"p.l <> $v", list{int64(2), nil}, []int64{1, 2}},
		{"p.l = $v", list{int64(1)}, []int64{}},
		{"p.l <> $v", list{int64(1)}, []int64{1, 2}},
		{"p.l = $v", nil, []int64{}},
	}
	for i, tt := range tests {
		t.Run(strconv.Itoa(i)+" "+tt.where, func(t *testing.T) {
			ex := listStore(t)
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE "+tt.where+" RETURN p.id AS id", map[string]any{"v": tt.param})
			if err != nil {
				t.Fatal(err)
			}
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WHERE %s with %#v: ids = %v, want %v", tt.where, tt.param, got, tt.want)
			}
		})
	}
}

// The simple form of CASE matches a branch only when the equality is true.
func TestSimpleCase_ListOperand(t *testing.T) {
	tests := []struct {
		expr string
		want any
	}{
		{"CASE [1, null] WHEN [1, null] THEN 'm' ELSE 'e' END", "e"},
		{"CASE [1, 2] WHEN [1, null] THEN 'm' ELSE 'e' END", "e"},
		{"CASE [1, 2] WHEN [1, 3] THEN 'm' WHEN [1, 2] THEN 'n' ELSE 'e' END", "n"},
		{"CASE [1, 2] WHEN [1, 2] THEN 'm' ELSE 'e' END", "m"},
		{"CASE [1, 2] WHEN [2, null] THEN 'm' ELSE 'e' END", "e"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			ex := equalityNullStore(t, false)
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE p.id = 4 RETURN "+tt.expr+" AS x", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := rs.Rows[0]["x"]; got != tt.want {
				t.Errorf("%s = %#v, want %#v", tt.expr, got, tt.want)
			}
		})
	}
}
