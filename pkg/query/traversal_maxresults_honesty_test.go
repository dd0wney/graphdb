package query

// A limit that acts and does not tell the caller.
//
// ValidateTraversalOptions had two silent behaviours in its MaxResults
// branch:
//
//	cap      a MaxResults above MaxAllowedResults was silently clamped and
//	         logged with log.Printf, which never reaches the caller.
//	truncate BFS and DFS never reported when they actually stopped at the
//	         (default or capped) MaxResults with more graph left to visit.
//
// This file gates both. The fix has two mechanisms for two different facts:
//
//   - Fact A (knowable before the run): the caller asked for more than the
//     engine allows. ValidateTraversalOptions now returns an error, wrapping
//     ErrInvalidMaxResults, mirroring how the MaxDepth branch above it already
//     wraps ErrInvalidTraversalDepth.
//   - Fact B (knowable only during the run): the traversal actually stopped at
//     the limit. BFS and DFS now return the results AND a non-nil error
//     wrapping ErrTraversalTruncated when the ENGINE's default MaxResults (not
//     a caller-named one) was the reason they stopped short of the graph's
//     edge. A nil error is the positive assertion that the answer is complete.
//
// Only an engine-imposed limit is incompleteness. A caller who names a limit
// and reaches it received exactly what was requested — that path must return
// a nil error, or the signal fires on nearly every bounded call and its reader
// learns to ignore it.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// starGraph builds a root node with `children` direct outgoing :Node
// children, all reachable in one hop. Using a wide, shallow graph (rather
// than a deep chain) keeps DFS recursion depth trivial while still letting
// the node count run past DefaultMaxResults.
func starGraph(t *testing.T, children int) (*storage.GraphStorage, uint64, func()) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)

	root, err := gs.CreateNode([]string{"Root", "Node"}, map[string]storage.Value{
		"name": storage.StringValue("root"),
	})
	if err != nil {
		cleanup()
		t.Fatalf("create root: %v", err)
	}

	for i := 0; i < children; i++ {
		n, err := gs.CreateNode([]string{"Node"}, map[string]storage.Value{
			"name": storage.StringValue(fmt.Sprintf("child%d", i)),
		})
		if err != nil {
			cleanup()
			t.Fatalf("create child %d: %v", i, err)
		}
		if _, err := gs.CreateEdge(root.ID, n.ID, "LINK", nil, 1); err != nil {
			cleanup()
			t.Fatalf("create edge %d: %v", i, err)
		}
	}

	return gs, root.ID, cleanup
}

// --- Fact A: ValidateTraversalOptions must refuse, not clamp -----------------

func TestValidateTraversalOptions_MaxResultsAboveCap_ReturnsError(t *testing.T) {
	opts := &TraversalOptions{MaxResults: MaxAllowedResults + 1}

	err := ValidateTraversalOptions(opts)

	if err == nil {
		t.Fatalf("expected an error for MaxResults %d (max %d), got nil and opts.MaxResults=%d",
			MaxAllowedResults+1, MaxAllowedResults, opts.MaxResults)
	}
	if !errors.Is(err, ErrInvalidMaxResults) {
		t.Errorf("expected error to wrap ErrInvalidMaxResults, got: %v", err)
	}
	// Positive control: prove the value under test is the one that leaked
	// through, not a copy the function silently rewrote before failing.
	if opts.MaxResults != MaxAllowedResults+1 {
		t.Errorf("expected opts.MaxResults to be left untouched at %d on error, got %d (function is silently clamping again)",
			MaxAllowedResults+1, opts.MaxResults)
	}
}

// TestValidateTraversalOptions_MaxResultsWithinCap_NoError is the control for
// the test above: a value at the exact boundary must still be accepted. Without
// it, an implementation that rejects every MaxResults would also pass the
// error-case assertion above for the wrong reason.
func TestValidateTraversalOptions_MaxResultsWithinCap_NoError(t *testing.T) {
	opts := &TraversalOptions{MaxResults: MaxAllowedResults}

	if err := ValidateTraversalOptions(opts); err != nil {
		t.Errorf("expected MaxResults == MaxAllowedResults (%d) to be accepted, got: %v", MaxAllowedResults, err)
	}
	if opts.MaxResults != MaxAllowedResults {
		t.Errorf("expected MaxResults to stay %d, got %d", MaxAllowedResults, opts.MaxResults)
	}
}

// TestValidateTraversalOptions_ZeroStillDefaults proves the owner's requested
// behaviour survives the fix: MaxResults == 0 still means "apply the default,"
// not "reject."
func TestValidateTraversalOptions_ZeroStillDefaults(t *testing.T) {
	opts := &TraversalOptions{MaxResults: 0}

	if err := ValidateTraversalOptions(opts); err != nil {
		t.Fatalf("expected MaxResults == 0 to default silently, got error: %v", err)
	}
	if opts.MaxResults != DefaultMaxResults {
		t.Errorf("expected MaxResults to become %d, got %d", DefaultMaxResults, opts.MaxResults)
	}
}

// --- Fact B: BFS/DFS must report when the ENGINE'S limit truncated them ----

func TestBFS_CompleteTraversal_ReturnsNilError(t *testing.T) {
	gs, rootID, cleanup := starGraph(t, 5)
	defer cleanup()

	traverser := NewTraverser(gs)
	result, err := traverser.BFS(TraversalOptions{
		StartNodeID: rootID,
		Direction:   DirectionOutgoing,
		MaxDepth:    1,
		// MaxResults left at 0: engine default (10000) applies, far above the
		// 6 reachable nodes here, so the traversal finishes on its own.
	})

	if err != nil {
		t.Fatalf("expected nil error for a complete traversal, got: %v", err)
	}
	// Positive control: the graph and the driver are real — a broken helper
	// returning zero nodes would also produce a nil error here for free.
	if len(result.Nodes) != 6 {
		t.Fatalf("expected 6 nodes (root + 5 children), got %d", len(result.Nodes))
	}
}

func TestBFS_CallerNamedLimitReached_ReturnsNilError(t *testing.T) {
	gs, rootID, cleanup := starGraph(t, 10)
	defer cleanup()

	traverser := NewTraverser(gs)
	result, err := traverser.BFS(TraversalOptions{
		StartNodeID: rootID,
		Direction:   DirectionOutgoing,
		MaxDepth:    1,
		MaxResults:  3, // caller-named, well below the 11 reachable nodes
	})

	if err != nil {
		t.Fatalf("caller named MaxResults=3 and got it: expected nil error, got: %v", err)
	}
	// Positive control: prove the cap is real and the graph has more to give,
	// so a passing test isn't just measuring an empty result.
	if len(result.Nodes) != 3 {
		t.Fatalf("expected exactly the caller's limit of 3 nodes, got %d", len(result.Nodes))
	}
}

func TestBFS_EngineDefaultLimitReached_ReturnsTruncatedError(t *testing.T) {
	const childCount = DefaultMaxResults + 5
	gs, rootID, cleanup := starGraph(t, childCount)
	defer cleanup()

	traverser := NewTraverser(gs)
	result, err := traverser.BFS(TraversalOptions{
		StartNodeID: rootID,
		Direction:   DirectionOutgoing,
		MaxDepth:    1,
		// MaxResults left at 0: the ENGINE chooses DefaultMaxResults here, the
		// caller named nothing, so hitting it is genuine incompleteness.
	})

	if err == nil {
		t.Fatalf("expected a truncation error: graph has %d reachable nodes, engine default caps at %d",
			childCount+1, DefaultMaxResults)
	}
	if !errors.Is(err, ErrTraversalTruncated) {
		t.Errorf("expected error to wrap ErrTraversalTruncated, got: %v", err)
	}
	// Positive control: the cap, not a bug, produced this count. If the cap
	// were not applied the count would exceed DefaultMaxResults; if the graph
	// were smaller than the cap the count would fall short of it too.
	if len(result.Nodes) != DefaultMaxResults {
		t.Fatalf("expected exactly the engine default of %d nodes, got %d", DefaultMaxResults, len(result.Nodes))
	}
}

func TestDFS_CompleteTraversal_ReturnsNilError(t *testing.T) {
	gs, rootID, cleanup := starGraph(t, 5)
	defer cleanup()

	traverser := NewTraverser(gs)
	result, err := traverser.DFS(TraversalOptions{
		StartNodeID: rootID,
		Direction:   DirectionOutgoing,
		MaxDepth:    1,
	})

	if err != nil {
		t.Fatalf("expected nil error for a complete traversal, got: %v", err)
	}
	if len(result.Nodes) != 6 {
		t.Fatalf("expected 6 nodes (root + 5 children), got %d", len(result.Nodes))
	}
}

func TestDFS_CallerNamedLimitReached_ReturnsNilError(t *testing.T) {
	gs, rootID, cleanup := starGraph(t, 10)
	defer cleanup()

	traverser := NewTraverser(gs)
	result, err := traverser.DFS(TraversalOptions{
		StartNodeID: rootID,
		Direction:   DirectionOutgoing,
		MaxDepth:    1,
		MaxResults:  3,
	})

	if err != nil {
		t.Fatalf("caller named MaxResults=3 and got it: expected nil error, got: %v", err)
	}
	if len(result.Nodes) != 3 {
		t.Fatalf("expected exactly the caller's limit of 3 nodes, got %d", len(result.Nodes))
	}
}

func TestDFS_EngineDefaultLimitReached_ReturnsTruncatedError(t *testing.T) {
	const childCount = DefaultMaxResults + 5
	gs, rootID, cleanup := starGraph(t, childCount)
	defer cleanup()

	traverser := NewTraverser(gs)
	result, err := traverser.DFS(TraversalOptions{
		StartNodeID: rootID,
		Direction:   DirectionOutgoing,
		MaxDepth:    1,
	})

	if err == nil {
		t.Fatalf("expected a truncation error: graph has %d reachable nodes, engine default caps at %d",
			childCount+1, DefaultMaxResults)
	}
	if !errors.Is(err, ErrTraversalTruncated) {
		t.Errorf("expected error to wrap ErrTraversalTruncated, got: %v", err)
	}
	if len(result.Nodes) != DefaultMaxResults {
		t.Fatalf("expected exactly the engine default of %d nodes, got %d", DefaultMaxResults, len(result.Nodes))
	}
}

// --- GetNeighborhood must not swallow the signal it inherits from BFS ------
//
// GetNeighborhood used to pass MaxResults: DefaultMaxResults explicitly into
// TraversalOptions. That non-zero value made BFS read the bound as
// caller-named, so BFS's engine-truncation signal could never fire for this
// caller, no matter how large the neighbourhood was. GetNeighborhood also
// discarded any non-nil error's results outright (`if err != nil { return
// nil, err }`), so even a signal that did fire would have thrown the nodes
// away. Both had to change: leave MaxResults at 0 so BFS reads the bound as
// the engine's, and give back the nodes together with the error when that
// error wraps ErrTraversalTruncated.

func TestGetNeighborhood_CompleteNeighborhood_ReturnsNilError(t *testing.T) {
	gs, rootID, cleanup := starGraph(t, 5)
	defer cleanup()

	traverser := NewTraverser(gs)
	nodes, err := traverser.GetNeighborhood(rootID, 1, DirectionOutgoing)

	if err != nil {
		t.Fatalf("expected nil error for a complete neighbourhood, got: %v", err)
	}
	// Positive control: the graph and the driver are real, not an empty stub.
	if len(nodes) != 6 {
		t.Fatalf("expected 6 nodes (root + 5 children), got %d", len(nodes))
	}
}

func TestGetNeighborhood_EngineDefaultLimitReached_ReturnsNodesAndTruncatedError(t *testing.T) {
	const childCount = DefaultMaxResults + 5
	gs, rootID, cleanup := starGraph(t, childCount)
	defer cleanup()

	traverser := NewTraverser(gs)
	nodes, err := traverser.GetNeighborhood(rootID, 1, DirectionOutgoing)

	if err == nil {
		t.Fatalf("expected a truncation error: neighbourhood has %d reachable nodes, engine default caps at %d",
			childCount+1, DefaultMaxResults)
	}
	if !errors.Is(err, ErrTraversalTruncated) {
		t.Errorf("expected error to wrap ErrTraversalTruncated, got: %v", err)
	}
	// The nodes must travel WITH the error, not be thrown away: a truncated
	// answer is still an answer.
	if len(nodes) != DefaultMaxResults {
		t.Fatalf("expected the nodes found before truncation (%d), got %d", DefaultMaxResults, len(nodes))
	}
}
