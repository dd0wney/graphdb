package query

import (
	"reflect"
	"testing"
)

// A bare variable parses as a PropertyExpression with an empty Property. The
// column name for it must be the variable alone, and the row key must be the
// same text as the column name.
func TestBareVariableColumnName(t *testing.T) {
	ex := arithmeticStore(t)
	cases := []struct {
		name, text string
		want       []string
	}{
		{"bare variable", `MATCH (n:Person) RETURN n`, []string{"n"}},
		{"count of variable", `MATCH (n:Person) RETURN count(n)`, []string{"COUNT(n)"}},
		{"WITH property alias", `MATCH (p:Person) WITH p.age AS age RETURN age`, []string{"age"}},
		{"WITH bare variable", `MATCH (p:Person) WITH p RETURN p`, []string{"p"}},
		{"WITH variable alias", `MATCH (p:Person) WITH p AS q RETURN q`, []string{"q"}},
		{"DISTINCT variable", `MATCH (n:Person) RETURN DISTINCT n`, []string{"n"}},
		{"grouped", `MATCH (n:Person) RETURN n, count(n)`, []string{"n", "COUNT(n)"}},
		{"property keeps its dot", `MATCH (n:Person) RETURN n.name`, []string{"n.name"}},
		{"property alias", `MATCH (n:Person) RETURN n.name AS x`, []string{"x"}},
		{"count of property keeps its dot", `MATCH (n:Person) RETURN count(n.age)`, []string{"COUNT(n.age)"}},
		{"UNION", `MATCH (n:Person) RETURN n UNION MATCH (n:Person) RETURN n`, []string{"n"}},
		{"CALL YIELD", `CALL algo.detectCycles() YIELD count RETURN count`, []string{"count"}},
		{"ORDER BY variable", `MATCH (n:Person) RETURN n ORDER BY n`, []string{"n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := runCypher(t, ex, tc.text)
			if err != nil {
				t.Fatalf("%q: %v", tc.text, err)
			}
			if !reflect.DeepEqual(rs.Columns, tc.want) {
				t.Fatalf("%q: Columns = %q, want %q", tc.text, rs.Columns, tc.want)
			}
			if len(rs.Rows) == 0 {
				t.Fatalf("%q: no rows, so the row keys cannot be checked", tc.text)
			}
			for i, row := range rs.Rows {
				for _, col := range rs.Columns {
					if _, ok := row[col]; !ok {
						t.Errorf("%q: row %d has no key %q (keys %v)", tc.text, i, col, keysOf(row))
					}
				}
			}
		})
	}
}

func TestBareVariableColumnNameOptionalMatchNull(t *testing.T) {
	ex := arithmeticStore(t)
	rs, err := runCypher(t, ex, `OPTIONAL MATCH (p:Person {id: 99}) RETURN p`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !reflect.DeepEqual(rs.Columns, []string{"p"}) {
		t.Fatalf("Columns = %q, want [p]", rs.Columns)
	}
	if len(rs.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rs.Rows))
	}
	v, ok := rs.Rows[0]["p"]
	if !ok || v != nil {
		t.Errorf("row[p] = %v (present %v), want a present null", v, ok)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
