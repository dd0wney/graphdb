package algorithms

import (
	"container/list"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ConnectedComponents finds all connected components in the graph
func ConnectedComponents(graph storage.StorageReader) (*CommunityDetectionResult, error) {
	return connectedComponentsView(newTenantBlindView(graph))
}

func connectedComponentsView(view graphView) (*CommunityDetectionResult, error) {
	allNodes := view.AllNodes()
	nodeIDs := make([]uint64, 0, len(allNodes))
	for _, n := range allNodes {
		nodeIDs = append(nodeIDs, n.ID)
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
			outEdges, _ := view.OutgoingEdges(nodeID)
			inEdges, _ := view.IncomingEdges(nodeID)

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
		Modularity:    calculateModularityView(view, nodeCommunity),
	}, nil
}
