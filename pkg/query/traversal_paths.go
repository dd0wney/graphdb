package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

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
	// Validate depth (0 is valid but will only return paths of length 0, i.e., fromID == toID)
	if maxDepth < MinTraversalDepth {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidTraversalDepth, maxDepth)
	}
	if maxDepth > MaxAllowedTraversalDepth {
		return nil, fmt.Errorf("%w: got %d (max %d)", ErrInvalidTraversalDepth, maxDepth, MaxAllowedTraversalDepth)
	}

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

		// Add edge (except for last node) - defensive: check edge exists
		if i > 0 {
			if edge, exists := parentEdge[nodeIDs[i-1]]; exists {
				path.Edges = append(path.Edges, edge)
			}
		}
	}

	return path, nil
}
