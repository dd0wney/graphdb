package algorithms

import (
	"container/list"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

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
			nodeID := queue.Remove(queue.Front()).(uint64)
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
		Modularity:    0.0, // TODO: Calculate modularity
	}, nil
}

// ClusteringCoefficient computes local clustering coefficient for all nodes
// Measures how close a node's neighbors are to being a complete graph
func ClusteringCoefficient(graph *storage.GraphStorage) (map[uint64]float64, error) {
	stats := graph.GetStatistics()

	// Get all node IDs - use uint64 to avoid overflow
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
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

		// Count triangles
		triangles := 0
		for i := 0; i < len(neighborsSlice); i++ {
			for j := i + 1; j < len(neighborsSlice); j++ {
				// Check if neighbors[i] connects to neighbors[j]
				edges, err := graph.GetOutgoingEdges(neighborsSlice[i])
				if err == nil {
					for _, edge := range edges {
						if edge.ToNodeID == neighborsSlice[j] {
							triangles++
							break
						}
					}
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
