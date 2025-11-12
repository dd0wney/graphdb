package algorithms

import (
	"container/list"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// BetweennessCentrality computes betweenness centrality for all nodes
// Measures how often a node appears on shortest paths between other nodes
func BetweennessCentrality(graph *storage.GraphStorage) (map[uint64]float64, error) {
	stats := graph.GetStatistics()

	// Get all node IDs - use uint64 to avoid overflow
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	// Initialize betweenness scores
	betweenness := make(map[uint64]float64)
	for _, nodeID := range nodeIDs {
		betweenness[nodeID] = 0.0
	}

	// For each node as source
	for _, source := range nodeIDs {
		// BFS-based shortest path counting
		stack := make([]uint64, 0)
		predecessors := make(map[uint64][]uint64)
		sigma := make(map[uint64]float64) // Number of shortest paths
		distance := make(map[uint64]int)

		for _, nodeID := range nodeIDs {
			predecessors[nodeID] = make([]uint64, 0)
			sigma[nodeID] = 0.0
			distance[nodeID] = -1
		}

		sigma[source] = 1.0
		distance[source] = 0

		// BFS
		queue := list.New()
		queue.PushBack(source)

		for queue.Len() > 0 {
			v := queue.Remove(queue.Front()).(uint64)
			stack = append(stack, v)

			// Get neighbors
			edges, err := graph.GetOutgoingEdges(v)
			if err != nil {
				continue
			}

			for _, edge := range edges {
				w := edge.ToNodeID

				// First time visiting w?
				if distance[w] < 0 {
					queue.PushBack(w)
					distance[w] = distance[v] + 1
				}

				// Shortest path to w via v?
				if distance[w] == distance[v]+1 {
					sigma[w] += sigma[v]
					predecessors[w] = append(predecessors[w], v)
				}
			}
		}

		// Accumulation (back-propagation)
		delta := make(map[uint64]float64)
		for _, nodeID := range nodeIDs {
			delta[nodeID] = 0.0
		}

		// Traverse stack in reverse
		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, v := range predecessors[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1.0 + delta[w])
			}
			if w != source {
				betweenness[w] += delta[w]
			}
		}
	}

	// Normalize for undirected graph
	if len(nodeIDs) > 2 {
		normFactor := 1.0 / float64((len(nodeIDs)-1)*(len(nodeIDs)-2))
		for nodeID := range betweenness {
			betweenness[nodeID] *= normFactor
		}
	}

	return betweenness, nil
}

// ClosenessCentrality computes closeness centrality for all nodes
// Measures average distance from a node to all other nodes
func ClosenessCentrality(graph *storage.GraphStorage) (map[uint64]float64, error) {
	stats := graph.GetStatistics()

	// Get all node IDs - use uint64 to avoid overflow
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	closeness := make(map[uint64]float64)

	for _, source := range nodeIDs {
		// BFS to find distances
		distance := make(map[uint64]int)
		for _, nodeID := range nodeIDs {
			distance[nodeID] = -1
		}
		distance[source] = 0

		queue := list.New()
		queue.PushBack(source)

		for queue.Len() > 0 {
			v := queue.Remove(queue.Front()).(uint64)

			edges, err := graph.GetOutgoingEdges(v)
			if err != nil {
				continue
			}

			for _, edge := range edges {
				w := edge.ToNodeID
				if distance[w] < 0 {
					distance[w] = distance[v] + 1
					queue.PushBack(w)
				}
			}
		}

		// Calculate closeness
		totalDistance := 0
		reachableNodes := 0
		for _, dist := range distance {
			if dist > 0 {
				totalDistance += dist
				reachableNodes++
			}
		}

		if totalDistance > 0 {
			closeness[source] = float64(reachableNodes) / float64(totalDistance)
		} else {
			closeness[source] = 0.0
		}
	}

	return closeness, nil
}

// DegreeCentrality computes degree centrality for all nodes
// Simple count of connections (in-degree + out-degree)
func DegreeCentrality(graph *storage.GraphStorage) (map[uint64]float64, error) {
	stats := graph.GetStatistics()

	// Get all node IDs - use uint64 to avoid overflow
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	degree := make(map[uint64]float64)

	for _, nodeID := range nodeIDs {
		inEdges, _ := graph.GetIncomingEdges(nodeID)
		outEdges, _ := graph.GetOutgoingEdges(nodeID)
		totalDegree := len(inEdges) + len(outEdges)

		// Normalize by max possible degree
		if len(nodeIDs) > 1 {
			degree[nodeID] = float64(totalDegree) / float64(len(nodeIDs)-1)
		} else {
			degree[nodeID] = 0.0
		}
	}

	return degree, nil
}

// CentralityResult contains centrality measures for all nodes
type CentralityResult struct {
	Betweenness      map[uint64]float64
	Closeness        map[uint64]float64
	Degree           map[uint64]float64
	TopByBetweenness []RankedNode
	TopByCloseness   []RankedNode
	TopByDegree      []RankedNode
}

// ComputeAllCentrality computes all centrality measures
func ComputeAllCentrality(graph *storage.GraphStorage) (*CentralityResult, error) {
	betweenness, err := BetweennessCentrality(graph)
	if err != nil {
		return nil, err
	}

	closeness, err := ClosenessCentrality(graph)
	if err != nil {
		return nil, err
	}

	degree, err := DegreeCentrality(graph)
	if err != nil {
		return nil, err
	}

	return &CentralityResult{
		Betweenness:      betweenness,
		Closeness:        closeness,
		Degree:           degree,
		TopByBetweenness: findTopNodes(graph, betweenness, 10),
		TopByCloseness:   findTopNodes(graph, closeness, 10),
		TopByDegree:      findTopNodes(graph, degree, 10),
	}, nil
}
