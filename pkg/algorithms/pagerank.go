package algorithms

import (
	"math"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
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
	Scores      map[uint64]float64 // Node ID -> PageRank score
	Iterations  int                // Number of iterations performed
	Converged   bool               // Whether algorithm converged
	TopNodes    []RankedNode       // Top N nodes by score
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

// findTopNodes finds the top N nodes by score
func findTopNodes(graph *storage.GraphStorage, scores map[uint64]float64, n int) []RankedNode {
	// Convert to slice for sorting
	ranked := make([]RankedNode, 0, len(scores))
	for nodeID, score := range scores {
		node, err := graph.GetNode(nodeID)
		if err == nil {
			ranked = append(ranked, RankedNode{
				NodeID: nodeID,
				Score:  score,
				Node:   node,
			})
		}
	}

	// Simple selection sort for top N
	if n > len(ranked) {
		n = len(ranked)
	}

	for i := 0; i < n; i++ {
		maxIdx := i
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].Score > ranked[maxIdx].Score {
				maxIdx = j
			}
		}
		ranked[i], ranked[maxIdx] = ranked[maxIdx], ranked[i]
	}

	return ranked[:n]
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
