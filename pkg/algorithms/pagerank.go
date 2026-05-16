package algorithms

import (
	"container/heap"
	"math"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// PageRankOptions configures PageRank algorithm
type PageRankOptions struct {
	DampingFactor float64 // Usually 0.85
	MaxIterations int
	Tolerance     float64 // Convergence threshold
}

// DefaultPageRankOptions returns default PageRank configuration
func DefaultPageRankOptions() PageRankOptions {
	return PageRankOptions{
		DampingFactor: 0.85,
		MaxIterations: 100,
		Tolerance:     1e-6,
	}
}

// PageRankResult contains PageRank scores for all nodes
type PageRankResult struct {
	Scores     map[uint64]float64 // Node ID -> PageRank score
	Iterations int                // Number of iterations performed
	Converged  bool               // Whether algorithm converged
	TopNodes   []RankedNode       // Top N nodes by score
}

// RankedNode represents a node with its rank
type RankedNode struct {
	NodeID uint64
	Score  float64
	Node   *storage.Node
}

// PageRank computes PageRank scores for all nodes in the graph
// (tenant-blind — runs across every tenant). Used by CLI, demos, and
// single-tenant deployments. Multi-tenant API callers must use
// PageRankForTenant.
func PageRank(graph storage.Storage, opts PageRankOptions) (*PageRankResult, error) {
	return pageRankView(newTenantBlindView(graph), opts)
}

// PageRankForTenant computes PageRank scores for nodes owned by the
// given tenant. Audit A6c-algorithms (2026-05-08): same algorithm
// body as PageRank, but the underlying graph access is restricted to
// the tenant — foreign-tenant nodes are excluded from the scoring
// graph and edges to foreign-tenant nodes are dropped at expansion.
func PageRankForTenant(graph storage.Storage, opts PageRankOptions, tenantID string) (*PageRankResult, error) {
	return pageRankView(newTenantScopedView(graph, tenantID), opts)
}

// pageRankView is the shared algorithm body. Operates against a
// graphView so tenant-blind and tenant-scoped public functions can
// share one implementation — see pkg/algorithms/view.go.
func pageRankView(view graphView, opts PageRankOptions) (*PageRankResult, error) {
	allNodes := view.AllNodes()

	if len(allNodes) == 0 {
		return &PageRankResult{
			Scores:    make(map[uint64]float64),
			Converged: true,
		}, nil
	}

	nodeIDs := make([]uint64, 0, len(allNodes))
	for _, n := range allNodes {
		nodeIDs = append(nodeIDs, n.ID)
	}

	// Initialize PageRank scores (uniform distribution)
	scores := make(map[uint64]float64)
	initialScore := 1.0 / float64(len(nodeIDs))
	for _, nodeID := range nodeIDs {
		scores[nodeID] = initialScore
	}

	// Get outgoing edge counts for each node
	outDegree := make(map[uint64]int)
	for _, nodeID := range nodeIDs {
		edges, err := view.OutgoingEdges(nodeID)
		if err == nil {
			outDegree[nodeID] = len(edges)
		}
	}

	// Iterative PageRank calculation
	newScores := make(map[uint64]float64)
	converged := false
	iterations := 0

	for iterations < opts.MaxIterations {
		iterations++

		for _, nodeID := range nodeIDs {
			newScore := (1.0 - opts.DampingFactor) / float64(len(nodeIDs))

			incomingEdges, err := view.IncomingEdges(nodeID)
			if err == nil {
				for _, edge := range incomingEdges {
					fromNode := edge.FromNodeID
					if outCount := outDegree[fromNode]; outCount > 0 {
						newScore += opts.DampingFactor * (scores[fromNode] / float64(outCount))
					}
				}
			}

			newScores[nodeID] = newScore
		}

		maxDiff := 0.0
		for nodeID := range scores {
			diff := math.Abs(newScores[nodeID] - scores[nodeID])
			if diff > maxDiff {
				maxDiff = diff
			}
		}

		if maxDiff < opts.Tolerance {
			converged = true
			break
		}

		scores, newScores = newScores, scores
	}

	// Normalize scores to sum to 1
	sum := 0.0
	for _, score := range scores {
		sum += score
	}
	if sum > 0 {
		for nodeID := range scores {
			scores[nodeID] /= sum
		}
	}

	topNodes := findTopNodesView(view, scores, 10)

	return &PageRankResult{
		Scores:     scores,
		Iterations: iterations,
		Converged:  converged,
		TopNodes:   topNodes,
	}, nil
}

// rankedNodeHeap implements a min-heap for RankedNode by score.
// We use a min-heap to efficiently find top N elements:
// - Keep at most N elements in the heap
// - The minimum element is at the root
// - When adding a new element, if heap is full and new > min, pop min and push new
// Time complexity: O(n log k) where n is total nodes and k is desired top count
type rankedNodeHeap []RankedNode

func (h rankedNodeHeap) Len() int           { return len(h) }
func (h rankedNodeHeap) Less(i, j int) bool { return h[i].Score < h[j].Score } // Min-heap
func (h rankedNodeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *rankedNodeHeap) Push(x any) {
	// heap.Interface.Push contract: callers always pass the heap's
	// element type. Mirrors rankedEdgeHeap.Push in centrality.go.
	n, ok := x.(RankedNode)
	if !ok {
		panic("rankedNodeHeap.Push: expected RankedNode")
	}
	*h = append(*h, n)
}

func (h *rankedNodeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// findTopNodes is the tenant-blind wrapper kept for callers that
// haven't yet been migrated to the graphView pattern (centrality,
// triangles). New callers should construct a graphView and use
// findTopNodesView directly.
func findTopNodes(graph storage.Storage, scores map[uint64]float64, n int) []RankedNode {
	return findTopNodesView(newTenantBlindView(graph), scores, n)
}

// findTopNodesView finds the top N nodes by score using a min-heap,
// fetching node data via the supplied graphView. The view-level
// indirection lets tenant-blind and tenant-scoped algorithm callers
// share this helper — see pkg/algorithms/view.go.
//
// Time complexity: O(n log k) where n = len(scores) and k = n
// Space complexity: O(k)
func findTopNodesView(view graphView, scores map[uint64]float64, n int) []RankedNode {
	if n <= 0 {
		return nil
	}

	h := make(rankedNodeHeap, 0, n)
	heap.Init(&h)

	for nodeID, score := range scores {
		node, err := view.Node(nodeID)
		if err != nil {
			continue
		}

		rn := RankedNode{
			NodeID: nodeID,
			Score:  score,
			Node:   node,
		}

		if h.Len() < n {
			heap.Push(&h, rn)
		} else if score > h[0].Score {
			heap.Pop(&h)
			heap.Push(&h, rn)
		}
	}

	result := make([]RankedNode, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		// The heap is populated only via rankedNodeHeap.Push above.
		n, ok := heap.Pop(&h).(RankedNode)
		if !ok {
			panic("findTopNodesView: heap returned non-RankedNode")
		}
		result[i] = n
	}

	return result
}

// GetTopNodesByPageRank returns top N nodes by PageRank score
func (pr *PageRankResult) GetTopNodesByPageRank(n int) []RankedNode {
	if n > len(pr.TopNodes) {
		return pr.TopNodes
	}
	return pr.TopNodes[:n]
}

// GetNodeRank returns the PageRank score for a specific node
func (pr *PageRankResult) GetNodeRank(nodeID uint64) float64 {
	return pr.Scores[nodeID]
}
