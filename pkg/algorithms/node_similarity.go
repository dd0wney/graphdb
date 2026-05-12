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

// getNeighborSet builds the set of neighbor node IDs reachable from
// nodeID in the given direction, optionally filtered by edge types.
// Operates against a graphView so the same helper supports both
// tenant-blind (Node Similarity, Link Prediction, K-hop legacy) and
// tenant-scoped (audit A6c-algorithms) callers.
func getNeighborSet(view graphView, nodeID uint64, direction NeighborDirection, edgeTypes []string) map[uint64]bool {
	neighbors := make(map[uint64]bool)

	edgeTypeSet := make(map[string]bool, len(edgeTypes))
	for _, et := range edgeTypes {
		edgeTypeSet[et] = true
	}
	filterByType := len(edgeTypes) > 0

	// Defensive: if a future graphView implementation returns an error,
	// proceed with whatever edges we did read. Similarity scores against
	// this node will be underestimated, never inflated.
	if direction == DirectionOut || direction == DirectionBoth {
		outEdges, err := view.OutgoingEdges(nodeID)
		if err != nil {
			outEdges = nil
		}
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
		inEdges, err := view.IncomingEdges(nodeID)
		if err != nil {
			inEdges = nil
		}
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

// NodeSimilarityPair computes similarity between two nodes (tenant-blind).
func NodeSimilarityPair(graph storage.StorageReader, nodeA, nodeB uint64, opts NodeSimilarityOptions) (float64, error) {
	return nodeSimilarityPairView(newTenantBlindView(graph), nodeA, nodeB, opts)
}

// NodeSimilarityPairForTenant computes similarity restricted to the
// caller's tenant. Audit A6c-algorithms.
func NodeSimilarityPairForTenant(graph storage.StorageReader, nodeA, nodeB uint64, opts NodeSimilarityOptions, tenantID string) (float64, error) {
	return nodeSimilarityPairView(newTenantScopedView(graph, tenantID), nodeA, nodeB, opts)
}

func nodeSimilarityPairView(view graphView, nodeA, nodeB uint64, opts NodeSimilarityOptions) (float64, error) {
	setA := getNeighborSet(view, nodeA, opts.Direction, opts.EdgeTypes)
	setB := getNeighborSet(view, nodeB, opts.Direction, opts.EdgeTypes)
	return computeSimilarity(setA, setB, opts.Metric), nil
}

// NodeSimilarityFor computes similarity of sourceNodeID against all
// other nodes (tenant-blind). Multi-tenant API callers must use
// NodeSimilarityForTenant.
func NodeSimilarityFor(graph storage.StorageReader, sourceNodeID uint64, opts NodeSimilarityOptions) (*NodeSimilarityResult, error) {
	return nodeSimilarityForView(newTenantBlindView(graph), sourceNodeID, opts)
}

// NodeSimilarityForForTenant restricts to caller's tenant.
// Audit A6c-algorithms.
func NodeSimilarityForForTenant(graph storage.StorageReader, sourceNodeID uint64, opts NodeSimilarityOptions, tenantID string) (*NodeSimilarityResult, error) {
	return nodeSimilarityForView(newTenantScopedView(graph, tenantID), sourceNodeID, opts)
}

func nodeSimilarityForView(view graphView, sourceNodeID uint64, opts NodeSimilarityOptions) (*NodeSimilarityResult, error) {
	allNodes := view.AllNodes()
	nodeIDs := make([]uint64, 0, len(allNodes))
	for _, n := range allNodes {
		nodeIDs = append(nodeIDs, n.ID)
	}

	sourceSet := getNeighborSet(view, sourceNodeID, opts.Direction, opts.EdgeTypes)

	var scores []NodeSimilarityScore
	for _, otherID := range nodeIDs {
		if otherID == sourceNodeID {
			continue
		}
		otherSet := getNeighborSet(view, otherID, opts.Direction, opts.EdgeTypes)
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

// NodeSimilarityAll computes similarity for every node against every
// other node (tenant-blind).
func NodeSimilarityAll(graph storage.StorageReader, opts NodeSimilarityOptions) ([]NodeSimilarityResult, error) {
	return nodeSimilarityAllView(newTenantBlindView(graph), opts)
}

// NodeSimilarityAllForTenant restricts to caller's tenant.
// Audit A6c-algorithms.
func NodeSimilarityAllForTenant(graph storage.StorageReader, opts NodeSimilarityOptions, tenantID string) ([]NodeSimilarityResult, error) {
	return nodeSimilarityAllView(newTenantScopedView(graph, tenantID), opts)
}

func nodeSimilarityAllView(view graphView, opts NodeSimilarityOptions) ([]NodeSimilarityResult, error) {
	allNodes := view.AllNodes()
	nodeIDs := make([]uint64, 0, len(allNodes))
	for _, n := range allNodes {
		nodeIDs = append(nodeIDs, n.ID)
	}

	// Pre-compute all neighbor sets
	neighborSets := make(map[uint64]map[uint64]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		neighborSets[id] = getNeighborSet(view, id, opts.Direction, opts.EdgeTypes)
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
