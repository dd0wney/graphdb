package query

import (
	"fmt"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// UNWIND takes any expression, not only a property reference. The first form
// that users reach for is a list parameter ($items) or a list literal, and the
// parser refused both with "UNWIND expression must be a property reference".

func unwindStore(t *testing.T) *Executor {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	return NewExecutor(gs)
}

func runUnwind(t *testing.T, ex *Executor, text string, params map[string]any) (*ResultSet, error) {
	t.Helper()
	return ex.ExecuteWithParams(parsePatternQuery(t, text), params)
}

func wantRows(t *testing.T, res *ResultSet, want ...string) {
	t.Helper()
	got := make([]string, 0, len(res.Rows))
	for _, r := range res.Rows {
		got = append(got, fmt.Sprint(r))
	}
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestUnwind_ListParameter(t *testing.T) {
	ex := unwindStore(t)
	res, err := runUnwind(t, ex, `UNWIND $xs AS x RETURN x`, map[string]any{"xs": []any{int64(1), int64(2)}})
	if err != nil {
		t.Fatalf("UNWIND $xs: %v", err)
	}
	wantRows(t, res, "map[x:1]", "map[x:2]")
}

func TestUnwind_ListLiteral(t *testing.T) {
	ex := unwindStore(t)
	res, err := runUnwind(t, ex, `UNWIND [1, 2, 3] AS x RETURN x`, nil)
	if err != nil {
		t.Fatalf("UNWIND [1, 2, 3]: %v", err)
	}
	wantRows(t, res, "map[x:1]", "map[x:2]", "map[x:3]")
}

func TestUnwind_ListOfMapsParameter(t *testing.T) {
	ex := unwindStore(t)
	items := []any{
		map[string]any{"id": int64(1), "text": "one"},
		map[string]any{"id": int64(2), "text": "two"},
	}
	res, err := runUnwind(t, ex, `UNWIND $items AS item RETURN item.id AS id, item.text AS text`, map[string]any{"items": items})
	if err != nil {
		t.Fatalf("UNWIND $items: %v", err)
	}
	wantRows(t, res, "map[id:1 text:one]", "map[id:2 text:two]")
}

func TestUnwind_EmptyAndNullListGiveNoRows(t *testing.T) {
	for name, val := range map[string]any{"empty": []any{}, "null": nil} {
		t.Run(name, func(t *testing.T) {
			ex := unwindStore(t)
			res, err := runUnwind(t, ex, `UNWIND $xs AS x RETURN x`, map[string]any{"xs": val})
			if err != nil {
				t.Fatalf("UNWIND $xs: %v", err)
			}
			if len(res.Rows) != 0 {
				t.Errorf("rows = %v, want none", res.Rows)
			}
		})
	}
}

// The existing form must keep its result: UNWIND of a stored list property.
func TestUnwind_PropertyReferenceKeepsItsResult(t *testing.T) {
	ex := unwindStore(t)
	gs := ex.graph
	tags, err := storage.JSONValue([]any{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.CreateNode([]string{"T"}, map[string]storage.Value{"tags": tags}); err != nil {
		t.Fatal(err)
	}
	res, err := runUnwind(t, ex, `MATCH (n:T) UNWIND n.tags AS tag RETURN tag`, nil)
	if err != nil {
		t.Fatalf("UNWIND n.tags: %v", err)
	}
	wantRows(t, res, "map[tag:a]", "map[tag:b]")
}

// A parameter that the run does not hold is still refused, as a
// *RefusalError that names it, before any row is made.
func TestUnwind_MissingParameterIsRefused(t *testing.T) {
	ex := unwindStore(t)
	_, err := runUnwind(t, ex, `UNWIND $missing AS x RETURN x`, map[string]any{})
	wantMissingParam(t, err, "missing")
}

// A parameter in an UNWIND expression of a second segment is seen too.
func TestUnwind_ListParameterAfterMatch(t *testing.T) {
	ex := unwindStore(t)
	gs := ex.graph
	for i := 0; i < 2; i++ {
		if _, err := gs.CreateNode([]string{"T"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	res, err := runUnwind(t, ex, `MATCH (n:T) UNWIND $xs AS x RETURN x`, map[string]any{"xs": []any{int64(1), int64(2)}})
	if err != nil {
		t.Fatalf("MATCH ... UNWIND $xs: %v", err)
	}
	if len(res.Rows) != 4 {
		t.Errorf("rows = %v, want 4 (2 nodes x 2 items)", res.Rows)
	}
}

// Probe, not a gate. It records what a write after UNWIND does today, for the
// task v1.4-cypher-unwind-write-per-item.
func TestUnwind_WriteAfterUnwind_Probe(t *testing.T) {
	items := []any{
		map[string]any{"id": int64(1), "text": "one"},
		map[string]any{"id": int64(2), "text": "two"},
	}
	for _, text := range []string{
		`UNWIND $items AS item CREATE (n:L {id: item.id})`,
		`UNWIND $items AS item MERGE (n:L {id: item.id}) SET n.text = item.text`,
		// The same two writes through SET, which takes an expression.
		`UNWIND $items AS item CREATE (n:L) SET n.id = item.id`,
		`UNWIND $items AS item MERGE (n:L) SET n.id = item.id, n.text = item.text`,
	} {
		ex := unwindStore(t)
		// Not parsePatternQuery: a refusal at parse time is a result to log.
		tokens, lexErr := NewLexer(text).Tokenize()
		if lexErr != nil {
			t.Fatalf("Tokenize %q: %v", text, lexErr)
		}
		q, parseErr := NewParser(tokens).Parse()
		if parseErr != nil {
			t.Logf("%s => parse refused: %v", text, parseErr)
			continue
		}
		res, err := ex.ExecuteWithParams(q, map[string]any{"items": items})
		t.Logf("%s => err=%v", text, err)
		if res != nil {
			t.Logf("  result rows=%v", res.Rows)
		}
		nodes, ferr := ex.graph.FindNodesByLabelAcrossTenants("L")
		if ferr != nil {
			t.Fatalf("FindNodesByLabel: %v", ferr)
		}
		t.Logf("  L nodes: %d", len(nodes))
		for _, n := range nodes {
			t.Logf("  node %d props=%v", n.ID, n.Properties)
		}
	}
	t.Skip("probe for v1.4-cypher-unwind-write-per-item")
}
