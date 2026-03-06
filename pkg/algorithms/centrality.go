package algorithms

import (
	"container/heap"
	"container/list"
	"sort"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// predEdge tracks a predecessor node and the edge used to reach it during BFS.
// This allows the back-propagation phase to accumulate flow onto specific edges.
type predEdge struct {
	nodeID uint64
	edgeID uint64
}

// brandesCentrality runs a single O(VE) Brandes pass and returns both node and
// edge betweenness centrality (raw, unnormalised). The caller is responsible for
// normalisation so that BetweennessCentrality and EdgeBetweennessCentrality can
// each apply the appropriate factor.
func brandesCentrality(graph *storage.GraphStorage) (nodeBetweenness map[uint64]float64, edgeBetweenness map[uint64]float64, nodeIDs []uint64, err error) {
	stats := graph.GetStatistics()

	nodeIDs = make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	nodeBetweenness = make(map[uint64]float64, len(nodeIDs))
	edgeBetweenness = make(map[uint64]float64)
	for _, nodeID := range nodeIDs {
		nodeBetweenness[nodeID] = 0.0
	}

	for _, source := range nodeIDs {
		stack := make([]uint64, 0, len(nodeIDs))
		predecessors := make(map[uint64][]predEdge, len(nodeIDs))
		sigma := make(map[uint64]float64, len(nodeIDs))
		distance := make(map[uint64]int, len(nodeIDs))

		for _, nodeID := range nodeIDs {
			predecessors[nodeID] = nil
			sigma[nodeID] = 0.0
			distance[nodeID] = -1
		}

		sigma[source] = 1.0
		distance[source] = 0

		queue := list.New()
		queue.PushBack(source)

		for queue.Len() > 0 {
			v, ok := queue.Remove(queue.Front()).(uint64)
			if !ok {
				continue
			}
			stack = append(stack, v)

			edges, edgeErr := graph.GetOutgoingEdges(v)
			if edgeErr != nil {
				continue
			}

			for _, edge := range edges {
				w := edge.ToNodeID

				if distance[w] < 0 {
					queue.PushBack(w)
					distance[w] = distance[v] + 1
				}

				if distance[w] == distance[v]+1 {
					sigma[w] += sigma[v]
					predecessors[w] = append(predecessors[w], predEdge{
						nodeID: v,
						edgeID: edge.ID,
					})
				}
			}
		}

		// Back-propagation: accumulate onto both nodes and edges
		delta := make(map[uint64]float64, len(nodeIDs))
		for _, nodeID := range nodeIDs {
			delta[nodeID] = 0.0
		}

		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, pred := range predecessors[w] {
				contribution := (sigma[pred.nodeID] / sigma[w]) * (1.0 + delta[w])
				delta[pred.nodeID] += contribution
				edgeBetweenness[pred.edgeID] += contribution
			}
			if w != source {
				nodeBetweenness[w] += delta[w]
			}
		}
	}

	return nodeBetweenness, edgeBetweenness, nodeIDs, nil
}

// BetweennessCentrality computes betweenness centrality for all nodes.
// Measures how often a node appears on shortest paths between other nodes.
func BetweennessCentrality(graph *storage.GraphStorage) (map[uint64]float64, error) {
	nodeBetweenness, _, nodeIDs, err := brandesCentrality(graph)
	if err != nil {
		return nil, err
	}

	if len(nodeIDs) > 2 {
		normFactor := 1.0 / float64((len(nodeIDs)-1)*(len(nodeIDs)-2))
		for nodeID := range nodeBetweenness {
			nodeBetweenness[nodeID] *= normFactor
		}
	}

	return nodeBetweenness, nil
}

// RankedEdge holds a ranked edge with its betweenness centrality score.
type RankedEdge struct {
	EdgeID     uint64  `json:"edge_id"`
	FromNodeID uint64  `json:"from_node_id"`
	ToNodeID   uint64  `json:"to_node_id"`
	Score      float64 `json:"score"`
}

// EdgeBetweennessResult contains edge betweenness centrality in multiple
// representations for different access patterns.
type EdgeBetweennessResult struct {
	// ByEdgeID maps each directed edge ID to its BC score.
	ByEdgeID map[uint64]float64 `json:"by_edge_id"`
	// ByNodePair maps [fromNodeID, toNodeID] to the directed edge's BC score.
	ByNodePair map[[2]uint64]float64 `json:"by_node_pair"`
	// TopEdges lists the top edges ranked by BC score (descending).
	TopEdges []RankedEdge `json:"top_edges"`
}

// EdgeBetweennessCentrality computes betweenness centrality for all edges.
// Measures how often an edge appears on shortest paths between all node pairs.
// Uses the same O(VE) Brandes pass as BetweennessCentrality.
func EdgeBetweennessCentrality(graph *storage.GraphStorage) (*EdgeBetweennessResult, error) {
	_, edgeBetweenness, nodeIDs, err := brandesCentrality(graph)
	if err != nil {
		return nil, err
	}

	n := len(nodeIDs)

	// Normalise: 1/(n*(n-1)) for directed graphs
	if n > 1 {
		normFactor := 1.0 / float64(n*(n-1))
		for edgeID := range edgeBetweenness {
			edgeBetweenness[edgeID] *= normFactor
		}
	}

	// Build ByNodePair by looking up each edge's endpoints
	byNodePair := make(map[[2]uint64]float64, len(edgeBetweenness))
	for edgeID, score := range edgeBetweenness {
		edge, edgeErr := graph.GetEdge(edgeID)
		if edgeErr != nil {
			continue
		}
		byNodePair[[2]uint64{edge.FromNodeID, edge.ToNodeID}] = score
	}

	topEdges := findTopEdges(graph, edgeBetweenness, 10)

	return &EdgeBetweennessResult{
		ByEdgeID:   edgeBetweenness,
		ByNodePair: byNodePair,
		TopEdges:   topEdges,
	}, nil
}

// rankedEdgeHeap implements a min-heap for RankedEdge by score.
type rankedEdgeHeap []RankedEdge

func (h rankedEdgeHeap) Len() int           { return len(h) }
func (h rankedEdgeHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h rankedEdgeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *rankedEdgeHeap) Push(x any) {
	*h = append(*h, x.(RankedEdge))
}

func (h *rankedEdgeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// findTopEdges returns the top n edges by score using a min-heap.
func findTopEdges(graph *storage.GraphStorage, scores map[uint64]float64, n int) []RankedEdge {
	if n <= 0 {
		return nil
	}

	h := make(rankedEdgeHeap, 0, n)
	heap.Init(&h)

	for edgeID, score := range scores {
		edge, err := graph.GetEdge(edgeID)
		if err != nil {
			continue
		}

		re := RankedEdge{
			EdgeID:     edgeID,
			FromNodeID: edge.FromNodeID,
			ToNodeID:   edge.ToNodeID,
			Score:      score,
		}

		if h.Len() < n {
			heap.Push(&h, re)
		} else if score > h[0].Score {
			heap.Pop(&h)
			heap.Push(&h, re)
		}
	}

	result := make([]RankedEdge, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		result[i] = heap.Pop(&h).(RankedEdge)
	}

	// Stable sort by score descending, then edge ID ascending for determinism
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].EdgeID < result[j].EdgeID
	})

	return result
}

// CentralityResult contains centrality measures for all nodes and edges.
type CentralityResult struct {
	Betweenness          map[uint64]float64
	Closeness            map[uint64]float64
	Degree               map[uint64]float64
	EdgeBetweenness      *EdgeBetweennessResult
	TopByBetweenness     []RankedNode
	TopByCloseness       []RankedNode
	TopByDegree          []RankedNode
	TopByEdgeBetweenness []RankedEdge
}

// ComputeAllCentrality computes all centrality measures in a single pass where
// possible. Node and edge betweenness share one Brandes traversal.
func ComputeAllCentrality(graph *storage.GraphStorage) (*CentralityResult, error) {
	nodeBetweenness, edgeBetweennessRaw, nodeIDs, err := brandesCentrality(graph)
	if err != nil {
		return nil, err
	}

	// Normalise node betweenness
	n := len(nodeIDs)
	if n > 2 {
		normFactor := 1.0 / float64((n-1)*(n-2))
		for nodeID := range nodeBetweenness {
			nodeBetweenness[nodeID] *= normFactor
		}
	}

	// Normalise edge betweenness
	if n > 1 {
		normFactor := 1.0 / float64(n*(n-1))
		for edgeID := range edgeBetweennessRaw {
			edgeBetweennessRaw[edgeID] *= normFactor
		}
	}

	// Build edge BC result
	byNodePair := make(map[[2]uint64]float64, len(edgeBetweennessRaw))
	for edgeID, score := range edgeBetweennessRaw {
		edge, edgeErr := graph.GetEdge(edgeID)
		if edgeErr != nil {
			continue
		}
		byNodePair[[2]uint64{edge.FromNodeID, edge.ToNodeID}] = score
	}
	topEdges := findTopEdges(graph, edgeBetweennessRaw, 10)

	edgeBCResult := &EdgeBetweennessResult{
		ByEdgeID:   edgeBetweennessRaw,
		ByNodePair: byNodePair,
		TopEdges:   topEdges,
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
		Betweenness:          nodeBetweenness,
		Closeness:            closeness,
		Degree:               degree,
		EdgeBetweenness:      edgeBCResult,
		TopByBetweenness:     findTopNodes(graph, nodeBetweenness, 10),
		TopByCloseness:       findTopNodes(graph, closeness, 10),
		TopByDegree:          findTopNodes(graph, degree, 10),
		TopByEdgeBetweenness: topEdges,
	}, nil
}

// ClosenessCentrality computes closeness centrality for all nodes.
// Measures average distance from a node to all other nodes.
func ClosenessCentrality(graph *storage.GraphStorage) (map[uint64]float64, error) {
	stats := graph.GetStatistics()

	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	closeness := make(map[uint64]float64)

	for _, source := range nodeIDs {
		distance := make(map[uint64]int)
		for _, nodeID := range nodeIDs {
			distance[nodeID] = -1
		}
		distance[source] = 0

		queue := list.New()
		queue.PushBack(source)

		for queue.Len() > 0 {
			v, ok := queue.Remove(queue.Front()).(uint64)
			if !ok {
				continue
			}

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

// DegreeCentrality computes degree centrality for all nodes.
// Simple count of connections (in-degree + out-degree).
func DegreeCentrality(graph *storage.GraphStorage) (map[uint64]float64, error) {
	stats := graph.GetStatistics()

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

		if len(nodeIDs) > 1 {
			degree[nodeID] = float64(totalDegree) / float64(len(nodeIDs)-1)
		} else {
			degree[nodeID] = 0.0
		}
	}

	return degree, nil
}
