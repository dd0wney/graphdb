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
	isVariableLength := (rel.MinHops != 1 || rel.MaxHops != 1) && (rel.MinHops != 0 || rel.MaxHops != 0)
	if isVariableLength {
		return ms.traverseVariablePath(ctx, currentNode, pattern, relIndex, currentBinding)
	}

	return ms.traverseFixedPath(ctx, currentNode, pattern, relIndex, currentBinding)
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
type bfsEntry struct {
	node    *storage.Node
	depth   int
	edges   []*storage.Edge // path of edges taken to reach this node
	visited map[uint64]bool // per-path visited set (prevents cycles within a single path)
}

// traverseVariablePath uses BFS to find all paths within [MinHops, MaxHops].
// Cycle detection is per-path (a single path can't revisit a node, but different
// paths can reach the same node). Relationship variables are bound as []*storage.Edge.
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

	// BFS queue with per-path visited tracking
	startVisited := map[uint64]bool{currentNode.ID: true}
	queue := []bfsEntry{{node: currentNode, depth: 0, edges: nil, visited: startVisited}}

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
					newBinding.bindings[rel.Variable] = entry.edges
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
			if engineCapped {
				for _, edge := range edges {
					if !entry.visited[ms.targetNodeID(edge, rel, entry.node)] {
						truncated = true
						break
					}
				}
			}
			continue
		}

		for _, edge := range edges {
			neighborID := ms.targetNodeID(edge, rel, entry.node)

			// Per-path cycle detection: skip only if this path already visited this node
			if entry.visited[neighborID] {
				continue
			}

			neighborNode, err := ctx.graph.GetNodeForTenant(neighborID, ctx.tenantID)
			if err != nil {
				continue
			}

			// Clone visited set for this new path branch
			newVisited := make(map[uint64]bool, len(entry.visited)+1)
			for k, v := range entry.visited {
				newVisited[k] = v
			}
			newVisited[neighborID] = true

			newEdges := make([]*storage.Edge, len(entry.edges)+1)
			copy(newEdges, entry.edges)
			newEdges[len(entry.edges)] = edge

			queue = append(queue, bfsEntry{
				node:    neighborNode,
				depth:   entry.depth + 1,
				edges:   newEdges,
				visited: newVisited,
			})
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
