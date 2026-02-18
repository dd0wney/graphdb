package algorithms

import "github.com/dd0wney/cluso-graphdb/pkg/storage"

// Community represents a detected community
type Community struct {
	ID      int
	Nodes   []uint64
	Size    int
	Density float64 // Edge density within community
}

// CommunityDetectionResult contains detected communities
type CommunityDetectionResult struct {
	Communities   []*Community
	Modularity    float64        // Quality measure of the partitioning
	NodeCommunity map[uint64]int // Node ID -> Community ID
}

// CalculateModularity computes the modularity score for a community partition.
// Modularity measures the quality of a partition: values > 0.3 indicate
// significant community structure, values > 0.7 indicate strong structure.
// Range: [-0.5, 1.0]
//
// Formula: Q = Σ_c [(l_c / m) - (d_c / 2m)²]
// Where:
//   - m = total number of edges
//   - l_c = number of edges within community c
//   - d_c = sum of degrees of nodes in community c
func CalculateModularity(graph *storage.GraphStorage, nodeCommunity map[uint64]int) float64 {
	if len(nodeCommunity) == 0 {
		return 0.0
	}

	// Calculate total edges and node degrees
	totalEdges := 0
	nodeDegree := make(map[uint64]int)

	for nodeID := range nodeCommunity {
		outEdges, err := graph.GetOutgoingEdges(nodeID)
		if err != nil {
			continue
		}
		inEdges, err := graph.GetIncomingEdges(nodeID)
		if err != nil {
			continue
		}

		// For undirected modularity, count each edge once
		// Degree = outgoing + incoming (may double-count for undirected)
		degree := len(outEdges) + len(inEdges)
		nodeDegree[nodeID] = degree
		totalEdges += len(outEdges) // Count only outgoing to avoid double-counting
	}

	if totalEdges == 0 {
		return 0.0
	}

	m := float64(totalEdges)
	twoM := 2.0 * m

	// Group nodes by community
	communityNodes := make(map[int][]uint64)
	for nodeID, commID := range nodeCommunity {
		communityNodes[commID] = append(communityNodes[commID], nodeID)
	}

	// Calculate modularity
	modularity := 0.0

	for _, nodes := range communityNodes {
		// Create set for fast lookup
		nodeSet := make(map[uint64]bool)
		for _, n := range nodes {
			nodeSet[n] = true
		}

		// Count edges within community (l_c)
		edgesWithin := 0
		// Sum of degrees in community (d_c)
		degreeSum := 0

		for _, nodeID := range nodes {
			degreeSum += nodeDegree[nodeID]

			outEdges, err := graph.GetOutgoingEdges(nodeID)
			if err != nil {
				continue
			}

			for _, edge := range outEdges {
				if nodeSet[edge.ToNodeID] {
					edgesWithin++
				}
			}
		}

		// Q_c = (l_c / m) - (d_c / 2m)²
		lc := float64(edgesWithin)
		dc := float64(degreeSum)

		modularity += (lc / m) - (dc/twoM)*(dc/twoM)
	}

	return modularity
}
