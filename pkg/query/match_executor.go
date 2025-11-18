package query

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// MatchStep executes a MATCH clause
type MatchStep struct {
	match *MatchClause
}

func (ms *MatchStep) Execute(ctx *ExecutionContext) error {
	newResults := make([]*BindingSet, 0)

	// For each existing binding
	for _, binding := range ctx.results {
		// For each pattern
		for _, pattern := range ms.match.Patterns {
			// Find matches for this pattern
			matches, err := ms.matchPattern(ctx, pattern, binding)
			if err != nil {
				return err
			}
			newResults = append(newResults, matches...)
		}
	}

	// Always update results, even if empty
	// If no matches found, results should be empty
	ctx.results = newResults

	return nil
}

func (ms *MatchStep) matchPattern(ctx *ExecutionContext, pattern *Pattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)

	// Simple case: single node
	if len(pattern.Nodes) == 1 && len(pattern.Relationships) == 0 {
		return ms.matchNode(ctx, pattern.Nodes[0], existingBinding)
	}

	// Pattern with relationships
	if len(pattern.Nodes) >= 2 && len(pattern.Relationships) >= 1 {
		return ms.matchPath(ctx, pattern, existingBinding)
	}

	// Multiple independent nodes (cartesian product)
	if len(pattern.Nodes) >= 2 && len(pattern.Relationships) == 0 {
		return ms.matchCartesianProduct(ctx, pattern, existingBinding)
	}

	return results, nil
}

func (ms *MatchStep) matchNode(ctx *ExecutionContext, nodePattern *NodePattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)

	// Get all nodes with matching labels
	stats := ctx.graph.GetStatistics()
	nodeCount := stats.NodeCount

	for nodeID := uint64(1); nodeID <= nodeCount; nodeID++ {
		node, err := ctx.graph.GetNode(nodeID)
		if err != nil {
			continue
		}

		// Check labels
		if len(nodePattern.Labels) > 0 {
			if !ms.hasLabels(node, nodePattern.Labels) {
				continue
			}
		}

		// Check properties
		if !ms.matchProperties(node.Properties, nodePattern.Properties) {
			continue
		}

		// Create new binding
		newBinding := ms.copyBinding(existingBinding)
		if nodePattern.Variable != "" {
			newBinding.bindings[nodePattern.Variable] = node
		}
		results = append(results, newBinding)
	}

	return results, nil
}

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

// matchCartesianProduct handles matching multiple independent nodes (no relationships)
// Returns the cartesian product of all matching nodes
func (ms *MatchStep) matchCartesianProduct(ctx *ExecutionContext, pattern *Pattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	// Start with the existing binding, or create an initial empty one
	results := []*BindingSet{existingBinding}
	if existingBinding == nil {
		results = []*BindingSet{{bindings: make(map[string]interface{})}}
	}

	// For each node pattern, match nodes and compute cartesian product
	for _, nodePattern := range pattern.Nodes {
		newResults := make([]*BindingSet, 0)

		// Create an empty binding for matchNode
		emptyBinding := &BindingSet{bindings: make(map[string]interface{})}

		// Match nodes for this pattern
		nodeMatches, err := ms.matchNode(ctx, nodePattern, emptyBinding)
		if err != nil {
			return nil, err
		}

		// For each existing binding, combine with each matching node
		for _, existingResult := range results {
			for _, nodeMatch := range nodeMatches {
				// Create new binding combining existing and new
				newBinding := ms.copyBinding(existingResult)

				// Add the node binding from nodeMatch
				if nodePattern.Variable != "" {
					newBinding.bindings[nodePattern.Variable] = nodeMatch.bindings[nodePattern.Variable]
				}

				newResults = append(newResults, newBinding)
			}
		}

		results = newResults
	}

	return results, nil
}

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

func (ms *MatchStep) hasLabels(node *storage.Node, labels []string) bool {
	for _, requiredLabel := range labels {
		found := false
		for _, nodeLabel := range node.Labels {
			if nodeLabel == requiredLabel {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (ms *MatchStep) matchProperties(nodeProps map[string]storage.Value, patternProps map[string]interface{}) bool {
	for key, patternValue := range patternProps {
		nodeValue, exists := nodeProps[key]
		if !exists {
			return false
		}

		// Simple value comparison
		if !ms.valuesEqual(nodeValue, patternValue) {
			return false
		}
	}
	return true
}

func (ms *MatchStep) valuesEqual(nodeValue storage.Value, patternValue interface{}) bool {
	switch v := patternValue.(type) {
	case string:
		nodeStr, err := nodeValue.AsString()
		if err != nil {
			return false // Type mismatch
		}
		return nodeStr == v
	case int64:
		nodeInt, err := nodeValue.AsInt()
		if err != nil {
			return false // Type mismatch
		}
		return nodeInt == v
	case float64:
		nodeFloat, err := nodeValue.AsFloat()
		if err != nil {
			return false // Type mismatch
		}
		return nodeFloat == v
	case bool:
		nodeBool, err := nodeValue.AsBool()
		if err != nil {
			return false // Type mismatch
		}
		return nodeBool == v
	}
	return false
}

func (ms *MatchStep) copyBinding(binding *BindingSet) *BindingSet {
	newBindings := make(map[string]interface{})
	for k, v := range binding.bindings {
		newBindings[k] = v
	}
	return &BindingSet{bindings: newBindings}
}
