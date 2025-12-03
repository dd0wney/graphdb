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
func PageRank(graph *storage.GraphStorage, opts PageRankOptions) (*PageRankResult, error) {
	// Get all nodes
	stats := graph.GetStatistics()

	// Use uint64 directly to avoid overflow on 32-bit systems
	if stats.NodeCount == 0 {
		return &PageRankResult{
			Scores:    make(map[uint64]float64),
			Converged: true,
		}, nil
	}

	// Get all node IDs (need to iterate through graph)
	// For now, assume sequential IDs from 1 to nodeCount
	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
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
		edges, err := graph.GetOutgoingEdges(nodeID)
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

		// Calculate new scores
		for _, nodeID := range nodeIDs {
			// Start with random jump probability
			newScore := (1.0 - opts.DampingFactor) / float64(len(nodeIDs))

			// Add contributions from incoming edges
			incomingEdges, err := graph.GetIncomingEdges(nodeID)
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

		// Check for convergence
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

		// Swap scores
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

	// Find top nodes
	topNodes := findTopNodes(graph, scores, 10)

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
	*h = append(*h, x.(RankedNode))
}

func (h *rankedNodeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// findTopNodes finds the top N nodes by score using a min-heap.
// Time complexity: O(n log k) where n = len(scores) and k = n
// Space complexity: O(k)
func findTopNodes(graph *storage.GraphStorage, scores map[uint64]float64, n int) []RankedNode {
	if n <= 0 {
		return nil
	}

	// Use a min-heap to track top N elements
	// By using a min-heap, we can efficiently discard elements smaller than the minimum
	h := make(rankedNodeHeap, 0, n)
	heap.Init(&h)

	for nodeID, score := range scores {
		node, err := graph.GetNode(nodeID)
		if err != nil {
			continue
		}

		rn := RankedNode{
			NodeID: nodeID,
			Score:  score,
			Node:   node,
		}

		if h.Len() < n {
			// Heap not full yet, just add
			heap.Push(&h, rn)
		} else if score > h[0].Score {
			// New element is larger than current minimum, replace
			heap.Pop(&h)
			heap.Push(&h, rn)
		}
		// Otherwise, this element is smaller than all top N, skip it
	}

	// Extract elements from heap (will be in ascending order)
	result := make([]RankedNode, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		result[i] = heap.Pop(&h).(RankedNode)
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
