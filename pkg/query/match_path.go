package query

import (
	"fmt"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// matchPath matches a pattern with relationships (path traversal)
func (ms *MatchStep) matchPath(ctx *ExecutionContext, pattern *Pattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)

	// Get starting nodes
	startNodePattern := pattern.Nodes[0]
	startNodes, err := ms.matchNode(ctx, startNodePattern, existingBinding)
	if err != nil {
		return nil, err
	}

	// For each starting node, traverse relationships
	for _, startBinding := range startNodes {
		nodeInterface, exists := startBinding.bindings[startNodePattern.Variable]
		if !exists {
			continue
		}
		startNode, ok := nodeInterface.(*storage.Node)
		if !ok {
			continue
		}

		pathResults, err := ms.traversePath(ctx, startNode, pattern, 0, startBinding)
		results = append(results, pathResults...)
		if err != nil {
			// Results travel WITH the error, never instead of it: a truncated
			// answer is still an answer, and withholding it would make the cap
			// useless rather than honest. ADR 0003's enumeration rule.
			return results, err
		}
	}

	return results, nil
}

// traversePath recursively traverses relationships in a pattern.
// Dispatches to traverseVariablePath when the relationship has variable-length hops.
func (ms *MatchStep) traversePath(ctx *ExecutionContext, currentNode *storage.Node, pattern *Pattern, relIndex int, currentBinding *BindingSet) ([]*BindingSet, error) {
	// Base case: no more relationships
	if relIndex >= len(pattern.Relationships) {
		return []*BindingSet{currentBinding}, nil
	}

	rel := pattern.Relationships[relIndex]

	// Variable-length path: dispatch to BFS traversal.
	// MinHops=1,MaxHops=1 is the default single-hop. MinHops=0,MaxHops=0 is the
	// Go zero value (unset) — also treat as single-hop for backward compatibility.
	if isVariableLengthRel(rel) {
		return ms.traverseVariablePath(ctx, currentNode, pattern, relIndex, currentBinding)
	}

	return ms.traverseFixedPath(ctx, currentNode, pattern, relIndex, currentBinding)
}

// isVariableLengthRel reports whether rel dispatches to traverseVariablePath.
//
// traversePath and the PathOptions validator must agree on this. If they came
// apart, a refusal would fire for a pattern the traversal never sees, or a
// pattern the traversal does see would escape validation. Note that MinHops >= 2
// implies true, which is what lets the validator test MinHops alone.
func isVariableLengthRel(rel *RelationshipPattern) bool {
	return (rel.MinHops != 1 || rel.MaxHops != 1) && (rel.MinHops != 0 || rel.MaxHops != 0)
}

// traverseFixedPath handles single-hop relationship traversal (the original logic).
func (ms *MatchStep) traverseFixedPath(ctx *ExecutionContext, currentNode *storage.Node, pattern *Pattern, relIndex int, currentBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)
	rel := pattern.Relationships[relIndex]
	targetNodePattern := pattern.Nodes[relIndex+1]

	edges := ms.getEdges(ctx, currentNode, rel)

	for _, edge := range edges {
		targetNodeID := ms.targetNodeID(edge, rel, currentNode)
		targetNode, err := ctx.graph.GetNodeForTenant(targetNodeID, ctx.tenantID)
		if err != nil {
			continue
		}

		if !ms.nodeMatchesPattern(targetNode, targetNodePattern) {
			continue
		}

		newBinding := ms.copyBinding(currentBinding)
		if rel.Variable != "" {
			newBinding.bindings[rel.Variable] = edge
		}
		if targetNodePattern.Variable != "" {
			newBinding.bindings[targetNodePattern.Variable] = targetNode
		}

		pathResults, err := ms.traversePath(ctx, targetNode, pattern, relIndex+1, newBinding)
		results = append(results, pathResults...)
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// bfsEntry tracks BFS state for variable-length path traversal.
//
// edges and visited are used by AllSimplePaths only. Under DistinctNodes the
// frontier owns one visited set for the whole call and one discovery record per
// node, so both fields stay nil and the queue costs O(1) per entry instead of
// O(depth).
type bfsEntry struct {
	node    *storage.Node
	depth   int
	edges   []*storage.Edge // path of edges taken to reach this node
	visited map[uint64]bool // per-path visited set (prevents cycles within a single path)
}

// discoveryRecord records how a node was first reached. DistinctNodes rebuilds
// an emitted row's edge path from the parent chain, which costs O(depth) for the
// nodes that actually produce a row rather than for every path prefix.
type discoveryRecord struct {
	edge   *storage.Edge
	parent uint64
}

// frontier owns a traversal's cycle detection and its record of how each node
// was reached.
//
// It exists so that traverseVariablePath keeps ONE loop, one emit gate and one
// pair of truncation checks. The two PathSemantics differ in exactly three
// answers — what counts as already visited, what a new queue entry costs, and
// where a row's edge path comes from — and all three live here.
//
// AllSimplePaths clones a visited set and an edge list per queue entry, so two
// routes to a node are two entries, each O(depth) to build. DistinctNodes keeps
// one visited set plus one discovery record per node, so a node reached by
// twenty routes is one entry.
type frontier struct {
	distinct  bool
	start     uint64
	seen      map[uint64]bool            // DistinctNodes only
	discovery map[uint64]discoveryRecord // DistinctNodes only
}

func newFrontier(semantics PathSemantics, start uint64) *frontier {
	f := &frontier{distinct: semantics == DistinctNodes, start: start}
	if f.distinct {
		f.seen = map[uint64]bool{start: true}
		f.discovery = make(map[uint64]discoveryRecord)
	}
	return f
}

// root returns the queue entry for the start node.
func (f *frontier) root(node *storage.Node) bfsEntry {
	if f.distinct {
		return bfsEntry{node: node}
	}
	return bfsEntry{node: node, visited: map[uint64]bool{node.ID: true}}
}

// visited reports whether the traversal must not enter id from entry.
func (f *frontier) visited(entry bfsEntry, id uint64) bool {
	if f.distinct {
		return f.seen[id]
	}
	return entry.visited[id]
}

// admit records that the traversal is entering node over edge and returns the
// queue entry for it.
//
// Call it only once every check has passed: under DistinctNodes it mutates
// state shared by the whole traversal, so admitting a node the caller then
// discards would exclude it from every other route as well.
func (f *frontier) admit(entry bfsEntry, node *storage.Node, edge *storage.Edge) bfsEntry {
	if f.distinct {
		f.seen[node.ID] = true
		f.discovery[node.ID] = discoveryRecord{edge: edge, parent: entry.node.ID}
		return bfsEntry{node: node, depth: entry.depth + 1}
	}

	visited := make(map[uint64]bool, len(entry.visited)+1)
	for k, v := range entry.visited {
		visited[k] = v
	}
	visited[node.ID] = true

	edges := make([]*storage.Edge, len(entry.edges)+1)
	copy(edges, entry.edges)
	edges[len(entry.edges)] = edge

	return bfsEntry{node: node, depth: entry.depth + 1, edges: edges, visited: visited}
}

// pathTo returns the edge path to bind to the relationship variable for entry.
//
// It returns an error rather than a short path when the parent chain is broken.
// The case is unreachable today, because every admitted node gets a record and a
// record is written once. If a later change makes it reachable, a caller must
// not receive a wrong path that looks like a right one.
func (f *frontier) pathTo(entry bfsEntry) ([]*storage.Edge, error) {
	if !f.distinct {
		return entry.edges, nil
	}

	// The parent chain is discovered target-first and a row wants it
	// start-first, so collect and reverse. Every admitted node has a record,
	// and a record is written once, so the chain cannot change after the node
	// was queued.
	path := make([]*storage.Edge, 0, entry.depth)
	for id := entry.node.ID; id != f.start; {
		rec, ok := f.discovery[id]
		if !ok {
			return nil, fmt.Errorf("traversal invariant broken: node %d is on the queue "+
				"with no discovery record, so the path to it cannot be rebuilt", id)
		}
		path = append(path, rec.edge)
		id = rec.parent
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}

// admissible reports whether the traversal may enter the node at the far end of
// edge, and returns the loaded node when it may.
//
// The expansion loop and the depth-cap truncation check both use it, so
// "would this candidate have been expanded?" has ONE answer. When they had two,
// the cap check saw a candidate the caller's own filter rejects and reported an
// incomplete answer that had lost nothing.
func (ms *MatchStep) admissible(ctx *ExecutionContext, f *frontier, entry bfsEntry, rel *RelationshipPattern, edge *storage.Edge) (*storage.Node, bool) {
	neighborID := ms.targetNodeID(edge, rel, entry.node)

	// Cycle detection. Per-path under AllSimplePaths, shared under DistinctNodes.
	if f.visited(entry, neighborID) {
		return nil, false
	}

	// GetNodeForTenant, never the tenant-blind reader: a foreign node must be
	// dropped here, before any caller code can observe that it exists.
	node, err := ctx.graph.GetNodeForTenant(neighborID, ctx.tenantID)
	if err != nil {
		return nil, false
	}

	// A rejection is deliberately not remembered — the filter may read Depth and
	// Edge, so "rejected here" does not mean "rejected everywhere".
	if ctx.pathOpts.Expand != nil && !ctx.pathOpts.Expand(Expansion{
		From:  entry.node,
		Edge:  edge,
		To:    node,
		Depth: entry.depth + 1,
	}) {
		return nil, false
	}

	return node, true
}

// traverseVariablePath uses BFS to find paths within [MinHops, MaxHops].
//
// Under the default AllSimplePaths semantics, cycle detection is per-path: a
// single path cannot revisit a node, but different paths can reach the same
// node, so the function returns every distinct simple path. Under DistinctNodes
// each node is admitted once per START NODE, so a node reachable by twenty
// routes produces one binding. See PathOptions.
//
// Relationship variables are bound as []*storage.Edge.
func (ms *MatchStep) traverseVariablePath(ctx *ExecutionContext, currentNode *storage.Node, pattern *Pattern, relIndex int, currentBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)
	rel := pattern.Relationships[relIndex]
	targetNodePattern := pattern.Nodes[relIndex+1]

	// An explicit request above the cap is a caller asking for something the
	// engine will not do. Refuse it, exactly as ValidateTraversalOptions
	// refuses the same request on the BFS/DFS surface — the rule already
	// existed here and was applied on one path only.
	if rel.MaxHops > MaxAllowedTraversalDepth {
		return nil, fmt.Errorf("%w: got %d (max %d)", ErrInvalidTraversalDepth, rel.MaxHops, MaxAllowedTraversalDepth)
	}

	// An unbounded request is different: the caller asked for "all", and both
	// available answers are bad. Erroring makes the pattern unusable on any
	// graph; the silent clamp this replaces answered a question nobody asked.
	// So run to the cap and report having reached it.
	// Only an ENGINE-imposed cap is incompleteness. A caller who asked for
	// *1..2 and got two hops received exactly what they requested, and
	// reporting that as truncated would make the signal meaningless — it would
	// fire on nearly every bounded pattern and readers would learn to ignore
	// it. The distinction is whether the caller named the bound.
	engineCapped := rel.MaxHops == -1
	maxHops := rel.MaxHops
	if engineCapped {
		maxHops = MaxAllowedTraversalDepth
	}
	truncated := false

	opts := ctx.pathOpts

	// DistinctNodes admits a node at its minimum depth and never re-admits it,
	// so a window starting above one hop would drop rows that AllSimplePaths
	// finds — a node whose minimum depth is 1 would never reappear at depth 2
	// even when a genuine path of length 2 exists. Refuse the combination
	// rather than answer a different question in silence.
	if opts.Semantics == DistinctNodes && rel.MinHops >= 2 {
		return nil, fmt.Errorf("%w: got MinHops %d", ErrDistinctNodesMinHops, rel.MinHops)
	}

	f := newFrontier(opts.Semantics, currentNode.ID)
	queue := []bfsEntry{f.root(currentNode)}

	for len(queue) > 0 {
		// Periodic cancellation check to respect query timeouts.
		//
		// The error travels BESIDE the partial results, never instead of
		// them (traversal_types.go:26-34): a cancelled context stopped the
		// traversal before it ran out of graph, so a nil error here would
		// wrongly assert the answer is complete.
		if err := ctx.CheckCancellation(); err != nil {
			return results, err
		}

		entry := queue[0]
		queue[0] = bfsEntry{} // release references for GC
		queue = queue[1:]

		// Collect results at depths within [MinHops, MaxHops]
		if entry.depth >= rel.MinHops && entry.depth <= maxHops {
			if ms.nodeMatchesPattern(entry.node, targetNodePattern) {
				newBinding := ms.copyBinding(currentBinding)
				if rel.Variable != "" {
					path, err := f.pathTo(entry)
					if err != nil {
						return results, err
					}
					newBinding.bindings[rel.Variable] = path
				}
				if targetNodePattern.Variable != "" {
					newBinding.bindings[targetNodePattern.Variable] = entry.node
				}

				pathResults, err := ms.traversePath(ctx, entry.node, pattern, relIndex+1, newBinding)
				results = append(results, pathResults...)
				if err != nil {
					return results, err
				}

				// This path had NO result limit at all, so a caller could not
				// even ask for one. The set grows with the number of ROUTES,
				// not the number of nodes, because the visited set is cloned
				// per branch — so a node reachable twenty ways costs twenty
				// bindings. MaxAllowedResults is the existing constant for
				// exactly this ("the absolute maximum to prevent memory
				// exhaustion"); reaching it is truncation and is reported.
				if len(results) >= MaxAllowedResults {
					ctx.noteTruncation(fmt.Errorf("%w: stopped after %d results (max %d)",
						ErrTraversalTruncated, len(results), MaxAllowedResults))
					return results, nil
				}
			}
		}

		edges := ms.getEdges(ctx, entry.node, rel)

		if entry.depth >= maxHops {
			// At the cap. The answer is incomplete only if something here would
			// actually have been expanded — a frontier node whose neighbours are
			// all already visited on this path costs the caller nothing.
			// The question is whether anything here would ACTUALLY have been
			// expanded. A neighbour already visited costs the caller nothing.
			// Under DistinctNodes "visited" is the shared set, which makes the
			// check strictly more accurate. A neighbour the caller's own filter
			// rejects costs the caller nothing either, so admissible answers
			// both halves and the signal keeps meaning something.
			if engineCapped {
				for _, edge := range edges {
					if _, ok := ms.admissible(ctx, f, entry, rel, edge); ok {
						truncated = true
						break
					}
				}
			}
			continue
		}

		for _, edge := range edges {
			// admissible runs the filter BEFORE admission. After admission the
			// filter would only hide the node: the queue would still expand it
			// and read its neighbours, which is the cost the filter exists to
			// remove.
			neighborNode, ok := ms.admissible(ctx, f, entry, rel, edge)
			if !ok {
				continue
			}

			queue = append(queue, f.admit(entry, neighborNode, edge))
		}
	}

	// A nil error here is the positive assertion that the traversal ran out of
	// graph rather than out of budget. That is the whole point of the change:
	// a caller must be able to tell a complete answer from a capped one.
	if truncated {
		ctx.noteTruncation(fmt.Errorf("%w: reached the depth cap of %d with unexplored neighbours remaining",
			ErrTraversalTruncated, maxHops))
	}
	return results, nil
}

// getEdges returns edges from a node filtered by relationship type,
// direction, and inline pattern properties (for example
// [r:KNOWS {since: 2020}]). A pattern with no type and no properties
// matches every edge, so that case returns the fetched slice unfiltered
// rather than allocating a copy of it.
func (ms *MatchStep) getEdges(ctx *ExecutionContext, node *storage.Node, rel *RelationshipPattern) []*storage.Edge {
	var edges []*storage.Edge

	// Audit A6c-query: tenant-scoped edge fetching for traversal.
	switch rel.Direction {
	case DirectionOutgoing:
		edges, _ = ctx.graph.GetOutgoingEdgesForTenant(node.ID, ctx.tenantID)
	case DirectionIncoming:
		edges, _ = ctx.graph.GetIncomingEdgesForTenant(node.ID, ctx.tenantID)
	case DirectionBoth:
		outgoing, _ := ctx.graph.GetOutgoingEdgesForTenant(node.ID, ctx.tenantID)
		incoming, _ := ctx.graph.GetIncomingEdgesForTenant(node.ID, ctx.tenantID)
		edges = make([]*storage.Edge, 0, len(outgoing)+len(incoming))
		edges = append(edges, outgoing...)
		edges = append(edges, incoming...)
	}

	if rel.Type == "" && len(rel.Properties) == 0 {
		return edges
	}

	filtered := make([]*storage.Edge, 0, len(edges))
	for _, edge := range edges {
		if rel.Type != "" && edge.Type != rel.Type {
			continue
		}
		if !ms.matchProperties(edge.Properties, rel.Properties) {
			continue
		}
		filtered = append(filtered, edge)
	}
	return filtered
}

// targetNodeID returns the ID of the node at the other end of an edge,
// accounting for direction and bidirectional edges.
func (ms *MatchStep) targetNodeID(edge *storage.Edge, rel *RelationshipPattern, fromNode *storage.Node) uint64 {
	switch rel.Direction {
	case DirectionIncoming:
		return edge.FromNodeID
	case DirectionBoth:
		if edge.FromNodeID == fromNode.ID {
			return edge.ToNodeID
		}
		return edge.FromNodeID
	default: // DirectionOutgoing
		return edge.ToNodeID
	}
}

// nodeMatchesPattern checks if a node matches label and property constraints.
func (ms *MatchStep) nodeMatchesPattern(node *storage.Node, pattern *NodePattern) bool {
	if len(pattern.Labels) > 0 && !ms.hasLabels(node, pattern.Labels) {
		return false
	}
	return ms.matchProperties(node.Properties, pattern.Properties)
}
