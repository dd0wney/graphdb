package query

import (
	"reflect"
	"strings"
	"testing"
)

// A "$name" parameter must have its value in each segment of a WITH chain.
// The projection of a WITH built the rows of the next segment from the WITH
// items alone, so the "$name" keys of the first segment were lost and a
// parameter after the WITH read as null. A write then ran on a null condition:
// "... WITH a MATCH (p:T) WHERE p.pid = 1 AND $one IS NULL DELETE p" with
// one = 5 deleted the node.
//
// The oracle is the same query with the literal in the place of the parameter.
// The rows, the error and the full store content must be equal, with an index
// on pid and with no index.
var withChainParamCases = []struct {
	query  string
	params map[string]any
	lits   map[string]string
}{
	{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 1 AND $one IS NULL DELETE p", map[string]any{"one": int64(5)}, map[string]string{"one": "5"}},
	{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 1 AND $one IS NULL DETACH DELETE p", map[string]any{"one": int64(5)}, map[string]string{"one": "5"}},
	{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 2 AND p.k < $lim SET p.x = 1", map[string]any{"lim": int64(5)}, map[string]string{"lim": "5"}},
	{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 2 AND p.k > $lim SET p.x = 1", map[string]any{"lim": int64(5)}, map[string]string{"lim": "5"}},
	{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.k < $lim RETURN p.pid", map[string]any{"lim": int64(3)}, map[string]string{"lim": "3"}},
	{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.name = $n SET p.x = 1", map[string]any{"n": "x"}, map[string]string{"n": "'x'"}},
	// A WHERE on the WITH itself, and a chain of two WITH clauses.
	{"MATCH (a:T) WITH a WHERE a.k < $lim MATCH (p:T) WHERE p.pid = 6 SET p.x = 1", map[string]any{"lim": int64(0)}, map[string]string{"lim": "0"}},
	{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (b:T) WHERE b.k = 2 WITH a, b MATCH (p:T) WHERE p.pid = 3 AND p.k > $lim REMOVE p.name", map[string]any{"lim": int64(5)}, map[string]string{"lim": "5"}},
	// The parameter is in the first segment only: this worked before.
	{"MATCH (a:T) WHERE a.k = $one WITH a MATCH (p:T) WHERE p.pid = 2 SET p.x = 1", map[string]any{"one": int64(1)}, map[string]string{"one": "1"}},
}

func TestWithChain_ParameterEqualsLiteralInEachSegment(t *testing.T) {
	for _, c := range withChainParamCases {
		literal := c.query
		for name, text := range c.lits {
			literal = strings.ReplaceAll(literal, "$"+name, text)
		}
		for _, withIndex := range []bool{false, true} {
			name := c.query
			if withIndex {
				name += " [index]"
			}
			t.Run(name, func(t *testing.T) {
				want := runIndexedWrite(t, withIndex, indexedCase{query: literal})
				got := runIndexedWrite(t, withIndex, indexedCase{query: c.query, params: c.params})
				if want.err != "" {
					t.Fatalf("the literal form gives an error, so the case proves nothing: %s", want.err)
				}
				want.usedIdx, got.usedIdx = false, false
				if !reflect.DeepEqual(want, got) {
					t.Errorf("parameter and literal differ\nliteral:   %+v\nparameter: %+v", want, got)
				}
			})
		}
	}
}

// The second branch of a UNION ran with no parameters, so "$v IS NULL" was true
// there and a write ran. The oracle is again the literal form.
var unionParamCases = []struct {
	query  string
	params map[string]any
	lits   map[string]string
}{
	{"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION MATCH (q:T) WHERE q.pid = 2 AND $v IS NULL DETACH DELETE q RETURN q.pid AS r", map[string]any{"v": int64(5)}, map[string]string{"v": "5"}},
	{"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION MATCH (q:T) WHERE q.k < $v RETURN q.pid AS r", map[string]any{"v": int64(4)}, map[string]string{"v": "4"}},
	{"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION ALL MATCH (q:T) WHERE q.k < $v RETURN q.pid AS r UNION ALL MATCH (s:T) WHERE s.k > $v RETURN s.pid AS r", map[string]any{"v": int64(4)}, map[string]string{"v": "4"}},
	{"MATCH (p:T) WHERE p.k < $v RETURN p.pid AS r UNION MATCH (q:T) WHERE q.pid = 6 RETURN q.pid AS r", map[string]any{"v": int64(3)}, map[string]string{"v": "3"}},
	// A pattern map with a parameter after a WITH and in a UNION branch.
	{"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T {pid: $v}) SET p.x = 1", map[string]any{"v": int64(5)}, map[string]string{"v": "5"}},
	{"MATCH (a:T) WHERE a.k = 1 WITH a CREATE (:Made {v: $v})", map[string]any{"v": int64(5)}, map[string]string{"v": "5"}},
	{"MATCH (a:T) WHERE a.k = 1 WITH a MERGE (:M {id: $v})", map[string]any{"v": int64(5)}, map[string]string{"v": "5"}},
	{"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION MATCH (q:T {pid: $v}) RETURN q.pid AS r", map[string]any{"v": int64(5)}, map[string]string{"v": "5"}},
}

func TestUnionAndLaterSegments_ParameterEqualsLiteral(t *testing.T) {
	for _, c := range unionParamCases {
		literal := c.query
		for name, text := range c.lits {
			literal = strings.ReplaceAll(literal, "$"+name, text)
		}
		for _, withIndex := range []bool{false, true} {
			name := c.query
			if withIndex {
				name += " [index]"
			}
			t.Run(name, func(t *testing.T) {
				want := runIndexedWrite(t, withIndex, indexedCase{query: literal})
				got := runIndexedWrite(t, withIndex, indexedCase{query: c.query, params: c.params})
				if want.err != "" {
					t.Fatalf("the literal form gives an error, so the case proves nothing: %s", want.err)
				}
				want.usedIdx, got.usedIdx = false, false
				if !reflect.DeepEqual(want, got) {
					t.Errorf("parameter and literal differ\nliteral:   %+v\nparameter: %+v", want, got)
				}
			})
		}
	}
}

// A parameter that the caller did not supply must be refused wherever it
// stands, before any segment runs. After a WITH or in a UNION branch it read as
// null, and "$two IS NULL DELETE p" deleted the node.
func TestMissingParameter_RefusedInEachSegment(t *testing.T) {
	queries := []string{
		"MATCH (p:T) WHERE p.pid = 1 AND $two IS NULL DELETE p",
		"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 1 AND $two IS NULL DELETE p",
		"MATCH (a:T) WHERE a.k = 1 SET a.x = 1 WITH a MATCH (p:T) WHERE p.pid = 1 AND $two IS NULL DELETE p",
		"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (b:T) WHERE b.k = 2 WITH a, b MATCH (p:T) WHERE p.k > $two RETURN p.pid",
		"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION MATCH (q:T) WHERE q.pid = 2 AND $two IS NULL DETACH DELETE q RETURN q.pid AS r",
		"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T {pid: $two}) DELETE p",
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			before := runIndexedWrite(t, false, indexedCase{query: "MATCH (n:Nope) RETURN n.pid"})
			got := runIndexedWrite(t, false, indexedCase{query: q, params: map[string]any{"one": int64(1)}})
			if !strings.Contains(got.err, "$two") {
				t.Errorf("want a refusal that names $two, got error %q", got.err)
			}
			if !reflect.DeepEqual(before.nodes, got.nodes) || !reflect.DeepEqual(before.edges, got.edges) {
				t.Errorf("a refused query changed the store\nbefore: %v %v\nafter:  %v %v", before.nodes, before.edges, got.nodes, got.edges)
			}
		})
	}
}
