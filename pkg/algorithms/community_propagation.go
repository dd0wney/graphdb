package algorithms

import "github.com/dd0wney/cluso-graphdb/pkg/storage"

// LabelPropagation performs label propagation for community detection
// Fast, scalable algorithm for large graphs
func LabelPropagation(graph *storage.GraphStorage, maxIterations int) (*CommunityDetectionResult, error) {
	stats := graph.GetStatistics()

	// Get all node IDs - use uint64 to avoid overflow
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	// Initialize: each node in its own community
	labels := make(map[uint64]int)
	for i, nodeID := range nodeIDs {
		labels[nodeID] = i
	}

	// Iterate until convergence or max iterations
	for iter := 0; iter < maxIterations; iter++ {
		changed := false

		for _, nodeID := range nodeIDs {
			// Count neighbor labels
			labelCount := make(map[int]int)

			// Get neighbors
			outEdges, _ := graph.GetOutgoingEdges(nodeID)
			inEdges, _ := graph.GetIncomingEdges(nodeID)

			for _, edge := range outEdges {
				neighbor := edge.ToNodeID
				labelCount[labels[neighbor]]++
			}

			for _, edge := range inEdges {
				neighbor := edge.FromNodeID
				labelCount[labels[neighbor]]++
			}

			// Find most frequent label
			maxCount := 0
			maxLabel := labels[nodeID]
			for label, count := range labelCount {
				if count > maxCount {
					maxCount = count
					maxLabel = label
				}
			}

			// Update label if changed
			if maxLabel != labels[nodeID] {
				labels[nodeID] = maxLabel
				changed = true
			}
		}

		if !changed {
			break // Converged
		}
	}

	// Build communities from labels
	communityNodes := make(map[int][]uint64)
	for nodeID, label := range labels {
		communityNodes[label] = append(communityNodes[label], nodeID)
	}

	communities := make([]*Community, 0, len(communityNodes))
	nodeCommunity := make(map[uint64]int)
	communityID := 0

	for _, nodes := range communityNodes {
		if len(nodes) == 0 {
			continue
		}

		community := &Community{
			ID:    communityID,
			Nodes: nodes,
			Size:  len(nodes),
		}

		for _, nodeID := range nodes {
			nodeCommunity[nodeID] = communityID
		}

		communities = append(communities, community)
		communityID++
	}

	return &CommunityDetectionResult{
		Communities:   communities,
		NodeCommunity: nodeCommunity,
		Modularity:    CalculateModularity(graph, nodeCommunity),
	}, nil
}
