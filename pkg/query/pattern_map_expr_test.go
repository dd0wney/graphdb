package query

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// GitHub #765, part 2: a pattern property map takes an expression that reads a
// row variable, so one request can upsert a whole list:
//
//	UNWIND $items AS item WITH item MERGE (n:L {id: item.id}) SET n.text = item.text
//
// A literal and a $parameter keep their meaning. A variable differs for each
// row, so these tests run the same query against stores with and without a
// property index on the key, and count nodes after each run.

const batchUpsert = `UNWIND $items AS item WITH item MERGE (n:L {id: item.id}) SET n.text = item.text RETURN n.id`

func itemsParam(pairs ...string) map[string]any {
	items := make([]any, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, map[string]any{"id": pairs[i], "text": pairs[i+1]})
	}
	return map[string]any{"items": items}
}

func mapExprStore(t *testing.T, withIndex bool) (*storage.GraphStorage, *Executor) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	if withIndex {
		if err := gs.CreatePropertyIndex("id", storage.TypeString); err != nil {
			t.Fatalf("CreatePropertyIndex: %v", err)
		}
	}
	return gs, NewExecutor(gs)
}

func runMapExpr(t *testing.T, ex *Executor, text string, params map[string]any) (*ResultSet, error) {
	t.Helper()
	return ex.ExecuteWithParams(parsePatternQuery(t, text), params)
}

func textOf(t *testing.T, gs *storage.GraphStorage, id string) string {
	t.Helper()
	nodes, err := gs.FindNodesByLabelAcrossTenants("L")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if s, err := n.Properties["id"].AsString(); err == nil && s == id {
			text, _ := n.Properties["text"].AsString()
			return text
		}
	}
	return "<missing>"
}

func TestPatternMapExpr_BatchUpsert(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		t.Run(fmt.Sprintf("index=%v", withIndex), func(t *testing.T) {
			gs, ex := mapExprStore(t, withIndex)

			if _, err := runMapExpr(t, ex, batchUpsert, itemsParam("a", "one", "b", "two")); err != nil {
				t.Fatalf("first run: %v", err)
			}
			if n := len(gs.GetAllNodesAcrossTenants()); n != 2 {
				t.Fatalf("after the first run: %d nodes, want 2", n)
			}

			if _, err := runMapExpr(t, ex, batchUpsert, itemsParam("a", "one", "b", "two")); err != nil {
				t.Fatalf("second run: %v", err)
			}
			if n := len(gs.GetAllNodesAcrossTenants()); n != 2 {
				t.Fatalf("after the same run again: %d nodes, want 2", n)
			}

			if _, err := runMapExpr(t, ex, batchUpsert, itemsParam("a", "uno")); err != nil {
				t.Fatalf("third run: %v", err)
			}
			if n := len(gs.GetAllNodesAcrossTenants()); n != 2 {
				t.Fatalf("after the changed run: %d nodes, want 2", n)
			}
			if got := textOf(t, gs, "a"); got != "uno" {
				t.Errorf("text of a = %q, want uno", got)
			}
			if got := textOf(t, gs, "b"); got != "two" {
				t.Errorf("text of b = %q, want two (the run did not name b)", got)
			}
		})
	}
}

func TestPatternMapExpr_BatchUpsertReturnsEachRow(t *testing.T) {
	_, ex := mapExprStore(t, false)
	res, err := runMapExpr(t, ex, batchUpsert, itemsParam("a", "one", "b", "two"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	wantRows(t, res, "map[n.id:a]", "map[n.id:b]")
}

func TestPatternMapExpr_ArithmeticOfRowVariable(t *testing.T) {
	gs, ex := mapExprStore(t, false)
	if _, err := runMapExpr(t, ex, `UNWIND [1, 2] AS x WITH x CREATE (:N {v: x + 10})`, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	nodes, _ := gs.FindNodesByLabelAcrossTenants("N")
	var got []string
	for _, n := range nodes {
		v, _ := n.Properties["v"].AsInt()
		got = append(got, fmt.Sprint(v))
	}
	if len(got) != 2 || !(got[0] == "11" && got[1] == "12" || got[0] == "12" && got[1] == "11") {
		t.Fatalf("v values = %v, want 11 and 12", got)
	}
}

func TestPatternMapExpr_Create(t *testing.T) {
	gs, ex := mapExprStore(t, false)
	if _, err := runMapExpr(t, ex, `UNWIND $items AS item WITH item CREATE (n:L {id: item.id, text: item.text})`, itemsParam("a", "one", "b", "two")); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if n := len(gs.GetAllNodesAcrossTenants()); n != 2 {
		t.Fatalf("%d nodes, want 2", n)
	}
	if got := textOf(t, gs, "b"); got != "two" {
		t.Errorf("text of b = %q, want two", got)
	}
}

func TestPatternMapExpr_MatchAndOptionalMatch(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		t.Run(fmt.Sprintf("index=%v", withIndex), func(t *testing.T) {
			gs, ex := mapExprStore(t, withIndex)
			mustOracleNode(t, gs, "L", map[string]storage.Value{"id": storage.StringValue("a")})
			mustOracleNode(t, gs, "L", map[string]storage.Value{"id": storage.StringValue("b")})

			t.Run("MATCH found", func(t *testing.T) {
				res, err := runMapExpr(t, ex, `UNWIND $ids AS x WITH x MATCH (n:L {id: x}) RETURN x, n.id`, map[string]any{"ids": []any{"a"}})
				if err != nil {
					t.Fatal(err)
				}
				wantRows(t, res, "map[n.id:a x:a]")
			})
			t.Run("MATCH missing", func(t *testing.T) {
				res, err := runMapExpr(t, ex, `UNWIND $ids AS x WITH x MATCH (n:L {id: x}) RETURN x, n.id`, map[string]any{"ids": []any{"zzz"}})
				if err != nil {
					t.Fatal(err)
				}
				wantRows(t, res)
			})
			t.Run("MATCH each row on its own", func(t *testing.T) {
				res, err := runMapExpr(t, ex, `UNWIND $ids AS x WITH x MATCH (n:L {id: x}) RETURN x, n.id`, map[string]any{"ids": []any{"a", "zzz", "b"}})
				if err != nil {
					t.Fatal(err)
				}
				wantRows(t, res, "map[n.id:a x:a]", "map[n.id:b x:b]")
			})
			t.Run("OPTIONAL MATCH found", func(t *testing.T) {
				res, err := runMapExpr(t, ex, `UNWIND $ids AS x WITH x OPTIONAL MATCH (n:L {id: x}) RETURN x, n.id`, map[string]any{"ids": []any{"a"}})
				if err != nil {
					t.Fatal(err)
				}
				wantRows(t, res, "map[n.id:a x:a]")
			})
			t.Run("OPTIONAL MATCH missing", func(t *testing.T) {
				res, err := runMapExpr(t, ex, `UNWIND $ids AS x WITH x OPTIONAL MATCH (n:L {id: x}) RETURN x, n.id`, map[string]any{"ids": []any{"zzz"}})
				if err != nil {
					t.Fatal(err)
				}
				wantRows(t, res, "map[n.id:<nil> x:zzz]")
			})
		})
	}
}

func TestPatternMapExpr_RelationshipProperty(t *testing.T) {
	gs, ex := mapExprStore(t, false)
	mustOracleNode(t, gs, "P", map[string]storage.Value{"name": storage.StringValue("x")})
	mustOracleNode(t, gs, "P", map[string]storage.Value{"name": storage.StringValue("y")})

	if _, err := runMapExpr(t, ex,
		`UNWIND [1, 2] AS w WITH w MATCH (a:P {name: 'x'}), (b:P {name: 'y'}) CREATE (a)-[:R {w: w}]->(b)`, nil); err != nil {
		t.Fatalf("CREATE relationship: %v", err)
	}
	edges := edgesOfType(t, gs, "R")
	if len(edges) != 2 {
		t.Fatalf("%d R edges, want 2", len(edges))
	}

	res, err := runMapExpr(t, ex,
		`UNWIND [2, 5] AS w WITH w MATCH (a:P)-[r:R {w: w}]->(b:P) RETURN w, b.name`, nil)
	if err != nil {
		t.Fatalf("MATCH relationship: %v", err)
	}
	wantRows(t, res, "map[b.name:y w:2]")

	// MERGE of a relationship with a row value finds the edge the first
	// run made and adds none.
	merge := `UNWIND [1, 3] AS w WITH w MATCH (a:P {name: 'x'}), (b:P {name: 'y'}) MERGE (a)-[:R {w: w}]->(b)`
	if _, err := runMapExpr(t, ex, merge, nil); err != nil {
		t.Fatalf("MERGE relationship: %v", err)
	}
	if n := len(edgesOfType(t, gs, "R")); n != 3 {
		t.Fatalf("%d R edges after MERGE of w=1 (exists) and w=3 (new), want 3", n)
	}
}

func TestPatternMapExpr_MergeOnCreateOnMatch(t *testing.T) {
	gs, ex := mapExprStore(t, false)
	q := `UNWIND $items AS item WITH item MERGE (n:L {id: item.id}) ON CREATE SET n.text = item.text, n.fresh = true ON MATCH SET n.text = item.text, n.fresh = false`
	if _, err := runMapExpr(t, ex, q, itemsParam("a", "one")); err != nil {
		t.Fatal(err)
	}
	if _, err := runMapExpr(t, ex, q, itemsParam("a", "uno", "b", "two")); err != nil {
		t.Fatal(err)
	}
	if n := len(gs.GetAllNodesAcrossTenants()); n != 2 {
		t.Fatalf("%d nodes, want 2", n)
	}
	nodes, _ := gs.FindNodesByLabelAcrossTenants("L")
	for _, n := range nodes {
		id, _ := n.Properties["id"].AsString()
		fresh, _ := n.Properties["fresh"].AsBool()
		if want := id == "b"; fresh != want {
			t.Errorf("node %s fresh = %v, want %v", id, fresh, want)
		}
	}
}

func TestPatternMapExpr_NullMergeValueIsRefused(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		t.Run(fmt.Sprintf("index=%v", withIndex), func(t *testing.T) {
			gs, ex := mapExprStore(t, withIndex)
			before := len(gs.GetAllNodesAcrossTenants())

			// The null item comes second: an earlier row must not write
			// before a later row is refused.
			items := map[string]any{"items": []any{
				map[string]any{"id": "a", "text": "one"},
				map[string]any{"id": nil, "text": "none"},
			}}
			_, err := runMapExpr(t, ex, batchUpsert, items)
			if err == nil {
				t.Fatal("MERGE with a null property value ran, want a refusal")
			}
			if !strings.Contains(err.Error(), "null property value") {
				t.Errorf("error = %q, want it to name the null property value", err)
			}
			if !isRefusal(err) {
				t.Errorf("error %T is not a refusal, a server would answer 500", err)
			}
			if after := len(gs.GetAllNodesAcrossTenants()); after != before {
				t.Fatalf("nodes before %d, after %d: a refused query wrote", before, after)
			}
		})
	}
}

func TestPatternMapExpr_UndefinedVariableIsRefused(t *testing.T) {
	for _, q := range []string{
		`MERGE (n:L {id: nope.id})`,
		`CREATE (n:L {id: nope.id})`,
		`MATCH (n:L {id: nope.id}) RETURN n`,
		`UNWIND [1] AS x WITH x MATCH (n:L {id: nope}) RETURN n`,
	} {
		t.Run(q, func(t *testing.T) {
			gs, ex := mapExprStore(t, false)
			_, err := runMapExpr(t, ex, q, nil)
			if err == nil {
				t.Fatal("ran, want a refusal of the undefined variable")
			}
			if !strings.Contains(err.Error(), "undefined variable: nope") {
				t.Errorf("error = %q, want it to name undefined variable nope", err)
			}
			if n := len(gs.GetAllNodesAcrossTenants()); n != 0 {
				t.Errorf("%d nodes after a refusal, want 0", n)
			}
		})
	}
}

func TestPatternMapExpr_MissingParameterIsRefused(t *testing.T) {
	for _, q := range []string{
		`MERGE (n:L {id: $missing})`,
		`UNWIND $items AS item WITH item MERGE (n:L {id: item.id + $missing})`,
		`UNWIND $items AS item WITH item CREATE (n:L {id: item.id + $missing})`,
	} {
		t.Run(q, func(t *testing.T) {
			gs, ex := mapExprStore(t, false)
			_, err := runMapExpr(t, ex, q, itemsParam("a", "one"))
			if err == nil {
				t.Fatal("ran, want a refusal of the missing parameter")
			}
			if !strings.Contains(err.Error(), "missing parameter: $missing") {
				t.Errorf("error = %q, want missing parameter: $missing", err)
			}
			if !isRefusal(err) {
				t.Errorf("error %T is not a refusal", err)
			}
			if n := len(gs.GetAllNodesAcrossTenants()); n != 0 {
				t.Errorf("%d nodes after a refusal, want 0", n)
			}
		})
	}
}

func TestPatternMapExpr_ParameterInsideExpressionIsRead(t *testing.T) {
	gs, ex := mapExprStore(t, false)
	params := itemsParam("a", "one")
	params["suffix"] = "-x"
	if _, err := runMapExpr(t, ex, `UNWIND $items AS item WITH item CREATE (n:L {id: item.id + $suffix})`, params); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := textOf(t, gs, "a-x"); got == "<missing>" {
		t.Fatal("no node with id a-x")
	}
}

func TestPatternMapExpr_LiteralsAndParametersUnchanged(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		t.Run(fmt.Sprintf("index=%v", withIndex), func(t *testing.T) {
			gs, ex := mapExprStore(t, withIndex)
			params := map[string]any{"id": "p"}

			for run := 1; run <= 2; run++ {
				if _, err := runMapExpr(t, ex, `MERGE (n:L {id: 'lit'}) SET n.text = 'x'`, nil); err != nil {
					t.Fatalf("literal MERGE run %d: %v", run, err)
				}
				if _, err := runMapExpr(t, ex, `MERGE (n:L {id: $id}) SET n.text = 'y'`, params); err != nil {
					t.Fatalf("parameter MERGE run %d: %v", run, err)
				}
			}
			if n := len(gs.GetAllNodesAcrossTenants()); n != 2 {
				t.Fatalf("%d nodes, want 2", n)
			}
			res, err := runMapExpr(t, ex, `MATCH (n:L {id: $id}) RETURN n.text`, params)
			if err != nil {
				t.Fatal(err)
			}
			wantRows(t, res, "map[n.text:y]")
			res, err = runMapExpr(t, ex, `MATCH (n:L {id: 'lit'}) RETURN n.text`, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantRows(t, res, "map[n.text:x]")
		})
	}
}

// A RowExprRef holds a value that differs for each row, so no access path may
// treat it as a constant. indexReplaceableNode already refuses every inline
// map; this test keeps it so when the map holds a row expression.
func TestPatternMapExpr_IndexPathGivesUp(t *testing.T) {
	q := parsePatternQuery(t, `UNWIND [1] AS x WITH x MATCH (n:L {id: x}) RETURN n`)
	if q.Next == nil || q.Next.Match == nil {
		t.Fatal("the MATCH after WITH is not in the next segment")
	}
	if _, ok := indexReplaceableNode(q.Next.Match); ok {
		t.Fatal("indexReplaceableNode accepted a pattern whose map holds a row expression")
	}
}

// The index path must not hide a WHERE the pattern map shares with it.
func TestPatternMapExpr_IndexAndWhereAgree(t *testing.T) {
	for _, withIndex := range []bool{false, true} {
		t.Run(fmt.Sprintf("index=%v", withIndex), func(t *testing.T) {
			gs, ex := mapExprStore(t, withIndex)
			mustOracleNode(t, gs, "L", map[string]storage.Value{"id": storage.StringValue("a"), "k": storage.StringValue("1")})
			mustOracleNode(t, gs, "L", map[string]storage.Value{"id": storage.StringValue("a"), "k": storage.StringValue("2")})
			res, err := runMapExpr(t, ex, `UNWIND ['a'] AS x WITH x MATCH (n:L {id: x}) WHERE n.id = 'a' AND n.k = '2' RETURN n.k`, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantRows(t, res, "map[n.k:2]")
		})
	}
}

// A negative number is an expression, not a literal token. It was refused
// before this change, and syntax_error_test.go used it as a faulty query.
func TestPatternMapExpr_NegativeNumberLiteral(t *testing.T) {
	gs, ex := mapExprStore(t, false)
	for run := 1; run <= 2; run++ {
		if _, err := runMapExpr(t, ex, `MERGE (n:N {v: -1})`, nil); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	nodes, _ := gs.FindNodesByLabelAcrossTenants("N")
	if len(nodes) != 1 {
		t.Fatalf("%d nodes, want 1", len(nodes))
	}
	if v, err := nodes[0].Properties["v"].AsInt(); err != nil || v != -1 {
		t.Fatalf("v = %v, %v; want -1", v, err)
	}
	res, err := runMapExpr(t, ex, `OPTIONAL MATCH (p:N {v: -1}) RETURN p.v`, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantRows(t, res, "map[p.v:-1]")
}

// A negative number is a constant, so it must stay a plain literal: as a row
// expression it took the per-row MERGE path, whose rows differ from the
// literal path, and -9223372036854775808 became a float (review of the row
// expression change).
func TestPatternMapExpr_NegativeNumberIsAPlainLiteral(t *testing.T) {
	cases := map[string]any{
		`MERGE (n:N {v: -7})`:                   int64(-7),
		`MERGE (n:N {v: -9223372036854775808})`: int64(math.MinInt64),
		`MERGE (n:N {v: -1.5})`:                 -1.5,
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			q := parsePatternQuery(t, text)
			got := q.Merges[0].Pattern.Nodes[0].Properties["v"]
			if _, isExpr := got.(*RowExprRef); isExpr {
				t.Fatalf("%q: v parsed as a row expression, want the literal %v", text, want)
			}
			if got != want {
				t.Fatalf("%q: v = %#v, want %#v", text, got, want)
			}
		})
	}
}
