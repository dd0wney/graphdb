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
		// Safe type assertion with check
		nodeInterface, exists := startBinding.bindings[startNodePattern.Variable]
		if !exists {
			continue // Skip if binding doesn't exist
		}
		startNode, ok := nodeInterface.(*storage.Node)
		if !ok {
			continue // Skip if wrong type
		}

		// Traverse each relationship in pattern
		pathResults := ms.traversePath(ctx, startNode, pattern, 0, startBinding)
		results = append(results, pathResults...)
	}

	return results, nil
}

// traversePath recursively traverses relationships in a pattern
func (ms *MatchStep) traversePath(ctx *ExecutionContext, currentNode *storage.Node, pattern *Pattern, relIndex int, currentBinding *BindingSet) []*BindingSet {
	results := make([]*BindingSet, 0)

	// Base case: no more relationships
	if relIndex >= len(pattern.Relationships) {
		return []*BindingSet{currentBinding}
	}

	rel := pattern.Relationships[relIndex]
	targetNodePattern := pattern.Nodes[relIndex+1]

	// Get edges based on direction
	var edges []*storage.Edge
	var err error

	switch rel.Direction {
	case DirectionOutgoing:
		edges, err = ctx.graph.GetOutgoingEdges(currentNode.ID)
	case DirectionIncoming:
		edges, err = ctx.graph.GetIncomingEdges(currentNode.ID)
	case DirectionBoth:
		outgoing, _ := ctx.graph.GetOutgoingEdges(currentNode.ID)
		incoming, _ := ctx.graph.GetIncomingEdges(currentNode.ID)
		edges = make([]*storage.Edge, 0, len(outgoing)+len(incoming))
		edges = append(edges, outgoing...)
		edges = append(edges, incoming...)
	}

	if err != nil {
		return results
	}

	// Filter edges by type
	for _, edge := range edges {
		if rel.Type != "" && edge.Type != rel.Type {
			continue
		}

		// Get target node
		targetNodeID := edge.ToNodeID
		if rel.Direction == DirectionIncoming {
			targetNodeID = edge.FromNodeID
		}

		targetNode, err := ctx.graph.GetNode(targetNodeID)
		if err != nil {
			continue
		}

		// Check if target matches pattern
		if len(targetNodePattern.Labels) > 0 && !ms.hasLabels(targetNode, targetNodePattern.Labels) {
			continue
		}

		if !ms.matchProperties(targetNode.Properties, targetNodePattern.Properties) {
			continue
		}

		// Create new binding with edge and target node
		newBinding := ms.copyBinding(currentBinding)
		if rel.Variable != "" {
			newBinding.bindings[rel.Variable] = edge
		}
		if targetNodePattern.Variable != "" {
			newBinding.bindings[targetNodePattern.Variable] = targetNode
		}

		// Recursively traverse next relationship
		pathResults := ms.traversePath(ctx, targetNode, pattern, relIndex+1, newBinding)
		results = append(results, pathResults...)
	}

	return results
}
