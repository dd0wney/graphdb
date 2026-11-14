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
