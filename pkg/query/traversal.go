package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TraversalOptions configures graph traversal
type TraversalOptions struct {
	StartNodeID   uint64
	Direction     Direction
	EdgeTypes     []string                 // Filter by edge types (empty = all types)
	MaxDepth      int                      // Maximum traversal depth
	MaxResults    int                      // Maximum nodes to return
	Predicate     func(*storage.Node) bool // Node filter function
	EdgePredicate func(*storage.Edge) bool // Edge filter function (for temporal/property filtering)
}

// TraversalResult contains the results of a traversal
type TraversalResult struct {
	Nodes []*storage.Node
	Paths []Path
}

// Path represents a path through the graph
type Path struct {
	Nodes []*storage.Node
	Edges []*storage.Edge
}

// Traverser performs graph traversals
type Traverser struct {
	storage *storage.GraphStorage
}

// NewTraverser creates a new traverser
func NewTraverser(storage *storage.GraphStorage) *Traverser {
	return &Traverser{storage: storage}
}

// BFS performs breadth-first search traversal
func (t *Traverser) BFS(opts TraversalOptions) (*TraversalResult, error) {
	visited := make(map[uint64]bool)
	queue := []uint64{opts.StartNodeID}
	depth := make(map[uint64]int)
	depth[opts.StartNodeID] = 0

	result := &TraversalResult{
		Nodes: make([]*storage.Node, 0),
		Paths: make([]Path, 0),
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
			continue
		}

		for _, neighborID := range neighbors {
			if !visited[neighborID] {
				queue = append(queue, neighborID)
				depth[neighborID] = currentDepth + 1
			}
		}
	}

	return result, nil
}

// DFS performs depth-first search traversal
func (t *Traverser) DFS(opts TraversalOptions) (*TraversalResult, error) {
	visited := make(map[uint64]bool)
	result := &TraversalResult{
		Nodes: make([]*storage.Node, 0),
		Paths: make([]Path, 0),
	}

	t.dfsRecursive(opts.StartNodeID, 0, opts, visited, result)

	return result, nil
}

// dfsRecursive is the recursive DFS implementation
func (t *Traverser) dfsRecursive(
	nodeID uint64,
	depth int,
	opts TraversalOptions,
	visited map[uint64]bool,
	result *TraversalResult,
) {
	if visited[nodeID] || depth > opts.MaxDepth || len(result.Nodes) >= opts.MaxResults {
		return
	}

	visited[nodeID] = true

	// Get node
	node, err := t.storage.GetNode(nodeID)
	if err != nil {
		return
	}

	// Apply predicate filter
	if opts.Predicate != nil && !opts.Predicate(node) {
		return
	}

	result.Nodes = append(result.Nodes, node)

	// Get neighbors
	neighbors, err := t.getNeighbors(nodeID, opts.Direction, opts.EdgeTypes, opts.EdgePredicate)
	if err != nil {
		return
	}

	for _, neighborID := range neighbors {
		t.dfsRecursive(neighborID, depth+1, opts, visited, result)
	}
}

// FindShortestPath finds the shortest path between two nodes (BFS-based)
func (t *Traverser) FindShortestPath(fromID, toID uint64, edgeTypes []string) (Path, error) {
	return t.FindShortestPathWithPredicate(fromID, toID, edgeTypes, nil)
}

// FindShortestPathWithPredicate finds shortest path with optional edge filtering
func (t *Traverser) FindShortestPathWithPredicate(fromID, toID uint64, edgeTypes []string, edgePredicate func(*storage.Edge) bool) (Path, error) {
	if fromID == toID {
		node, err := t.storage.GetNode(fromID)
		if err != nil {
			return Path{}, err
		}
		return Path{Nodes: []*storage.Node{node}, Edges: []*storage.Edge{}}, nil
	}

	visited := make(map[uint64]bool)
	queue := []uint64{fromID}
	parent := make(map[uint64]uint64)
	parentEdge := make(map[uint64]*storage.Edge)

	visited[fromID] = true

	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]

		if nodeID == toID {
			// Reconstruct path
			return t.reconstructPath(fromID, toID, parent, parentEdge)
		}

		// Get outgoing edges
		edges, err := t.storage.GetOutgoingEdges(nodeID)
		if err != nil {
			continue
		}

		for _, edge := range edges {
			// Filter by edge type if specified
			if len(edgeTypes) > 0 && !contains(edgeTypes, edge.Type) {
				continue
			}
			// Filter by edge predicate
			if edgePredicate != nil && !edgePredicate(edge) {
				continue
			}

			neighborID := edge.ToNodeID
			if !visited[neighborID] {
				visited[neighborID] = true
				parent[neighborID] = nodeID
				parentEdge[neighborID] = edge
				queue = append(queue, neighborID)
			}
		}
	}

	return Path{}, fmt.Errorf("no path found between nodes %d and %d", fromID, toID)
}

// FindAllPaths finds all paths between two nodes up to maxDepth
func (t *Traverser) FindAllPaths(fromID, toID uint64, maxDepth int, edgeTypes []string) ([]Path, error) {
	return t.FindAllPathsWithPredicate(fromID, toID, maxDepth, edgeTypes, nil)
}

// FindAllPathsWithPredicate finds all paths with optional edge filtering
func (t *Traverser) FindAllPathsWithPredicate(fromID, toID uint64, maxDepth int, edgeTypes []string, edgePredicate func(*storage.Edge) bool) ([]Path, error) {
	paths := make([]Path, 0)
	currentPath := Path{
		Nodes: make([]*storage.Node, 0),
		Edges: make([]*storage.Edge, 0),
	}

	visited := make(map[uint64]bool)

	startNode, err := t.storage.GetNode(fromID)
	if err != nil {
		return nil, err
	}

	t.findAllPathsRecursive(fromID, toID, maxDepth, edgeTypes, edgePredicate, currentPath, visited, &paths, startNode)

	return paths, nil
}

// findAllPathsRecursive is the recursive implementation
func (t *Traverser) findAllPathsRecursive(
	currentID, targetID uint64,
	remainingDepth int,
	edgeTypes []string,
	edgePredicate func(*storage.Edge) bool,
	currentPath Path,
	visited map[uint64]bool,
	allPaths *[]Path,
	currentNode *storage.Node,
) {
	// Add current node to path
	currentPath.Nodes = append(currentPath.Nodes, currentNode)
	visited[currentID] = true

	// Check if we reached the target
	if currentID == targetID {
		// Clone the path and add to results
		pathCopy := Path{
			Nodes: make([]*storage.Node, len(currentPath.Nodes)),
			Edges: make([]*storage.Edge, len(currentPath.Edges)),
		}
		copy(pathCopy.Nodes, currentPath.Nodes)
		copy(pathCopy.Edges, currentPath.Edges)
		*allPaths = append(*allPaths, pathCopy)
	} else if remainingDepth > 0 {
		// Continue traversal
		edges, err := t.storage.GetOutgoingEdges(currentID)
		if err == nil {
			for _, edge := range edges {
				// Filter by edge type
				if len(edgeTypes) > 0 && !contains(edgeTypes, edge.Type) {
					continue
				}
				// Filter by edge predicate
				if edgePredicate != nil && !edgePredicate(edge) {
					continue
				}

				neighborID := edge.ToNodeID
				if !visited[neighborID] {
					neighbor, err := t.storage.GetNode(neighborID)
					if err != nil {
						continue
					}

					// Add edge to current path
					newPath := currentPath
					newPath.Edges = append(newPath.Edges, edge)

					t.findAllPathsRecursive(
						neighborID,
						targetID,
						remainingDepth-1,
						edgeTypes,
						edgePredicate,
						newPath,
						visited,
						allPaths,
						neighbor,
					)
				}
			}
		}
	}

	// Backtrack
	visited[currentID] = false
}

// GetNeighborhood gets all nodes within N hops
func (t *Traverser) GetNeighborhood(nodeID uint64, hops int, direction Direction) ([]*storage.Node, error) {
	result, err := t.BFS(TraversalOptions{
		StartNodeID: nodeID,
		Direction:   direction,
		EdgeTypes:   []string{},
		MaxDepth:    hops,
		MaxResults:  10000, // Large limit
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

// reconstructPath reconstructs a path from parent pointers
func (t *Traverser) reconstructPath(
	fromID, toID uint64,
	parent map[uint64]uint64,
	parentEdge map[uint64]*storage.Edge,
) (Path, error) {
	path := Path{
		Nodes: make([]*storage.Node, 0),
		Edges: make([]*storage.Edge, 0),
	}

	// Reconstruct in reverse
	currentID := toID
	nodeIDs := []uint64{currentID}

	for currentID != fromID {
		parentID, exists := parent[currentID]
		if !exists {
			return Path{}, fmt.Errorf("path reconstruction failed")
		}
		nodeIDs = append(nodeIDs, parentID)
		currentID = parentID
	}

	// Reverse to get correct order
	for i := len(nodeIDs) - 1; i >= 0; i-- {
		node, err := t.storage.GetNode(nodeIDs[i])
		if err != nil {
			return Path{}, err
		}
		path.Nodes = append(path.Nodes, node)

		// Add edge (except for last node)
		if i > 0 {
			edge := parentEdge[nodeIDs[i-1]]
			path.Edges = append(path.Edges, edge)
		}
	}

	return path, nil
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
