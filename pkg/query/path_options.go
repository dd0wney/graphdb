package query

import (
	"errors"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// PathSemantics selects how a variable-length pattern segment enumerates.
//
// The zero value is today's behaviour. That is deliberate and load-bearing:
// PathOptions{} must mean "unchanged" on every execution path, so a caller who
// does not opt in keeps its rows by construction rather than by a conditional
// somewhere in the traversal.
type PathSemantics int

const (
	// AllSimplePaths is the zero value: every distinct simple path within
	// [MinHops, MaxHops] produces a binding. A node reachable by twenty routes
	// produces twenty rows, and the traversal expands it twenty times.
	AllSimplePaths PathSemantics = iota

	// DistinctNodes admits each node at most once, so a node reachable by
	// twenty routes produces one row. Cost grows with the number of nodes and
	// edges in the reachable subgraph rather than with the number of routes
	// through it, which is the difference between O(N+E) and O(b^d).
	//
	// The relationship variable is bound to the BFS discovery path. A
	// breadth-first traversal that admits each node once admits it at its
	// minimum depth, so that path is A shortest path in edge count. It is not
	// THE shortest path: when several paths tie on length the winner is decided
	// by storage adjacency order, which pkg/storage does not specify. Do not
	// depend on which one comes back.
	//
	// "Once" is scoped to one start node, not to one query. matchPath calls the
	// traversal once per start node, so MATCH (a:Root)-[:LINK*]->(b:Sink) with
	// three :Root nodes can return b three times, once per a. Each row is a
	// distinct (a, b) binding, so that is the correct reading — a query-wide
	// set would silently merge rows across different a bindings.
	//
	// MinHops of 2 or more is refused in this mode. See ErrDistinctNodesMinHops.
	DistinctNodes
)

// ErrDistinctNodesMinHops reports that DistinctNodes was combined with a
// MinHops of 2 or more, which the engine refuses rather than answers wrongly.
//
// With one shared visited set a node is admitted at its minimum depth and never
// re-enters the queue. A node whose minimum depth is 1 is therefore admitted at
// depth 1, falls outside a [2, n] window, and never reappears at depth 2 — even
// when a genuine simple path of length 2 exists. The mode would drop rows that
// AllSimplePaths returns, and drop them silently. A loud refusal is the honest
// answer, and it costs nothing for MinHops of 0 or 1: the start node sits at
// depth 0 and every other admitted node sits at depth 1 or deeper, so no row
// can be lost there.
var ErrDistinctNodesMinHops = errors.New("DistinctNodes semantics do not support MinHops above 1")

// Expansion describes one candidate step, offered to an ExpandFilter before the
// traversal enters it.
//
// It is a struct rather than a parameter list so that a later field is a
// compatible change. It carries all four values on purpose: To is the decision
// subject, Edge lets a rule depend on how the node was reached (which the
// pattern cannot express, because getEdges filters on one type only), From lets
// a rule compare the two ends, and Depth lets a rule tighten with distance
// without lowering MaxHops globally.
type Expansion struct {
	From  *storage.Node // the frontier node being expanded
	Edge  *storage.Edge // the edge under consideration
	To    *storage.Node // the candidate node at the far end
	Depth int           // the depth To would occupy (From's depth plus one)
}

// ExpandFilter reports whether the traversal may enter To.
//
// A false return prunes the candidate: To is not bound, is not queued, and is
// never expanded, so To's own neighbours are never read from storage. To itself
// IS read once, because the filter receives the loaded node.
//
// The filter runs once per examined edge, and a rejection is not remembered: the
// filter may read Depth and Edge, so "rejected here" does not mean "rejected
// everywhere", and marking a rejected node visited would be wrong.
//
// It runs on the query goroutine, under the query timeout, holding no storage
// lock. A slow filter is indistinguishable from a slow query. A panic inside it
// is recovered by ExecuteWithOptions and returned as a query error.
//
// This shape diverges from TraversalOptions.Predicate and EdgePredicate
// (traversal_types.go) on purpose. Those are two functions for one decision,
// neither carries depth, and the node predicate cannot see the edge that
// brought it. Copying them here would lock in those limits.
type ExpandFilter func(Expansion) bool

// PathOptions carries per-call traversal policy for variable-length patterns.
//
// The zero value is today's behaviour exactly: AllSimplePaths with no filter.
// The two fields are orthogonal — Expand applies under both semantics, and
// Semantics works with a nil filter.
//
// The options are per call rather than per Executor because pkg/api shares one
// Executor across every request, so a process-wide switch would change every
// tenant's rows. They are not on *Query either: that struct is what the parser
// produces, and a Go closure is not syntax.
type PathOptions struct {
	Semantics PathSemantics
	Expand    ExpandFilter
}
