package query

import (
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A refusal must not depend on the plan or on the order of AND and OR operands.
// A row that one conjunct already excludes is not in the result under any plan,
// so an error in another conjunct on that row tells the caller nothing.

type planNode map[string]storage.Value

func planStr(s string) storage.Value { return storage.StringValue(s) }
func planInt(i int64) storage.Value  { return storage.IntValue(i) }

// Every node has the key k, so a row has a name even when it has no id.
var (
	planNodeOK   = planNode{"k": planInt(1), "id": planInt(1), "q": storage.BoolValue(true), "ar": planInt(5)}
	nodeBadQ     = planNode{"k": planInt(2), "id": planInt(2), "q": planStr("str"), "ar": planStr("s")}
	nodeBadQID1  = planNode{"k": planInt(1), "id": planInt(1), "q": planStr("x"), "ar": planInt(5)}
	nodeNoID     = planNode{"k": planInt(3), "q": planStr("str")}
	nodeStrQID1  = planNode{"k": planInt(1), "id": planInt(1), "q": planStr("str")}
	nodeStrQID2  = planNode{"k": planInt(2), "id": planInt(2), "q": planStr("str")}
	fixtureBasic = []planNode{planNodeOK, nodeBadQ}
)

func planName(withIndex bool) string {
	if withIndex {
		return "indexed"
	}
	return "scan"
}

func planStore(t *testing.T, withIndex bool, nodes []planNode) (*storage.GraphStorage, *Executor) {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	if withIndex {
		if err := gs.CreatePropertyIndex("id", storage.TypeInt); err != nil {
			t.Fatalf("CreatePropertyIndex: %v", err)
		}
	}
	for _, n := range nodes {
		if _, err := gs.CreateNode([]string{"T"}, n); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}
	return gs, NewExecutor(gs)
}

func planHasLookup(t *testing.T, gs *storage.GraphStorage, ex *Executor, text string) bool {
	t.Helper()
	parsed := parseCallInput(t, text)
	plan := NewOptimizer(gs).Optimize(ex.buildExecutionPlan(parsed), parsed)
	for _, s := range plan.Steps {
		if _, ok := s.(*IndexLookupStep); ok {
			return true
		}
	}
	return false
}

// runPlan runs text on one plan. With the index it asserts that the plan holds
// an IndexLookupStep exactly when wantLookup, so a pass cannot come from an
// index that the optimizer skipped. With no index it asserts that none is used.
func runPlan(t *testing.T, withIndex, wantLookup bool, nodes []planNode, text string) (*ResultSet, error) {
	t.Helper()
	gs, ex := planStore(t, withIndex, nodes)
	got := planHasLookup(t, gs, ex, text)
	if want := withIndex && wantLookup; got != want {
		t.Fatalf("%s: IndexLookupStep in plan = %v, want %v for %s", planName(withIndex), got, want, text)
	}
	return ex.Execute(parseCallInput(t, text))
}

func planWantRefusal(t *testing.T, label string, rs *ResultSet, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: want a RefusalError, got rows %v", label, rs.Rows)
		return
	}
	var r *RefusalError
	if !errors.As(err, &r) {
		t.Errorf("%s: error %q is %T, want *RefusalError", label, err, err)
	}
}

func planWantIDs(t *testing.T, label string, rs *ResultSet, err error, want []int64) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: unexpected error: %v", label, err)
		return
	}
	if got := sortedIDs(t, rs); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: ids = %v, want %v", label, got, want)
	}
}

func TestRefusalPlan_MainQueryAnswersTheSameOnBothPlans(t *testing.T) {
	cases := []struct{ name, text string }{
		{"boolean", `MATCH (p:T) WHERE p.q AND p.id = 1 RETURN p.id AS id`},
		{"arithmetic", `MATCH (p:T) WHERE p.ar - 1 >= 0 AND p.id = 1 RETURN p.id AS id`},
	}
	for _, tc := range cases {
		for _, idx := range []bool{true, false} {
			rs, err := runPlan(t, idx, true, fixtureBasic, tc.text)
			planWantIDs(t, tc.name+" "+planName(idx), rs, err, []int64{1})
		}
	}
}

func TestRefusalPlan_OperandOrderDoesNotMatter(t *testing.T) {
	for _, text := range []string{
		`MATCH (p:T) WHERE p.q AND p.id = 1 RETURN p.id AS id`,
		`MATCH (p:T) WHERE p.id = 1 AND p.q RETURN p.id AS id`,
	} {
		rs, err := runPlan(t, false, false, fixtureBasic, text)
		planWantIDs(t, text, rs, err, []int64{1})
	}
}

func TestRefusalPlan_RefusalOnTheSelectedRowStays(t *testing.T) {
	fixture := []planNode{nodeBadQID1, nodeBadQ}
	for _, idx := range []bool{true, false} {
		rs, err := runPlan(t, idx, true, fixture, `MATCH (p:T) WHERE p.q AND p.id = 1 RETURN p.id AS id`)
		planWantRefusal(t, planName(idx), rs, err)
	}
}

func TestRefusalPlan_UnknownConjunctExcludesTheRow(t *testing.T) {
	fixture := []planNode{planNodeOK, nodeBadQ, nodeNoID}
	for _, idx := range []bool{true, false} {
		rs, err := runPlan(t, idx, true, fixture, `MATCH (p:T) WHERE p.q AND p.id = 1 RETURN p.k AS id`)
		planWantIDs(t, planName(idx), rs, err, []int64{1})
	}
}

func TestRefusalPlan_ThreeConjunctsInEachPosition(t *testing.T) {
	// p.q is the error conjunct, p.id = 1 excludes node 2, p.k > 0 is true.
	for _, where := range []string{
		"p.q AND p.id = 1 AND p.k > 0",
		"p.id = 1 AND p.q AND p.k > 0",
		"p.id = 1 AND p.k > 0 AND p.q",
		"p.q AND p.k > 0 AND p.id = 1",
		"p.k > 0 AND p.q AND p.id = 1",
		"p.k > 0 AND p.id = 1 AND p.q",
	} {
		for _, idx := range []bool{true, false} {
			rs, err := runPlan(t, idx, true, fixtureBasic, "MATCH (p:T) WHERE "+where+" RETURN p.k AS id")
			planWantIDs(t, where+" "+planName(idx), rs, err, []int64{1})
		}
	}
}

func TestRefusalPlan_Or(t *testing.T) {
	for _, where := range []string{"p.id = 1 OR p.q", "p.q OR p.id = 1"} {
		rs, err := runPlan(t, false, false, []planNode{nodeStrQID1}, "MATCH (p:T) WHERE "+where+" RETURN p.k AS id")
		planWantIDs(t, where+" on id 1", rs, err, []int64{1})

		rs, err = runPlan(t, false, false, []planNode{nodeStrQID2}, "MATCH (p:T) WHERE "+where+" RETURN p.k AS id")
		planWantRefusal(t, where+" on id 2", rs, err)
	}
}

func TestRefusalPlan_ValuePosition(t *testing.T) {
	rs, err := runPlan(t, false, false, fixtureBasic, `MATCH (p:T) RETURN p.k AS id, (p.q AND p.id = 1) AS x`)
	if err != nil {
		t.Fatalf("RETURN (p.q AND p.id = 1): %v", err)
	}
	for _, row := range rs.Rows {
		want := row["id"] == int64(1)
		if row["x"] != want {
			t.Errorf("row %v: x = %v, want %v", row["id"], row["x"], want)
		}
	}

	rs, err = runPlan(t, false, false, fixtureBasic, `MATCH (p:T) RETURN p.k AS id, (p.q AND p.id = 2) AS x`)
	planWantRefusal(t, "RETURN (p.q AND p.id = 2)", rs, err)
}

func TestRefusalPlan_Not(t *testing.T) {
	// NOT at the top gives the optimizer no equality to index, so both runs scan.
	for _, idx := range []bool{true, false} {
		rs, err := runPlan(t, idx, false, fixtureBasic, `MATCH (p:T) WHERE NOT (p.q AND p.id = 1) RETURN p.k AS id`)
		planWantIDs(t, planName(idx), rs, err, []int64{2})
	}
}

func TestRefusalPlan_CaseWhen(t *testing.T) {
	rs, err := runPlan(t, false, false, fixtureBasic,
		`MATCH (p:T) RETURN p.k AS id, CASE WHEN p.q AND p.id = 1 THEN 'y' ELSE 'n' END AS c`)
	if err != nil {
		t.Fatalf("CASE: %v", err)
	}
	for _, row := range rs.Rows {
		want := "n"
		if row["id"] == int64(1) {
			want = "y"
		}
		if row["c"] != want {
			t.Errorf("row %v: c = %v, want %v", row["id"], row["c"], want)
		}
	}
}

// The path with no error must do the same work as before: a decided result
// skips the operand that cannot change it.
func TestRefusalPlan_ShortCircuitStaysOnTheNoErrorPath(t *testing.T) {
	var calls atomic.Int64
	RegisterFunction("refusalplanprobe", func([]any) (any, error) {
		calls.Add(1)
		return true, nil
	})
	probe := func() Expression { return &FunctionCallExpression{Name: "refusalplanprobe"} }
	lit := func(v any) Expression { return &LiteralExpression{Value: v} }
	and := func(l, r Expression) Expression { return &BinaryExpression{Left: l, Operator: "AND", Right: r} }
	or := func(l, r Expression) Expression { return &BinaryExpression{Left: l, Operator: "OR", Right: r} }
	// A string is not a boolean, so this operand returns a RefusalError.
	bad := lit("str")

	cases := []struct {
		name      string
		run       func() error
		wantCalls int64
	}{
		{"false AND x", func() error { _, err := evalTri(and(lit(false), probe()), nil); return err }, 0},
		{"true OR x", func() error { _, err := evalTri(or(lit(true), probe()), nil); return err }, 0},
		{"true AND x", func() error { _, err := evalTri(and(lit(true), probe()), nil); return err }, 1},
		{"predicate: false first conjunct", func() error {
			_, err := evalPredicate(and(and(lit(false), probe()), probe()), nil)
			return err
		}, 0},
		// Unknown does not skip the next operand on the path with no error; that
		// is the work of the code before this change, and it stays.
		{"predicate: unknown first conjunct", func() error {
			_, err := evalPredicate(and(and(lit(nil), probe()), probe()), nil)
			return err
		}, 2},
		{"predicate: excluding conjunct after an error stops there", func() error {
			_, err := evalPredicate(and(and(bad, lit(false)), probe()), nil)
			return err
		}, 0},
		{"predicate: an error does not stop the scan for an excluding conjunct", func() error {
			_, err := evalPredicate(and(and(bad, probe()), lit(false)), nil)
			return err
		}, 1},
	}
	for _, tc := range cases {
		calls.Store(0)
		if err := tc.run(); err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if got := calls.Load(); got != tc.wantCalls {
			t.Errorf("%s: probe evaluated %d times, want %d", tc.name, got, tc.wantCalls)
		}
	}
}

func TestRefusalPlan_ErrorStaysWhenNothingDecides(t *testing.T) {
	lit := func(v any) Expression { return &LiteralExpression{Value: v} }
	bad := lit("str")
	for name, expr := range map[string]Expression{
		"true AND error":    &BinaryExpression{Left: lit(true), Operator: "AND", Right: bad},
		"unknown AND error": &BinaryExpression{Left: lit(nil), Operator: "AND", Right: bad},
		"error AND true":    &BinaryExpression{Left: bad, Operator: "AND", Right: lit(true)},
		"false OR error":    &BinaryExpression{Left: lit(false), Operator: "OR", Right: bad},
		"error OR unknown":  &BinaryExpression{Left: bad, Operator: "OR", Right: lit(nil)},
	} {
		if _, err := evalTri(expr, nil); err == nil {
			t.Errorf("evalTri %s: want an error", name)
		}
	}
}

// BenchmarkFilterAndChain measures the WHERE filter over an AND chain with no
// error, the path that the refusal rules must not slow down.
func BenchmarkFilterAndChain(b *testing.B) {
	gs, err := storage.NewGraphStorage(b.TempDir())
	if err != nil {
		b.Fatalf("NewGraphStorage: %v", err)
	}
	b.Cleanup(func() { _ = gs.Close() })
	bindings := make([]map[string]any, 0, 10000)
	for i := int64(0); i < 10000; i++ {
		n, err := gs.CreateNode([]string{"T"}, map[string]storage.Value{
			"a": storage.IntValue(i % 5), "b": storage.IntValue(i % 7), "c": storage.IntValue(i % 11),
		})
		if err != nil {
			b.Fatalf("CreateNode: %v", err)
		}
		bindings = append(bindings, map[string]any{"p": n})
	}
	tokens, err := NewLexer("MATCH (p:T) WHERE p.a > 1 AND p.b > 2 AND p.c > 3 RETURN p").Tokenize()
	if err != nil {
		b.Fatal(err)
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		b.Fatal(err)
	}
	expr := q.Where.Expression

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, binding := range bindings {
			if _, err := evalPredicate(expr, binding); err != nil {
				b.Fatal(err)
			}
		}
	}
}
