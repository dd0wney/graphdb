package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// matchNode matches a single node pattern against the graph.
// If the variable is already bound in existingBinding, only validates that the
// bound node matches labels/properties (avoids re-scanning all nodes).
func (ms *MatchStep) matchNode(ctx *ExecutionContext, nodePattern *NodePattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)

	// If the variable is already bound, validate the bound node instead of scanning
	if nodePattern.Variable != "" {
		if existing, ok := existingBinding.bindings[nodePattern.Variable]; ok && existing != nil {
			if node, ok := existing.(*storage.Node); ok {
				if len(nodePattern.Labels) > 0 && !ms.hasLabels(node, nodePattern.Labels) {
					return results, nil
				}
				if !ms.matchProperties(node.Properties, nodePattern.Properties) {
					return results, nil
				}
				newBinding := ms.copyBinding(existingBinding)
				return append(results, newBinding), nil
			}
		}
	}

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

// matchCartesianProduct handles matching multiple independent nodes (no relationships)
// Returns the cartesian product of all matching nodes.
// Limited by MaxCartesianProductResults to prevent memory exhaustion.
func (ms *MatchStep) matchCartesianProduct(ctx *ExecutionContext, pattern *Pattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	// Start with the existing binding, or create an initial empty one
	results := []*BindingSet{existingBinding}
	if existingBinding == nil {
		results = []*BindingSet{{bindings: make(map[string]any)}}
	}

	// For each node pattern, match nodes and compute cartesian product
	for _, nodePattern := range pattern.Nodes {
		newResults := make([]*BindingSet, 0)

		// Create an empty binding for matchNode
		emptyBinding := &BindingSet{bindings: make(map[string]any)}

		// Match nodes for this pattern
		nodeMatches, err := ms.matchNode(ctx, nodePattern, emptyBinding)
		if err != nil {
			return nil, err
		}

		// Check if the product would exceed the limit
		expectedSize := len(results) * len(nodeMatches)
		if expectedSize > MaxCartesianProductResults {
			return nil, fmt.Errorf("cartesian product would produce %d results, exceeding limit of %d; consider adding relationship constraints or LIMIT clause",
				expectedSize, MaxCartesianProductResults)
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
