package algorithms

import (
	"container/list"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ShortestPath finds the shortest path between two nodes using bidirectional BFS
// This is 2x faster than unidirectional BFS for large graphs
func ShortestPath(graph *storage.GraphStorage, startID, endID uint64) ([]uint64, error) {
	if startID == endID {
		return []uint64{startID}, nil
	}

	// Forward search from start
	forwardQueue := list.New()
	forwardVisited := make(map[uint64]uint64) // node -> parent
	forwardQueue.PushBack(startID)
	forwardVisited[startID] = startID

	// Backward search from end
	backwardQueue := list.New()
	backwardVisited := make(map[uint64]uint64) // node -> parent
	backwardQueue.PushBack(endID)
	backwardVisited[endID] = endID

	// Bidirectional BFS
	for forwardQueue.Len() > 0 || backwardQueue.Len() > 0 {
		// Expand forward frontier
		if forwardQueue.Len() > 0 {
			meetingNode := expandFrontier(graph, forwardQueue, forwardVisited, backwardVisited)
			if meetingNode != 0 {
				return reconstructPath(meetingNode, forwardVisited, backwardVisited), nil
			}
		}

		// Expand backward frontier
		if backwardQueue.Len() > 0 {
			meetingNode := expandFrontier(graph, backwardQueue, backwardVisited, forwardVisited)
			if meetingNode != 0 {
				return reconstructPath(meetingNode, forwardVisited, backwardVisited), nil
			}
		}
	}

	return nil, nil // No path found
}

// expandFrontier expands one level of BFS from the queue
func expandFrontier(
	graph *storage.GraphStorage,
	queue *list.List,
	visited map[uint64]uint64,
	otherVisited map[uint64]uint64,
) uint64 {
	// Process one level
	levelSize := queue.Len()
	for i := 0; i < levelSize; i++ {
		currentID := queue.Remove(queue.Front()).(uint64)

		// Get neighbors
		edges, err := graph.GetOutgoingEdges(currentID)
		if err != nil {
			continue
		}

		for _, edge := range edges {
			neighborID := edge.ToNodeID

			// Check if we've met the other search
			if _, found := otherVisited[neighborID]; found {
				visited[neighborID] = currentID
				return neighborID
			}

			// Add unvisited neighbors
			if _, seen := visited[neighborID]; !seen {
				visited[neighborID] = currentID
				queue.PushBack(neighborID)
			}
		}
	}

	return 0 // No meeting point yet
}

// reconstructPath builds the path from start to end
func reconstructPath(
	meetingNode uint64,
	forwardVisited map[uint64]uint64,
	backwardVisited map[uint64]uint64,
) []uint64 {
	// Build forward path (start -> meeting)
	forwardPath := make([]uint64, 0)
	node := meetingNode
	for node != forwardVisited[node] {
		forwardPath = append(forwardPath, node)
		node = forwardVisited[node]
	}
	forwardPath = append(forwardPath, node) // Add start node

	// Reverse forward path
	for i, j := 0, len(forwardPath)-1; i < j; i, j = i+1, j-1 {
		forwardPath[i], forwardPath[j] = forwardPath[j], forwardPath[i]
	}

	// Build backward path (meeting -> end), excluding meeting node
	backwardPath := make([]uint64, 0)
	node = backwardVisited[meetingNode]
	// Skip if meetingNode's parent is itself (meeting node is the end)
	if node != meetingNode {
		for node != backwardVisited[node] {
			backwardPath = append(backwardPath, node)
			node = backwardVisited[node]
		}
		backwardPath = append(backwardPath, node) // Add end node
	}

	// Combine paths
	return append(forwardPath, backwardPath...)
}

// AllShortestPaths finds all shortest paths from a source node using BFS
func AllShortestPaths(graph *storage.GraphStorage, sourceID uint64) (map[uint64]int, error) {
	distances := make(map[uint64]int)
	distances[sourceID] = 0

	queue := list.New()
	queue.PushBack(sourceID)

	for queue.Len() > 0 {
		currentID := queue.Remove(queue.Front()).(uint64)
		currentDist := distances[currentID]

		edges, err := graph.GetOutgoingEdges(currentID)
		if err != nil {
			continue
		}

		for _, edge := range edges {
			neighborID := edge.ToNodeID
			if _, visited := distances[neighborID]; !visited {
				distances[neighborID] = currentDist + 1
				queue.PushBack(neighborID)
			}
		}
	}

	return distances, nil
}

// WeightedShortestPath finds shortest path with edge weights using Dijkstra's algorithm
func WeightedShortestPath(graph *storage.GraphStorage, startID, endID uint64) ([]uint64, float64, error) {
	// Priority queue using simple slice (for simplicity, not optimal)
	type pqItem struct {
		nodeID   uint64
		distance float64
	}

	distances := make(map[uint64]float64)
	parent := make(map[uint64]uint64)
	distances[startID] = 0
	parent[startID] = startID

	pq := []pqItem{{startID, 0}}

	for len(pq) > 0 {
		// Extract min (simple linear search)
		minIdx := 0
		for i := 1; i < len(pq); i++ {
			if pq[i].distance < pq[minIdx].distance {
				minIdx = i
			}
		}

		current := pq[minIdx]
		pq = append(pq[:minIdx], pq[minIdx+1:]...)

		// Found target
		if current.nodeID == endID {
			// Reconstruct path
			path := make([]uint64, 0)
			node := endID
			for node != startID {
				path = append(path, node)
				node = parent[node]
			}
			path = append(path, startID)

			// Reverse path
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}

			return path, distances[endID], nil
		}

		// Process neighbors
		edges, err := graph.GetOutgoingEdges(current.nodeID)
		if err != nil {
			continue
		}

		for _, edge := range edges {
			neighborID := edge.ToNodeID
			newDist := current.distance + edge.Weight

			if oldDist, visited := distances[neighborID]; !visited || newDist < oldDist {
				distances[neighborID] = newDist
				parent[neighborID] = current.nodeID
				pq = append(pq, pqItem{neighborID, newDist})
			}
		}
	}

	return nil, 0, nil // No path found
}
