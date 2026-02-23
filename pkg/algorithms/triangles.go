package algorithms

import "github.com/dd0wney/cluso-graphdb/pkg/storage"

// TriangleCountResult holds triangle counting results including per-node counts,
// global count, clustering coefficients, and top nodes by triangle participation.
type TriangleCountResult struct {
	PerNode                map[uint64]int
	GlobalCount            int
	ClusteringCoefficients map[uint64]float64
	TopNodes               []RankedNode
}

// CountTriangles counts triangles in the graph, treating all edges as undirected.
// For each node u, it iterates over pairs (v,w) in u's neighbor set; if v and w
// are also neighbors, that's a triangle. Each triangle is counted once per
// participating node, so GlobalCount = sum(PerNode) / 3.
// Clustering coefficients are computed in the same pass.
func CountTriangles(graph *storage.GraphStorage) (*TriangleCountResult, error) {
	stats := graph.GetStatistics()

	nodeIDs := make([]uint64, 0, stats.NodeCount)
	maxID := stats.NodeCount + 10
	if maxID < 100 {
		maxID = 100
	}
	for i := uint64(1); i <= maxID; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
		if uint64(len(nodeIDs)) >= stats.NodeCount && stats.NodeCount > 0 {
			break
		}
	}

	// Build undirected neighbor sets for all nodes
	neighborSets := make(map[uint64]map[uint64]bool, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		neighbors := make(map[uint64]bool)

		outEdges, _ := graph.GetOutgoingEdges(nodeID)
		for _, e := range outEdges {
			neighbors[e.ToNodeID] = true
		}

		inEdges, _ := graph.GetIncomingEdges(nodeID)
		for _, e := range inEdges {
			neighbors[e.FromNodeID] = true
		}

		// Exclude self-loops
		delete(neighbors, nodeID)
		neighborSets[nodeID] = neighbors
	}

	perNode := make(map[uint64]int, len(nodeIDs))
	for _, u := range nodeIDs {
		uNeighbors := neighborSets[u]
		neighborsSlice := make([]uint64, 0, len(uNeighbors))
		for v := range uNeighbors {
			neighborsSlice = append(neighborsSlice, v)
		}

		count := 0
		for i := 0; i < len(neighborsSlice); i++ {
			v := neighborsSlice[i]
			for j := i + 1; j < len(neighborsSlice); j++ {
				w := neighborsSlice[j]
				if neighborSets[v][w] {
					count++
				}
			}
		}
		perNode[u] = count
	}

	// GlobalCount: each triangle counted 3 times (once per vertex)
	total := 0
	for _, c := range perNode {
		total += c
	}
	globalCount := total / 3

	// Clustering coefficients
	coefficients := make(map[uint64]float64, len(nodeIDs))
	for _, u := range nodeIDs {
		k := len(neighborSets[u])
		if k < 2 {
			coefficients[u] = 0.0
			continue
		}
		possible := k * (k - 1) / 2
		coefficients[u] = float64(perNode[u]) / float64(possible)
	}

	// TopNodes via findTopNodes (convert int counts to float64)
	floatScores := make(map[uint64]float64, len(perNode))
	for id, c := range perNode {
		floatScores[id] = float64(c)
	}
	topNodes := findTopNodes(graph, floatScores, 10)

	return &TriangleCountResult{
		PerNode:                perNode,
		GlobalCount:            globalCount,
		ClusteringCoefficients: coefficients,
		TopNodes:               topNodes,
	}, nil
}
