package algorithms

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// IsDAG checks if the graph is a Directed Acyclic Graph
// Returns true if the graph contains no cycles
func IsDAG(graph *storage.GraphStorage) (bool, error) {
	hasCycle, err := HasCycle(graph)
	if err != nil {
		return false, err
	}
	return !hasCycle, nil
}

// TopologicalSort returns nodes in topological order using Kahn's algorithm
// Returns error if graph contains a cycle (not a DAG)
// The ordering ensures that for every directed edge u->v, u comes before v
func TopologicalSort(graph *storage.GraphStorage) ([]uint64, error) {
	// First check if it's a DAG
	isDAG, err := IsDAG(graph)
	if err != nil {
		return nil, err
	}
	if !isDAG {
		return nil, fmt.Errorf("graph contains cycles, cannot perform topological sort")
	}

	// Get all nodes
	stats := graph.GetStatistics()
	if stats.NodeCount == 0 {
		return []uint64{}, nil
	}

	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	// Calculate in-degree for each node
	inDegree := make(map[uint64]int)
	for _, nodeID := range nodeIDs {
		inDegree[nodeID] = 0
	}

	for _, nodeID := range nodeIDs {
		outgoing, err := graph.GetOutgoingEdges(nodeID)
		if err != nil {
			continue
		}
		for _, edge := range outgoing {
			inDegree[edge.ToNodeID]++
		}
	}

	// Queue of nodes with in-degree 0
	queue := make([]uint64, 0)
	for _, nodeID := range nodeIDs {
		if inDegree[nodeID] == 0 {
			queue = append(queue, nodeID)
		}
	}

	// Process nodes in topological order
	sorted := make([]uint64, 0, len(nodeIDs))

	for len(queue) > 0 {
		// Dequeue node with in-degree 0
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		// Reduce in-degree of neighbors
		outgoing, err := graph.GetOutgoingEdges(current)
		if err != nil {
			continue
		}

		for _, edge := range outgoing {
			inDegree[edge.ToNodeID]--
			if inDegree[edge.ToNodeID] == 0 {
				queue = append(queue, edge.ToNodeID)
			}
		}
	}

	// If we didn't process all nodes, there's a cycle (shouldn't happen since we checked)
	if len(sorted) != len(nodeIDs) {
		return nil, fmt.Errorf("unexpected cycle detected during sort")
	}

	return sorted, nil
}

// IsTree checks if the graph forms a valid tree structure
// A tree must:
// - Be connected
// - Have exactly n-1 edges for n nodes
// - Contain no cycles
// - Have a single root (node with in-degree 0)
func IsTree(graph *storage.GraphStorage) (bool, error) {
	stats := graph.GetStatistics()

	// Empty graph is not a tree
	if stats.NodeCount == 0 {
		return false, nil
	}

	// Single node is a tree
	if stats.NodeCount == 1 {
		return true, nil
	}

	// Tree must have exactly n-1 edges
	if stats.EdgeCount != stats.NodeCount-1 {
		return false, nil
	}

	// Tree must be acyclic
	isDAG, err := IsDAG(graph)
	if err != nil {
		return false, err
	}
	if !isDAG {
		return false, nil
	}

	// Tree must be connected (all nodes reachable from root)
	isConnected, err := IsConnected(graph)
	if err != nil {
		return false, err
	}
	if !isConnected {
		return false, nil
	}

	// Tree should have exactly one root (in-degree 0)
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	rootCount := 0
	for _, nodeID := range nodeIDs {
		incoming, err := graph.GetIncomingEdges(nodeID)
		if err != nil {
			continue
		}
		if len(incoming) == 0 {
			rootCount++
		}
	}

	return rootCount == 1, nil
}

// IsConnected checks if all nodes in the graph are reachable from any starting node
// For directed graphs, this checks weak connectivity (treating edges as undirected)
func IsConnected(graph *storage.GraphStorage) (bool, error) {
	stats := graph.GetStatistics()

	// Empty graph is considered connected
	if stats.NodeCount == 0 {
		return true, nil
	}

	// Single node is connected
	if stats.NodeCount == 1 {
		return true, nil
	}

	// Get all nodes
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	if len(nodeIDs) == 0 {
		return true, nil
	}

	// BFS from first node (treating graph as undirected)
	visited := make(map[uint64]bool)
	queue := []uint64{nodeIDs[0]}
	visited[nodeIDs[0]] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Get outgoing neighbors
		outgoing, err := graph.GetOutgoingEdges(current)
		if err == nil {
			for _, edge := range outgoing {
				if !visited[edge.ToNodeID] {
					visited[edge.ToNodeID] = true
					queue = append(queue, edge.ToNodeID)
				}
			}
		}

		// Get incoming neighbors (treating as undirected)
		incoming, err := graph.GetIncomingEdges(current)
		if err == nil {
			for _, edge := range incoming {
				if !visited[edge.FromNodeID] {
					visited[edge.FromNodeID] = true
					queue = append(queue, edge.FromNodeID)
				}
			}
		}
	}

	// Check if all nodes were visited
	return len(visited) == len(nodeIDs), nil
}

// IsBipartite checks if the graph can be colored with two colors
// such that no two adjacent nodes have the same color
// Returns (is_bipartite, partition1, partition2, error)
func IsBipartite(graph *storage.GraphStorage) (bool, []uint64, []uint64, error) {
	stats := graph.GetStatistics()

	// Empty graph is bipartite
	if stats.NodeCount == 0 {
		return true, []uint64{}, []uint64{}, nil
	}

	// Get all nodes
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	// Color map: -1 = uncolored, 0 = color A, 1 = color B
	color := make(map[uint64]int)
	for _, nodeID := range nodeIDs {
		color[nodeID] = -1
	}

	partition1 := make([]uint64, 0)
	partition2 := make([]uint64, 0)

	// BFS coloring for each component
	for _, startID := range nodeIDs {
		if color[startID] != -1 {
			continue // Already colored
		}

		// BFS from this node
		queue := []uint64{startID}
		color[startID] = 0
		partition1 = append(partition1, startID)

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			currentColor := color[current]
			nextColor := 1 - currentColor

			// Check all neighbors (treating as undirected)
			neighbors := make([]uint64, 0)

			outgoing, err := graph.GetOutgoingEdges(current)
			if err == nil {
				for _, edge := range outgoing {
					neighbors = append(neighbors, edge.ToNodeID)
				}
			}

			incoming, err := graph.GetIncomingEdges(current)
			if err == nil {
				for _, edge := range incoming {
					neighbors = append(neighbors, edge.FromNodeID)
				}
			}

			for _, neighbor := range neighbors {
				if color[neighbor] == -1 {
					// Uncolored, assign opposite color
					color[neighbor] = nextColor
					queue = append(queue, neighbor)

					if nextColor == 0 {
						partition1 = append(partition1, neighbor)
					} else {
						partition2 = append(partition2, neighbor)
					}
				} else if color[neighbor] == currentColor {
					// Same color as current - not bipartite!
					return false, nil, nil, nil
				}
			}
		}
	}

	return true, partition1, partition2, nil
}
