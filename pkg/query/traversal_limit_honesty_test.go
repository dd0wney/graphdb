package query

// A limit that acts and does not tell the caller.
//
// That is one defect in three forms, and this file gates two of them on the
// variable-length pattern path:
//
//	depth    match_path.go:106 silently clamped an explicit or unbounded
//	         MaxHops to MaxAllowedTraversalDepth
//	results  traverseVariablePath had NO result cap at all, so the caller
//	         could not even ask for one
//
// graphdb already held the rule and applied it on one path only.
// ValidateTraversalOptions (traversal_types.go:40) returns
// ErrInvalidTraversalDepth for a MaxDepth above the cap, and the sibling
// pattern path clamped in silence. Two implementations of one idea that had
// come apart.
//
// The shape of the fix is ADR 0003's, extended from enumeration to traversal:
// return what was found AND a non-nil error, so that a nil error is the
// positive assertion of completeness.
//
// Reported by the interrogate session (oit-cyber/interrogate), which needs to
// answer "which employees reach this path". A group nested deeper than the cap
// vanished from a security answer and the report looked clean.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// chainGraph builds root -[:LINK]-> n1 -> ... -> nDepth, all :Node.
func chainGraph(t *testing.T, depth int) (*storage.GraphStorage, *Executor, func()) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)

	prev, err := gs.CreateNode([]string{"Root", "Node"}, map[string]storage.Value{
		"name": storage.StringValue("n0"),
	})
	if err != nil {
		cleanup()
		t.Fatalf("create root: %v", err)
	}
	for i := 1; i <= depth; i++ {
		n, err := gs.CreateNode([]string{"Node"}, map[string]storage.Value{
			"name": storage.StringValue(fmt.Sprintf("n%d", i)),
		})
		if err != nil {
			cleanup()
			t.Fatalf("create n%d: %v", i, err)
		}
		if _, err := gs.CreateEdge(prev.ID, n.ID, "LINK", nil, 1); err != nil {
			cleanup()
			t.Fatalf("create edge %d: %v", i, err)
		}
		prev = n
	}
	return gs, NewExecutor(gs), cleanup
}

func runQuery(t *testing.T, e *Executor, q string) (*ResultSet, error) {
	t.Helper()
	tokens, err := NewLexer(q).Tokenize()
	if err != nil {
		t.Fatalf("lex %q: %v", q, err)
	}
	parsed, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("parse %q: %v", q, err)
	}
	return e.Execute(parsed)
}

// An explicit depth above the cap must be refused, exactly as
// ValidateTraversalOptions refuses it on the other traversal surface.
func TestVariablePathRefusesAnExplicitDepthAboveTheCap(t *testing.T) {
	_, e, cleanup := chainGraph(t, 3)
	defer cleanup()

	over := MaxAllowedTraversalDepth + 50
	_, err := runQuery(t, e, fmt.Sprintf("MATCH (a:Root)-[:LINK*1..%d]->(b:Node) RETURN b.name", over))
	if err == nil {
		t.Fatalf("a request for %d hops was accepted; the engine silently answered a "+
			"different question (at most %d) and said nothing", over, MaxAllowedTraversalDepth)
	}
	if !errors.Is(err, ErrInvalidTraversalDepth) {
		t.Errorf("the refusal does not wrap ErrInvalidTraversalDepth, so a caller cannot "+
			"tell it from any other failure: %v", err)
	}
}

// An unbounded pattern must run to the cap and then SAY it stopped there.
//
// The chain is deeper than the cap, so the answer is genuinely incomplete.
func TestUnboundedVariablePathReportsThatItStoppedAtTheCap(t *testing.T) {
	_, e, cleanup := chainGraph(t, MaxAllowedTraversalDepth+2)
	defer cleanup()

	rs, err := runQuery(t, e, "MATCH (a:Root)-[:LINK*1..]->(b:Node) RETURN b.name")

	// The results still come back. ADR 0003's shape: what was found, PLUS the
	// error. Withholding them would make the cap useless rather than honest.
	if rs == nil || len(rs.Rows) == 0 {
		t.Fatalf("no rows came back, so this test cannot distinguish a truncation "+
			"signal from a broken query: rs=%v err=%v", rs, err)
	}
	if err == nil {
		t.Errorf("the traversal stopped at the cap of %d on a chain of %d and reported "+
			"success; a caller cannot tell this answer from a complete one",
			MaxAllowedTraversalDepth, MaxAllowedTraversalDepth+2)
	}
	if err != nil && !errors.Is(err, ErrTraversalTruncated) {
		t.Errorf("the error does not wrap ErrTraversalTruncated: %v", err)
	}
}

// A complete traversal must report a nil error. Without this the truncation
// signal could be returned unconditionally and every test above would pass.
func TestCompleteVariablePathReportsNoError(t *testing.T) {
	_, e, cleanup := chainGraph(t, 5)
	defer cleanup()

	rs, err := runQuery(t, e, "MATCH (a:Root)-[:LINK*1..]->(b:Node) RETURN b.name")
	if err != nil {
		t.Errorf("a chain of 5 is well inside the cap of %d, and the traversal reported "+
			"an error; a nil error must be the positive assertion of completeness: %v",
			MaxAllowedTraversalDepth, err)
	}
	if rs == nil || len(rs.Rows) != 5 {
		t.Errorf("expected 5 reachable nodes, got %v", rs)
	}
}

// executeWithProfiling (executor_explain.go:111) ended with "return result,
// nil" and never read execCtx.truncation, unlike executePlanWithContext
// (executor_plan.go:222, :229). A PROFILE query that hit the depth cap
// reported success anyway.
//
// Against the unfixed code the error is nil even though the chain is deeper
// than the cap.
func TestProfileVariablePathReportsThatItStoppedAtTheCap(t *testing.T) {
	_, e, cleanup := chainGraph(t, MaxAllowedTraversalDepth+2)
	defer cleanup()

	rs, err := runQuery(t, e, "PROFILE MATCH (a:Root)-[:LINK*1..]->(b:Node) RETURN b.name")

	if rs == nil || len(rs.Rows) == 0 {
		t.Fatalf("no rows came back, so this test cannot distinguish a truncation "+
			"signal from a broken query: rs=%v err=%v", rs, err)
	}
	if err == nil {
		t.Errorf("PROFILE on a traversal that stopped at the depth cap of %d on a chain "+
			"of %d reported success; a caller cannot tell this answer from a complete one",
			MaxAllowedTraversalDepth, MaxAllowedTraversalDepth+2)
	}
	if err != nil && !errors.Is(err, ErrTraversalTruncated) {
		t.Errorf("the error does not wrap ErrTraversalTruncated: %v", err)
	}
}

// Control: a PROFILE query that does not truncate returns a nil error. It
// must pass before and after the fix.
func TestProfileCompleteVariablePathReportsNoError(t *testing.T) {
	_, e, cleanup := chainGraph(t, 5)
	defer cleanup()

	rs, err := runQuery(t, e, "PROFILE MATCH (a:Root)-[:LINK*1..]->(b:Node) RETURN b.name")
	if err != nil {
		t.Errorf("a chain of 5 is well inside the cap of %d, and PROFILE reported an "+
			"error; a nil error must be the positive assertion of completeness: %v",
			MaxAllowedTraversalDepth, err)
	}
	if rs == nil || len(rs.Rows) != 5 {
		t.Errorf("expected 5 reachable nodes, got %v", rs)
	}
}
