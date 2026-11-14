package queryutil

import (
	"math"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Direct tests of the parts of the harness. The self-test fixtures prove the
// outcomes end to end. These tests prove each value form and each rule of
// the comparer and of the table reader on their own.

func TestTCKValuesParse(t *testing.T) {
	str := func(s string) tckVal { return tckVal{kind: kString, s: s} }
	cases := []struct {
		text string
		want tckVal
	}{
		{"null", tckVal{kind: kNull}},
		{"true", tckVal{kind: kBool, b: true}},
		{"false", tckVal{kind: kBool}},
		{"0", tckVal{kind: kInt}},
		{"-12", tckVal{kind: kInt, i: -12}},
		{"1.5", tckVal{kind: kFloat, f: 1.5}},
		{"-1.0", tckVal{kind: kFloat, f: -1}},
		{"1.5E3", tckVal{kind: kFloat, f: 1500}},
		{"Infinity", tckVal{kind: kFloat, f: math.Inf(1)}},
		{"-Infinity", tckVal{kind: kFloat, f: math.Inf(-1)}},
		{"'abc'", str("abc")},
		{"'a|b'", str("a|b")},
		{`'a\nb'`, str("a\nb")},
		{`'a\tb'`, str("a\tb")},
		{`'a\'b'`, str("a'b")},
		{`'a\\b'`, str(`a\b`)},
		{"[]", tckVal{kind: kList}},
		{"[1, 'a']", tckVal{kind: kList, list: []tckVal{{kind: kInt, i: 1}, str("a")}}},
		{"{}", tckVal{kind: kMap, m: map[string]tckVal{}}},
		{"{a: 1}", tckVal{kind: kMap, m: map[string]tckVal{"a": {kind: kInt, i: 1}}}},
	}
	for _, c := range cases {
		got := tckParseValue(c.text)
		if tckCompare(c.want, got) != triEq || got.kind != c.want.kind {
			t.Errorf("%s: parsed as %s, want %s", c.text, tckString(got), tckString(c.want))
		}
	}
	if v := tckParseValue("-Infinity"); !math.IsInf(v.f, -1) {
		t.Errorf("-Infinity read as %v", v.f)
	}
	if v := tckParseValue(`'a\nb'`); v.s != "a\nb" {
		t.Errorf(`the \n escape read as %q`, v.s)
	}
	if v := tckParseValue("NaN"); v.kind != kFloat || !math.IsNaN(v.f) {
		t.Errorf("NaN read as %s", tckString(v))
	}
	for _, text := range []string{"(:A)", "[:T]", "[(:A)]", "<(:A)>", "1.", ".5", "'abc", `'a\qb'`, "[1,", "{a 1}", "{a: 1, a: 2}", "1 2", "", "[1] x", "99999999999999999999"} {
		if v := tckParseValue(text); v.kind != kOpaque {
			t.Errorf("%q: read as %s, want opaque", text, tckString(v))
		}
	}
}

func TestTCKValuesCompare(t *testing.T) {
	cases := []struct {
		want, got string
		res       tckTri
	}{
		{"1", "1", triEq},
		{"1", "2", triNe},
		{"1", "1.0", triNe},
		{"1.0", "1.0", triEq},
		{"1.0", "2.0", triNe},
		{"NaN", "NaN", triEq},
		{"NaN", "1.0", triNe},
		{"1.0", "NaN", triNe},
		{"Infinity", "Infinity", triEq},
		{"-Infinity", "Infinity", triNe},
		{"'a'", "'a'", triEq},
		{"'a'", "'b'", triNe},
		{"'1'", "1", triNe},
		{"true", "true", triEq},
		{"true", "false", triNe},
		{"null", "null", triEq},
		{"null", "0", triNe},
		{"[1, 2]", "[1, 2]", triEq},
		{"[1, 2]", "[2, 1]", triNe},
		{"[1]", "[1, 2]", triNe},
		{"{a: 1}", "{a: 1}", triEq},
		{"{a: 1, b: 2}", "{b: 2, a: 1}", triEq},
		{"{a: 1, b: 2}", "{a: 1}", triNe},
		{"{a: 1}", "{a: 1, b: 2}", triNe},
		{"{a: 1}", "{b: 1}", triNe},
		{"{a: 1}", "{a: 2}", triNe},
		{"[1, (:A)]", "[1, 2]", triUnknown},
		{"(:A)", "1", triUnknown},
	}
	for _, c := range cases {
		if got := tckCompare(tckParseValue(c.want), tckParseValue(c.got)); got != c.res {
			t.Errorf("compare %s with %s: %d, want %d", c.want, c.got, got, c.res)
		}
	}
	// A not-equal part wins over an unknown part, in a list and in a map.
	if got := tckCompare(tckParseValue("[1, 2]"), tckGoValue([]any{[]byte{1}, int64(3)})); got != triNe {
		t.Errorf("list with unknown and not-equal parts: %d, want not equal", got)
	}
	// Go values from the executor.
	if tckCompare(tckParseValue("1"), tckGoValue(int64(1))) != triEq || tckCompare(tckParseValue("1"), tckGoValue(1.0)) != triNe {
		t.Error("a Go integer must equal an integer and not a float")
	}
	if tckCompare(tckParseValue("1"), tckGoValue(&storage.Node{})) != triNe {
		t.Error("a node value must not equal an integer")
	}
	if tckCompare(tckParseValue("['a']"), tckGoValue([]string{"a"})) != triEq {
		t.Error("a Go string slice must read as a list")
	}
}

func TestTCKGherkinCells(t *testing.T) {
	cases := []struct {
		row  string
		want []string
	}{
		{"| a | b |", []string{"a", "b"}},
		{`| 'a\|b' | c |`, []string{"'a|b'", "c"}},
		{`| 'a\\nb' |`, []string{`'a\nb'`}},
		{`| x\ny |`, []string{"x\ny"}},
		{"|  |", []string{""}},
	}
	for _, c := range cases {
		got := splitGherkinRow(c.row)
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") || len(got) != len(c.want) {
			t.Errorf("%s: cells %q, want %q", c.row, got, c.want)
		}
	}
}

// A side effect that no count shows must still fail "no side effects". A
// Cypher query cannot move the end of a relationship, so a fixture cannot test
// this. The test builds the "before" state by hand.
func TestTCKSideEffectsFingerprint(t *testing.T) {
	g, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	a, err := g.CreateNode([]string{"A"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.CreateNode([]string{"A"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.CreateEdge(a.ID, b.ID, "T", nil, 1); err != nil {
		t.Fatal(err)
	}
	before, err := tckSnapshot(g)
	if err != nil {
		t.Fatal(err)
	}
	sp := &tckSpec{noSideEffects: true}
	if r := tckCheckSideEffects(g, sp, before, nil); r.outcome != outPass {
		t.Fatalf("same state: %s %s, want pass", r.outcome, r.detail)
	}
	moved := tckState{}
	for k, e := range before {
		if strings.HasPrefix(k, "r") {
			e.to = a.ID
		}
		moved[k] = e
	}
	if n := len(tckEffects(moved, before)); n != 0 {
		t.Fatalf("test set-up is wrong: the counts show %d changes", n)
	}
	if r := tckCheckSideEffects(g, sp, moved, nil); r.outcome != outFail {
		t.Errorf("a moved relationship end with equal counts: %s, want fail", r.outcome)
	}
}
