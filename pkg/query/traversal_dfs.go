package query

import (
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// DFS performs depth-first search traversal
func (t *Traverser) DFS(opts TraversalOptions) (*TraversalResult, error) {
	// Validate and normalize options
	if err := ValidateTraversalOptions(&opts); err != nil {
		return nil, fmt.Errorf("invalid traversal options: %w", err)
	}

	visited := make(map[uint64]bool)
	result := &TraversalResult{
		Nodes:      make([]*storage.Node, 0),
		Paths:      make([]Path, 0),
		SkippedIDs: make([]uint64, 0),
		Errors:     make([]TraversalError, 0),
	}

	if err := t.dfsRecursive(opts.StartNodeID, 0, opts, visited, result); err != nil {
		return result, err
	}

	// Log summary if errors occurred
	if len(result.Errors) > 0 {
		log.Printf("WARNING: DFS traversal completed with %d errors (%d nodes skipped)", len(result.Errors), len(result.SkippedIDs))
	}

	return result, nil
}

// dfsRecursive is the recursive DFS implementation
func (t *Traverser) dfsRecursive(
	nodeID uint64,
	depth int,
	opts TraversalOptions,
	visited map[uint64]bool,
	result *TraversalResult,
) error {
	if visited[nodeID] || depth > opts.MaxDepth || len(result.Nodes) >= opts.MaxResults {
		return nil
	}

	visited[nodeID] = true

	// Get node
	node, err := t.storage.GetNode(nodeID)
	if err != nil {
		if opts.FailOnMissing {
			return fmt.Errorf("DFS failed at node %d: %w", nodeID, err)
		}
		// Track skipped node and continue
		result.SkippedIDs = append(result.SkippedIDs, nodeID)
		result.Errors = append(result.Errors, TraversalError{NodeID: nodeID, Err: err})
		log.Printf("WARNING: DFS skipping node %d: %v", nodeID, err)
		return nil
	}

	// Apply predicate filter
	if opts.Predicate != nil && !opts.Predicate(node) {
		return nil
	}

	result.Nodes = append(result.Nodes, node)

	// Get neighbors
	neighbors, err := t.getNeighbors(nodeID, opts.Direction, opts.EdgeTypes, opts.EdgePredicate)
	if err != nil {
		if opts.FailOnMissing {
			return fmt.Errorf("DFS failed getting neighbors for node %d: %w", nodeID, err)
		}
		// Track error but continue traversal
		result.Errors = append(result.Errors, TraversalError{NodeID: nodeID, Err: fmt.Errorf("get neighbors: %w", err)})
		log.Printf("WARNING: DFS skipping neighbors of node %d: %v", nodeID, err)
		return nil
	}

	for _, neighborID := range neighbors {
		if err := t.dfsRecursive(neighborID, depth+1, opts, visited, result); err != nil {
			return err // Propagate error in strict mode
		}
	}

	return nil
}
