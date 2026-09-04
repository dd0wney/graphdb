package query

// A limit that acts and does not tell the caller — this time, the limit is a
// caller-cancelled context, not a size cap.
//
// traverseVariablePath (match_path.go:148-150) checked ctx.IsCancelled()
// inside its BFS loop and, on a cancelled context, returned its
// partial results with a nil error:
//
//	if ctx.IsCancelled() {
//	    return results, nil
//	}
//
// A nil error is the positive assertion of a complete answer
// (traversal_types.go:26-34, ErrTraversalTruncated). Returning it here
// asserted completeness for an answer that was whatever the BFS had reached
// when the context died — the traversal Step itself could not distinguish
// "ran out of graph" from "ran out of time".
//
// Two callers upstream (executePlanWithContext, executeWithProfiling)
// already re-check cancellation before returning to a caller, so this does
// not reach a REST caller as a false success today. It is still a defect:
// the function lies about its own result, and it is saved only by two
// callers that happen to re-check. The fix makes the Step itself honest,
// using the same CheckCancellation the two callers already rely on.

import (
	"context"
	"errors"
	"testing"
)

// cancellationTestPattern builds (a:Root)-[:LINK*1..3]->(b:Node), a
// variable-length pattern, directly as AST nodes rather than through the
// parser — the observable under test is the Step boundary
// (MatchStep.Execute), not query text.
func cancellationTestPattern() *MatchClause {
	return &MatchClause{
		Patterns: []*Pattern{
			{
				Nodes: []*NodePattern{
					{Variable: "a", Labels: []string{"Root"}},
					{Variable: "b", Labels: []string{"Node"}},
				},
				Relationships: []*RelationshipPattern{
					{
						Type:      "LINK",
						Direction: DirectionOutgoing,
						MinHops:   1,
						MaxHops:   3,
					},
				},
			},
		},
	}
}

// A context cancelled before the Step runs must make MatchStep.Execute
// return a non-nil error that satisfies errors.Is(err, context.Canceled).
// Against the unfixed code the traversal returns a nil error instead.
func TestVariablePathReportsCancellationInsteadOfClaimingCompletion(t *testing.T) {
	gs, _, cleanup := chainGraph(t, 3)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	execCtx := newExecutionContext(ctx, gs)
	execCtx.results = []*BindingSet{{bindings: make(map[string]any)}}

	ms := &MatchStep{match: cancellationTestPattern()}
	err := ms.Execute(execCtx)

	if err == nil {
		t.Fatalf("MatchStep.Execute returned a nil error on an already-cancelled " +
			"context; a nil error asserts the traversal is complete, but it stopped " +
			"because the context died, not because it ran out of graph")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error does not wrap context.Canceled, so a caller cannot tell "+
			"a cancellation from any other failure: %v", err)
	}
}

// Control: the identical pattern on a live (uncancelled) context must still
// return a nil error. Without this control, an unconditional error would
// also pass the test above. It must pass before and after the fix.
func TestVariablePathOnLiveContextReportsNoError(t *testing.T) {
	gs, _, cleanup := chainGraph(t, 3)
	defer cleanup()

	execCtx := newExecutionContext(context.Background(), gs)
	execCtx.results = []*BindingSet{{bindings: make(map[string]any)}}

	ms := &MatchStep{match: cancellationTestPattern()}
	err := ms.Execute(execCtx)

	if err != nil {
		t.Errorf("a live (uncancelled) context returned an error: %v", err)
	}
}
