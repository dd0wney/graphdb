package algorithms

import "github.com/dd0wney/cluso-graphdb/pkg/storage"

// ClusteringCoefficient computes local clustering coefficient for all nodes
// Measures how close a node's neighbors are to being a complete graph
func ClusteringCoefficient(graph *storage.GraphStorage) (map[uint64]float64, error) {
	stats := graph.GetStatistics()

	// Get all node IDs - use uint64 to avoid overflow
	// Note: Node IDs start from 1 and are sequential, but we need to scan
	// up to at least NodeCount + some buffer to handle any timing issues
	// with stats updates. The actual upper bound should be NodeCount * 2 or
	// the actual nextNodeID, but we don't have direct access to that.
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	maxID := stats.NodeCount + 10 // Small buffer for timing
	if maxID < 100 {
		maxID = 100 // Minimum scan range
	}
	for i := uint64(1); i <= maxID; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
		// Stop early if we've found all expected nodes
		if uint64(len(nodeIDs)) >= stats.NodeCount && stats.NodeCount > 0 {
			break
		}
	}

	coefficients := make(map[uint64]float64)

	for _, nodeID := range nodeIDs {
		// Get neighbors
		outEdges, _ := graph.GetOutgoingEdges(nodeID)
		neighbors := make(map[uint64]bool)

		for _, edge := range outEdges {
			neighbors[edge.ToNodeID] = true
		}

		neighborsSlice := make([]uint64, 0, len(neighbors))
		for neighbor := range neighbors {
			neighborsSlice = append(neighborsSlice, neighbor)
		}

		if len(neighborsSlice) < 2 {
			coefficients[nodeID] = 0.0
			continue
		}

		// Count triangles - use pre-computed neighbor sets for O(k²) instead of O(k² * d)
		// where k = number of neighbors and d = average degree
		triangles := 0

		// Pre-build neighbor sets for all neighbors to avoid repeated graph lookups
		neighborSets := make(map[uint64]map[uint64]bool, len(neighborsSlice))
		for _, neighbor := range neighborsSlice {
			neighborSet := make(map[uint64]bool)
			edges, err := graph.GetOutgoingEdges(neighbor)
			if err == nil {
				for _, edge := range edges {
					neighborSet[edge.ToNodeID] = true
				}
			}
			neighborSets[neighbor] = neighborSet
		}

		// Now check pairs using O(1) set lookup
		for i := 0; i < len(neighborsSlice); i++ {
			for j := i + 1; j < len(neighborsSlice); j++ {
				// Check if neighbors[i] connects to neighbors[j]
				if neighborSets[neighborsSlice[i]][neighborsSlice[j]] {
					triangles++
				}
			}
		}

		// Clustering coefficient = actual triangles / possible triangles
		k := len(neighborsSlice)
		possibleTriangles := k * (k - 1) / 2
		if possibleTriangles > 0 {
			coefficients[nodeID] = float64(triangles) / float64(possibleTriangles)
		} else {
			coefficients[nodeID] = 0.0
		}
	}

	return coefficients, nil
}

// AverageClusteringCoefficient computes the average clustering coefficient
func AverageClusteringCoefficient(graph *storage.GraphStorage) (float64, error) {
	coefficients, err := ClusteringCoefficient(graph)
	if err != nil {
		return 0.0, err
	}

	if len(coefficients) == 0 {
		return 0.0, nil
	}

	sum := 0.0
	for _, coef := range coefficients {
		sum += coef
	}

	return sum / float64(len(coefficients)), nil
}
