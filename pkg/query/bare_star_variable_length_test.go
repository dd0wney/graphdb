package query

// Cypher defines -[:TYPE*]-> as "one or more hops" (unbounded). graphdb's
// parser instead initialised MinHops and MaxHops to 1 and only overwrote them
// when a TokenNumber followed the star, so a bare star silently meant "exactly
// one hop" instead. `*1..` (open-ended) already produced MaxHops == -1
// correctly; only the bare star was wrong.
//
// Two levels of test, because a parser-level test alone would not have caught
// the consequence for a running query:
//
//   - parser level: a bare star must parse to MinHops == 1, MaxHops == -1,
//     graphdb's own encoding for "unbounded" (see match_path.go).
//   - execution level: a bare-star pattern over a real chain must reach past
//     one hop.
//
// Each level carries a control: `*1..3` must still mean one-to-three
// (proves the bounded forms are untouched), and a plain, star-free single-hop
// pattern must still mean exactly one hop (proves the fix did not make every
// pattern unbounded).

import (
	"testing"
)

// TestParser_BareStarVariableLengthPath is the parser-level red test: a bare
// star must mean "one or more hops", not "exactly one hop".
func TestParser_BareStarVariableLengthPath(t *testing.T) {
	input := "MATCH (a)-[:KNOWS*]->(b)"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatalf("lex %q: %v", input, err)
	}
	query, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", input, err)
	}

	rel := query.Match.Patterns[0].Relationships[0]
	if rel.MinHops != 1 {
		t.Errorf("bare star: expected MinHops 1, got %d", rel.MinHops)
	}
	if rel.MaxHops != -1 {
		t.Errorf("bare star: expected MaxHops -1 (unbounded), got %d", rel.MaxHops)
	}
}

// TestParser_BoundedVariableLengthPathUnaffected is the control for the
// bounded form: `*1..3` must still mean one-to-three after the fix.
func TestParser_BoundedVariableLengthPathUnaffected(t *testing.T) {
	input := "MATCH (a)-[:KNOWS*1..3]->(b)"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatalf("lex %q: %v", input, err)
	}
	query, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", input, err)
	}

	rel := query.Match.Patterns[0].Relationships[0]
	if rel.MinHops != 1 {
		t.Errorf("*1..3: expected MinHops 1, got %d", rel.MinHops)
	}
	if rel.MaxHops != 3 {
		t.Errorf("*1..3: expected MaxHops 3, got %d", rel.MaxHops)
	}
}

// TestParser_SingleHopPatternWithoutStarUnaffected is the control for the
// default form: a relationship pattern with no star at all must still mean
// exactly one hop. This proves the fix did not make every pattern unbounded.
func TestParser_SingleHopPatternWithoutStarUnaffected(t *testing.T) {
	input := "MATCH (a)-[:KNOWS]->(b)"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatalf("lex %q: %v", input, err)
	}
	query, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", input, err)
	}

	rel := query.Match.Patterns[0].Relationships[0]
	if rel.MinHops != 1 {
		t.Errorf("no star: expected MinHops 1, got %d", rel.MinHops)
	}
	if rel.MaxHops != 1 {
		t.Errorf("no star: expected MaxHops 1, got %d", rel.MaxHops)
	}
}

// TestExecutor_BareStarReachesBeyondOneHop is the execution-level red test.
// A parser-only fix could be right about MinHops/MaxHops and still leave the
// traversal walking a single hop if some other layer re-clamped it, so this
// runs a real query over a real chain and checks the rows that come back.
func TestExecutor_BareStarReachesBeyondOneHop(t *testing.T) {
	const depth = 5
	_, e, cleanup := chainGraph(t, depth)
	defer cleanup()

	rs, err := runQuery(t, e, "MATCH (a:Root)-[:LINK*]->(b:Node) RETURN b.name")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rs.Rows) != depth {
		t.Errorf("bare star over a chain of %d: expected %d reachable nodes, got %d",
			depth, depth, len(rs.Rows))
	}
}

// TestExecutor_SingleHopPatternWithoutStarUnaffected is the execution-level
// control: a plain, star-free pattern must still reach exactly one hop.
func TestExecutor_SingleHopPatternWithoutStarUnaffected(t *testing.T) {
	const depth = 5
	_, e, cleanup := chainGraph(t, depth)
	defer cleanup()

	rs, err := runQuery(t, e, "MATCH (a:Root)-[:LINK]->(b:Node) RETURN b.name")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rs.Rows) != 1 {
		t.Errorf("no star over a chain of %d: expected exactly 1 reachable node, got %d",
			depth, len(rs.Rows))
	}
}
