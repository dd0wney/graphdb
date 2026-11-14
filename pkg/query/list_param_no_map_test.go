package query

import (
	"reflect"
	"testing"
)

// A query that names a parameter can run with no parameter map (Execute, or an
// HTTP request with no "parameters"). v1.12.0 does not refuse it. A missing
// item of a list literal must then be one null item, and the other items of the
// list must stay. When the whole list read as null, "NOT p.k IN [1, $v]" became
// true for a node whose k is 1, and the DELETE ran.
//
// The oracle is the same query with null in the place of the parameter.
func TestListLiteral_MissingItemWithNoParameterMapIsOneNull(t *testing.T) {
	cases := []struct{ query, withNull string }{
		{"MATCH (p:T) WHERE p.pid = 1 AND NOT p.k IN [1, $v] DELETE p", "MATCH (p:T) WHERE p.pid = 1 AND NOT p.k IN [1, null] DELETE p"},
		{"MATCH (p:T) WHERE p.pid = 1 AND p.k IN [1, $v] SET p.x = 1", "MATCH (p:T) WHERE p.pid = 1 AND p.k IN [1, null] SET p.x = 1"},
		{"MATCH (p:T) WHERE p.k IN [$v, 2, 3] RETURN p.pid", "MATCH (p:T) WHERE p.k IN [null, 2, 3] RETURN p.pid"},
		{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 2 AND NOT p.k IN [2, $v] DETACH DELETE p", "MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 2 AND NOT p.k IN [2, null] DETACH DELETE p"},
	}
	for _, c := range cases {
		for _, withIndex := range []bool{false, true} {
			name := c.query
			if withIndex {
				name += " [index]"
			}
			t.Run(name, func(t *testing.T) {
				want := runIndexedWrite(t, withIndex, indexedCase{query: c.withNull})
				got := runIndexedWrite(t, withIndex, indexedCase{query: c.query})
				if want.err != "" {
					t.Fatalf("the null form gives an error, so the case proves nothing: %s", want.err)
				}
				want.usedIdx, got.usedIdx = false, false
				if !reflect.DeepEqual(want, got) {
					t.Errorf("missing parameter and null differ\nnull:      %+v\nparameter: %+v", want, got)
				}
			})
		}
	}
}
