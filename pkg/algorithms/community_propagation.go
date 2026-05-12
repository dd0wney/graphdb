package algorithms

import "github.com/dd0wney/cluso-graphdb/pkg/storage"

// LabelPropagation performs label propagation for community detection
// Fast, scalable algorithm for large graphs
func LabelPropagation(graph storage.StorageReader, maxIterations int) (*CommunityDetectionResult, error) {
	return labelPropagationView(newTenantBlindView(graph), maxIterations)
}

func labelPropagationView(view graphView, maxIterations int) (*CommunityDetectionResult, error) {
	allNodes := view.AllNodes()
	nodeIDs := make([]uint64, 0, len(allNodes))
	for _, n := range allNodes {
		nodeIDs = append(nodeIDs, n.ID)
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
			outEdges, _ := view.OutgoingEdges(nodeID)
			inEdges, _ := view.IncomingEdges(nodeID)

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
		Modularity:    calculateModularityView(view, nodeCommunity),
	}, nil
}
