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

// CountTriangles counts triangles in the graph, treating all edges
// as undirected (tenant-blind). Multi-tenant API callers must use
// CountTrianglesForTenant.
func CountTriangles(graph storage.StorageReader) (*TriangleCountResult, error) {
	return countTrianglesView(newTenantBlindView(graph))
}

// CountTrianglesForTenant counts triangles within the caller's
// tenant subgraph. Audit A6c-algorithms (2026-05-08).
func CountTrianglesForTenant(graph storage.StorageReader, tenantID string) (*TriangleCountResult, error) {
	return countTrianglesView(newTenantScopedView(graph, tenantID))
}

// countTrianglesView is the shared algorithm body. For each node u
// it iterates over pairs (v,w) in u's neighbor set; if v and w are
// also neighbors, that's a triangle. Each triangle is counted once
// per participating node, so GlobalCount = sum(PerNode) / 3.
// Clustering coefficients are computed in the same pass.
func countTrianglesView(view graphView) (*TriangleCountResult, error) {
	allNodes := view.AllNodes()
	nodeIDs := make([]uint64, 0, len(allNodes))
	for _, n := range allNodes {
		nodeIDs = append(nodeIDs, n.ID)
	}

	// Build undirected neighbor sets for all nodes
	neighborSets := make(map[uint64]map[uint64]bool, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		neighbors := make(map[uint64]bool)

		// Defensive: if Outgoing/IncomingEdges fails (future LSM/snapshot
		// views may), proceed with whatever edges we did read. The node's
		// triangle count will be undercounted, never inflated.
		outEdges, err := view.OutgoingEdges(nodeID)
		if err != nil {
			outEdges = nil
		}
		for _, e := range outEdges {
			neighbors[e.ToNodeID] = true
		}

		inEdges, err := view.IncomingEdges(nodeID)
		if err != nil {
			inEdges = nil
		}
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

	total := 0
	for _, c := range perNode {
		total += c
	}
	globalCount := total / 3

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

	// TopNodes (convert int counts to float64)
	floatScores := make(map[uint64]float64, len(perNode))
	for id, c := range perNode {
		floatScores[id] = float64(c)
	}
	topNodes := findTopNodesView(view, floatScores, 10)

	return &TriangleCountResult{
		PerNode:                perNode,
		GlobalCount:            globalCount,
		ClusteringCoefficients: coefficients,
		TopNodes:               topNodes,
	}, nil
}
