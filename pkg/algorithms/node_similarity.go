package algorithms

import (
	"math"
	"sort"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// NeighborDirection controls which edges to follow when building neighbor sets.
type NeighborDirection int

const (
	DirectionOut  NeighborDirection = iota // outgoing edges only
	DirectionIn                            // incoming edges only
	DirectionBoth                          // union of both
)

// SimilarityMetric selects which similarity formula to use.
type SimilarityMetric int

const (
	SimilarityJaccard SimilarityMetric = iota // |A∩B| / |A∪B|
	SimilarityOverlap                         // |A∩B| / min(|A|,|B|)
	SimilarityCosine                          // |A∩B| / sqrt(|A|×|B|)
)

// NodeSimilarityOptions configures the node similarity computation.
type NodeSimilarityOptions struct {
	Metric    SimilarityMetric
	Direction NeighborDirection
	EdgeTypes []string // nil means all edge types
	TopK      int      // max results per node (0 = all)
}

// NodeSimilarityScore holds a similarity score between two nodes.
type NodeSimilarityScore struct {
	NodeA uint64
	NodeB uint64
	Score float64
}

// NodeSimilarityResult holds similarity results for a single source node.
type NodeSimilarityResult struct {
	SourceNodeID uint64
	Similar      []NodeSimilarityScore // sorted desc by Score, zeros excluded
}

// DefaultNodeSimilarityOptions returns sensible defaults.
func DefaultNodeSimilarityOptions() NodeSimilarityOptions {
	return NodeSimilarityOptions{
		Metric:    SimilarityJaccard,
		Direction: DirectionOut,
		TopK:      10,
	}
}

// getNeighborSet builds the set of neighbor node IDs reachable from nodeID
// in the given direction, optionally filtered by edge types.
// Excludes self-loops. Used by Node Similarity, Link Prediction, and K-hop.
func getNeighborSet(graph *storage.GraphStorage, nodeID uint64, direction NeighborDirection, edgeTypes []string) map[uint64]bool {
	neighbors := make(map[uint64]bool)

	edgeTypeSet := make(map[string]bool, len(edgeTypes))
	for _, et := range edgeTypes {
		edgeTypeSet[et] = true
	}
	filterByType := len(edgeTypes) > 0

	if direction == DirectionOut || direction == DirectionBoth {
		outEdges, _ := graph.GetOutgoingEdges(nodeID)
		for _, e := range outEdges {
			if filterByType && !edgeTypeSet[e.Type] {
				continue
			}
			if e.ToNodeID != nodeID {
				neighbors[e.ToNodeID] = true
			}
		}
	}

	if direction == DirectionIn || direction == DirectionBoth {
		inEdges, _ := graph.GetIncomingEdges(nodeID)
		for _, e := range inEdges {
			if filterByType && !edgeTypeSet[e.Type] {
				continue
			}
			if e.FromNodeID != nodeID {
				neighbors[e.FromNodeID] = true
			}
		}
	}

	return neighbors
}

// computeSimilarity calculates the similarity between two neighbor sets.
func computeSimilarity(setA, setB map[uint64]bool, metric SimilarityMetric) float64 {
	if len(setA) == 0 || len(setB) == 0 {
		return 0.0
	}

	// Count intersection
	intersection := 0
	// Iterate over the smaller set for efficiency
	small, big := setA, setB
	if len(setA) > len(setB) {
		small, big = setB, setA
	}
	for id := range small {
		if big[id] {
			intersection++
		}
	}

	if intersection == 0 {
		return 0.0
	}

	switch metric {
	case SimilarityJaccard:
		union := len(setA) + len(setB) - intersection
		return float64(intersection) / float64(union)
	case SimilarityOverlap:
		minSize := len(setA)
		if len(setB) < minSize {
			minSize = len(setB)
		}
		return float64(intersection) / float64(minSize)
	case SimilarityCosine:
		return float64(intersection) / math.Sqrt(float64(len(setA))*float64(len(setB)))
	default:
		return 0.0
	}
}

// NodeSimilarityPair computes the similarity between two specific nodes.
func NodeSimilarityPair(graph *storage.GraphStorage, nodeA, nodeB uint64, opts NodeSimilarityOptions) (float64, error) {
	setA := getNeighborSet(graph, nodeA, opts.Direction, opts.EdgeTypes)
	setB := getNeighborSet(graph, nodeB, opts.Direction, opts.EdgeTypes)
	return computeSimilarity(setA, setB, opts.Metric), nil
}

// NodeSimilarityFor computes similarity of sourceNodeID against all other nodes.
// Results are sorted descending by score; zero-score pairs are excluded.
func NodeSimilarityFor(graph *storage.GraphStorage, sourceNodeID uint64, opts NodeSimilarityOptions) (*NodeSimilarityResult, error) {
	stats := graph.GetStatistics()

	nodeIDs := make([]uint64, 0, stats.NodeCount)
	maxID := stats.NodeCount + 10
	if maxID < 100 {
		maxID = 100
	}
	for i := uint64(1); i <= maxID; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
		if uint64(len(nodeIDs)) >= stats.NodeCount && stats.NodeCount > 0 {
			break
		}
	}

	sourceSet := getNeighborSet(graph, sourceNodeID, opts.Direction, opts.EdgeTypes)

	var scores []NodeSimilarityScore
	for _, otherID := range nodeIDs {
		if otherID == sourceNodeID {
			continue
		}
		otherSet := getNeighborSet(graph, otherID, opts.Direction, opts.EdgeTypes)
		score := computeSimilarity(sourceSet, otherSet, opts.Metric)
		if score > 0 {
			scores = append(scores, NodeSimilarityScore{
				NodeA: sourceNodeID,
				NodeB: otherID,
				Score: score,
			})
		}
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	if opts.TopK > 0 && len(scores) > opts.TopK {
		scores = scores[:opts.TopK]
	}

	return &NodeSimilarityResult{
		SourceNodeID: sourceNodeID,
		Similar:      scores,
	}, nil
}

// NodeSimilarityAll computes similarity for every node against every other node.
// Returns one result per node, each with up to TopK similar nodes.
func NodeSimilarityAll(graph *storage.GraphStorage, opts NodeSimilarityOptions) ([]NodeSimilarityResult, error) {
	stats := graph.GetStatistics()

	nodeIDs := make([]uint64, 0, stats.NodeCount)
	maxID := stats.NodeCount + 10
	if maxID < 100 {
		maxID = 100
	}
	for i := uint64(1); i <= maxID; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
		if uint64(len(nodeIDs)) >= stats.NodeCount && stats.NodeCount > 0 {
			break
		}
	}

	// Pre-compute all neighbor sets
	neighborSets := make(map[uint64]map[uint64]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		neighborSets[id] = getNeighborSet(graph, id, opts.Direction, opts.EdgeTypes)
	}

	results := make([]NodeSimilarityResult, 0, len(nodeIDs))
	for _, sourceID := range nodeIDs {
		var scores []NodeSimilarityScore
		for _, otherID := range nodeIDs {
			if otherID == sourceID {
				continue
			}
			score := computeSimilarity(neighborSets[sourceID], neighborSets[otherID], opts.Metric)
			if score > 0 {
				scores = append(scores, NodeSimilarityScore{
					NodeA: sourceID,
					NodeB: otherID,
					Score: score,
				})
			}
		}

		sort.Slice(scores, func(i, j int) bool {
			return scores[i].Score > scores[j].Score
		})
		if opts.TopK > 0 && len(scores) > opts.TopK {
			scores = scores[:opts.TopK]
		}

		results = append(results, NodeSimilarityResult{
			SourceNodeID: sourceID,
			Similar:      scores,
		})
	}

	return results, nil
}
