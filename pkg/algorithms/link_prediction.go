package algorithms

import (
	"math"
	"sort"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// LinkPredictionMethod selects the scoring formula for link prediction.
type LinkPredictionMethod int

const (
	// LinkPredCommonNeighbours scores by |N(u) ∩ N(v)| — integer counts.
	LinkPredCommonNeighbours LinkPredictionMethod = iota

	// LinkPredAdamicAdar scores by Σ_{w ∈ N(u)∩N(v)} 1/log(|N(w)|) — weighted sum
	// giving higher weight to common neighbors with fewer connections.
	LinkPredAdamicAdar

	// LinkPredPreferentialAttachment scores by |N(u)| × |N(v)| — degree product.
	// Requires no intersection computation.
	LinkPredPreferentialAttachment
)

// LinkPredictionOptions configures link prediction.
//
// Scores across different methods are not comparable. Common Neighbours returns
// integer counts, Adamic-Adar returns weighted sums, and Preferential Attachment
// returns degree products.
type LinkPredictionOptions struct {
	Method          LinkPredictionMethod
	Direction       NeighborDirection
	EdgeTypes       []string
	ExcludeExisting bool // default true — skip pairs that already share an edge
	TopK            int  // default 10, 0 = all
}

// LinkPrediction holds a predicted link score between two nodes.
type LinkPrediction struct {
	FromNodeID uint64
	ToNodeID   uint64
	Score      float64
}

// LinkPredictionResult holds predictions for a single source node.
type LinkPredictionResult struct {
	SourceNodeID uint64
	Predictions  []LinkPrediction // sorted desc by Score
}

// DefaultLinkPredictionOptions returns sensible defaults.
func DefaultLinkPredictionOptions() LinkPredictionOptions {
	return LinkPredictionOptions{
		Method:          LinkPredCommonNeighbours,
		Direction:       DirectionOut,
		ExcludeExisting: true,
		TopK:            10,
	}
}

// PredictLinkScore computes the link prediction score between two specific nodes.
func PredictLinkScore(graph *storage.GraphStorage, fromNodeID, toNodeID uint64, opts LinkPredictionOptions) (float64, error) {
	setFrom := getNeighborSet(graph, fromNodeID, opts.Direction, opts.EdgeTypes)
	setTo := getNeighborSet(graph, toNodeID, opts.Direction, opts.EdgeTypes)

	return computeLinkScore(graph, setFrom, setTo, opts), nil
}

// PredictLinksFor predicts links for a source node against all other nodes.
// Results are sorted descending by score; zero-score pairs are excluded.
func PredictLinksFor(graph *storage.GraphStorage, sourceNodeID uint64, opts LinkPredictionOptions) (*LinkPredictionResult, error) {
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

	// Build set of existing neighbors for exclusion check
	var existingNeighbors map[uint64]bool
	if opts.ExcludeExisting {
		existingNeighbors = getNeighborSet(graph, sourceNodeID, DirectionBoth, nil)
	}

	var predictions []LinkPrediction
	for _, otherID := range nodeIDs {
		if otherID == sourceNodeID {
			continue
		}
		if opts.ExcludeExisting && existingNeighbors[otherID] {
			continue
		}

		otherSet := getNeighborSet(graph, otherID, opts.Direction, opts.EdgeTypes)
		score := computeLinkScore(graph, sourceSet, otherSet, opts)
		if score > 0 {
			predictions = append(predictions, LinkPrediction{
				FromNodeID: sourceNodeID,
				ToNodeID:   otherID,
				Score:      score,
			})
		}
	}

	sort.Slice(predictions, func(i, j int) bool {
		return predictions[i].Score > predictions[j].Score
	})
	if opts.TopK > 0 && len(predictions) > opts.TopK {
		predictions = predictions[:opts.TopK]
	}

	return &LinkPredictionResult{
		SourceNodeID: sourceNodeID,
		Predictions:  predictions,
	}, nil
}

// computeLinkScore calculates the prediction score for a pair of neighbor sets.
func computeLinkScore(graph *storage.GraphStorage, setA, setB map[uint64]bool, opts LinkPredictionOptions) float64 {
	switch opts.Method {
	case LinkPredPreferentialAttachment:
		return float64(len(setA)) * float64(len(setB))

	case LinkPredCommonNeighbours:
		count := 0
		small, big := setA, setB
		if len(setA) > len(setB) {
			small, big = setB, setA
		}
		for id := range small {
			if big[id] {
				count++
			}
		}
		return float64(count)

	case LinkPredAdamicAdar:
		sum := 0.0
		small, big := setA, setB
		if len(setA) > len(setB) {
			small, big = setB, setA
		}
		for id := range small {
			if big[id] {
				// Degree of the common neighbor
				degree := len(getNeighborSet(graph, id, opts.Direction, opts.EdgeTypes))
				if degree > 1 {
					sum += 1.0 / math.Log(float64(degree))
				}
				// degree <= 1: skip (log(1)=0 causes division by zero, log(<1) is negative)
			}
		}
		return sum

	default:
		return 0.0
	}
}
