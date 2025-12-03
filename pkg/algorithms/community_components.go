package algorithms

import (
	"container/list"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ConnectedComponents finds all connected components in the graph
func ConnectedComponents(graph *storage.GraphStorage) (*CommunityDetectionResult, error) {
	stats := graph.GetStatistics()

	// Get all node IDs - use uint64 to avoid overflow
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	visited := make(map[uint64]bool)
	nodeCommunity := make(map[uint64]int)
	communities := make([]*Community, 0)
	communityID := 0

	// BFS to find each component
	for _, startNode := range nodeIDs {
		if visited[startNode] {
			continue
		}

		// New component found
		component := &Community{
			ID:    communityID,
			Nodes: make([]uint64, 0),
		}

		queue := list.New()
		queue.PushBack(startNode)
		visited[startNode] = true

		for queue.Len() > 0 {
			nodeID, ok := queue.Remove(queue.Front()).(uint64)
			if !ok {
				continue
			}
			component.Nodes = append(component.Nodes, nodeID)
			nodeCommunity[nodeID] = communityID

			// Get all neighbors (both incoming and outgoing)
			outEdges, _ := graph.GetOutgoingEdges(nodeID)
			inEdges, _ := graph.GetIncomingEdges(nodeID)

			for _, edge := range outEdges {
				if !visited[edge.ToNodeID] {
					visited[edge.ToNodeID] = true
					queue.PushBack(edge.ToNodeID)
				}
			}

			for _, edge := range inEdges {
				if !visited[edge.FromNodeID] {
					visited[edge.FromNodeID] = true
					queue.PushBack(edge.FromNodeID)
				}
			}
		}

		component.Size = len(component.Nodes)
		communities = append(communities, component)
		communityID++
	}

	return &CommunityDetectionResult{
		Communities:   communities,
		NodeCommunity: nodeCommunity,
		Modularity:    0.0, // TODO: Calculate modularity
	}, nil
}
