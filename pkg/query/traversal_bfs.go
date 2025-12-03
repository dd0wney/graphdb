package query

import (
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// BFS performs breadth-first search traversal
func (t *Traverser) BFS(opts TraversalOptions) (*TraversalResult, error) {
	// Validate and normalize options
	if err := ValidateTraversalOptions(&opts); err != nil {
		return nil, fmt.Errorf("invalid traversal options: %w", err)
	}

	visited := make(map[uint64]bool)
	queue := []uint64{opts.StartNodeID}
	depth := make(map[uint64]int)
	depth[opts.StartNodeID] = 0

	result := &TraversalResult{
		Nodes:      make([]*storage.Node, 0),
		Paths:      make([]Path, 0),
		SkippedIDs: make([]uint64, 0),
		Errors:     make([]TraversalError, 0),
	}

	for len(queue) > 0 && len(result.Nodes) < opts.MaxResults {
		nodeID := queue[0]
		queue = queue[1:]

		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true

		// Get node
		node, err := t.storage.GetNode(nodeID)
		if err != nil {
			if opts.FailOnMissing {
				return result, fmt.Errorf("BFS failed at node %d: %w", nodeID, err)
			}
			// Track skipped node and continue
			result.SkippedIDs = append(result.SkippedIDs, nodeID)
			result.Errors = append(result.Errors, TraversalError{NodeID: nodeID, Err: err})
			log.Printf("WARNING: BFS skipping node %d: %v", nodeID, err)
			continue
		}

		// Apply predicate filter
		if opts.Predicate != nil && !opts.Predicate(node) {
			continue
		}

		result.Nodes = append(result.Nodes, node)

		// Stop if max depth reached
		currentDepth := depth[nodeID]
		if currentDepth >= opts.MaxDepth {
			continue
		}

		// Get neighbors
		neighbors, err := t.getNeighbors(nodeID, opts.Direction, opts.EdgeTypes, opts.EdgePredicate)
		if err != nil {
			if opts.FailOnMissing {
				return result, fmt.Errorf("BFS failed getting neighbors for node %d: %w", nodeID, err)
			}
			// Track error but continue traversal
			result.Errors = append(result.Errors, TraversalError{NodeID: nodeID, Err: fmt.Errorf("get neighbors: %w", err)})
			log.Printf("WARNING: BFS skipping neighbors of node %d: %v", nodeID, err)
			continue
		}

		for _, neighborID := range neighbors {
			if !visited[neighborID] {
				queue = append(queue, neighborID)
				depth[neighborID] = currentDepth + 1
			}
		}
	}

	// Log summary if errors occurred
	if len(result.Errors) > 0 {
		log.Printf("WARNING: BFS traversal completed with %d errors (%d nodes skipped)", len(result.Errors), len(result.SkippedIDs))
	}

	return result, nil
}

// GetNeighborhood gets all nodes within N hops.
// If hops is 0, only the start node is returned.
func (t *Traverser) GetNeighborhood(nodeID uint64, hops int, direction Direction) ([]*storage.Node, error) {
	// Validate hops (same as depth; 0 is valid and returns only the start node)
	if hops < MinTraversalDepth {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidTraversalDepth, hops)
	}
	if hops > MaxAllowedTraversalDepth {
		return nil, fmt.Errorf("%w: got %d (max %d)", ErrInvalidTraversalDepth, hops, MaxAllowedTraversalDepth)
	}

	result, err := t.BFS(TraversalOptions{
		StartNodeID: nodeID,
		Direction:   direction,
		EdgeTypes:   []string{},
		MaxDepth:    hops,
		MaxResults:  DefaultMaxResults,
	})

	if err != nil {
		return nil, err
	}

	return result.Nodes, nil
}

// getNeighbors gets neighboring node IDs based on direction
func (t *Traverser) getNeighbors(nodeID uint64, direction Direction, edgeTypes []string, edgePredicate func(*storage.Edge) bool) ([]uint64, error) {
	neighbors := make([]uint64, 0)

	if direction == DirectionOutgoing || direction == DirectionBoth {
		edges, err := t.storage.GetOutgoingEdges(nodeID)
		if err != nil {
			return nil, err
		}

		for _, edge := range edges {
			// Filter by edge type
			if len(edgeTypes) > 0 && !contains(edgeTypes, edge.Type) {
				continue
			}
			// Filter by edge predicate (temporal/property filtering)
			if edgePredicate != nil && !edgePredicate(edge) {
				continue
			}
			neighbors = append(neighbors, edge.ToNodeID)
		}
	}

	if direction == DirectionIncoming || direction == DirectionBoth {
		edges, err := t.storage.GetIncomingEdges(nodeID)
		if err != nil {
			return nil, err
		}

		for _, edge := range edges {
			// Filter by edge type
			if len(edgeTypes) > 0 && !contains(edgeTypes, edge.Type) {
				continue
			}
			// Filter by edge predicate (temporal/property filtering)
			if edgePredicate != nil && !edgePredicate(edge) {
				continue
			}
			neighbors = append(neighbors, edge.FromNodeID)
		}
	}

	return neighbors, nil
}
