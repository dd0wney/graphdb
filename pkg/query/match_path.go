package query

import "github.com/dd0wney/cluso-graphdb/pkg/storage"

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

		pathResults := ms.traversePath(ctx, startNode, pattern, 0, startBinding)
		results = append(results, pathResults...)
	}

	return results, nil
}

// traversePath recursively traverses relationships in a pattern.
// Dispatches to traverseVariablePath when the relationship has variable-length hops.
func (ms *MatchStep) traversePath(ctx *ExecutionContext, currentNode *storage.Node, pattern *Pattern, relIndex int, currentBinding *BindingSet) []*BindingSet {
	// Base case: no more relationships
	if relIndex >= len(pattern.Relationships) {
		return []*BindingSet{currentBinding}
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
func (ms *MatchStep) traverseFixedPath(ctx *ExecutionContext, currentNode *storage.Node, pattern *Pattern, relIndex int, currentBinding *BindingSet) []*BindingSet {
	results := make([]*BindingSet, 0)
	rel := pattern.Relationships[relIndex]
	targetNodePattern := pattern.Nodes[relIndex+1]

	edges := ms.getEdges(ctx, currentNode, rel)

	for _, edge := range edges {
		targetNodeID := ms.targetNodeID(edge, rel, currentNode)
		targetNode, err := ctx.graph.GetNode(targetNodeID)
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

		pathResults := ms.traversePath(ctx, targetNode, pattern, relIndex+1, newBinding)
		results = append(results, pathResults...)
	}

	return results
}

// bfsEntry tracks BFS state for variable-length path traversal.
type bfsEntry struct {
	node    *storage.Node
	depth   int
	edges   []*storage.Edge    // path of edges taken to reach this node
	visited map[uint64]bool    // per-path visited set (prevents cycles within a single path)
}

// traverseVariablePath uses BFS to find all paths within [MinHops, MaxHops].
// Cycle detection is per-path (a single path can't revisit a node, but different
// paths can reach the same node). Relationship variables are bound as []*storage.Edge.
func (ms *MatchStep) traverseVariablePath(ctx *ExecutionContext, currentNode *storage.Node, pattern *Pattern, relIndex int, currentBinding *BindingSet) []*BindingSet {
	results := make([]*BindingSet, 0)
	rel := pattern.Relationships[relIndex]
	targetNodePattern := pattern.Nodes[relIndex+1]

	maxHops := rel.MaxHops
	if maxHops == -1 || maxHops > MaxAllowedTraversalDepth {
		maxHops = MaxAllowedTraversalDepth
	}

	// BFS queue with per-path visited tracking
	startVisited := map[uint64]bool{currentNode.ID: true}
	queue := []bfsEntry{{node: currentNode, depth: 0, edges: nil, visited: startVisited}}

	for len(queue) > 0 {
		// Periodic cancellation check to respect query timeouts
		if ctx.IsCancelled() {
			return results
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

				pathResults := ms.traversePath(ctx, entry.node, pattern, relIndex+1, newBinding)
				results = append(results, pathResults...)
			}
		}

		if entry.depth >= maxHops {
			continue
		}

		edges := ms.getEdges(ctx, entry.node, rel)
		for _, edge := range edges {
			neighborID := ms.targetNodeID(edge, rel, entry.node)

			// Per-path cycle detection: skip only if this path already visited this node
			if entry.visited[neighborID] {
				continue
			}

			neighborNode, err := ctx.graph.GetNode(neighborID)
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

	return results
}

// getEdges returns edges from a node filtered by relationship type and direction.
func (ms *MatchStep) getEdges(ctx *ExecutionContext, node *storage.Node, rel *RelationshipPattern) []*storage.Edge {
	var edges []*storage.Edge

	switch rel.Direction {
	case DirectionOutgoing:
		edges, _ = ctx.graph.GetOutgoingEdges(node.ID)
	case DirectionIncoming:
		edges, _ = ctx.graph.GetIncomingEdges(node.ID)
	case DirectionBoth:
		outgoing, _ := ctx.graph.GetOutgoingEdges(node.ID)
		incoming, _ := ctx.graph.GetIncomingEdges(node.ID)
		edges = make([]*storage.Edge, 0, len(outgoing)+len(incoming))
		edges = append(edges, outgoing...)
		edges = append(edges, incoming...)
	}

	// Filter by edge type
	if rel.Type == "" {
		return edges
	}
	filtered := make([]*storage.Edge, 0, len(edges))
	for _, edge := range edges {
		if edge.Type == rel.Type {
			filtered = append(filtered, edge)
		}
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
