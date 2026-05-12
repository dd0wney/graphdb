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

// PredictLinkScore computes the link prediction score between two
// specific nodes (tenant-blind).
func PredictLinkScore(graph storage.StorageReader, fromNodeID, toNodeID uint64, opts LinkPredictionOptions) (float64, error) {
	return predictLinkScoreView(newTenantBlindView(graph), fromNodeID, toNodeID, opts)
}

// PredictLinkScoreForTenant restricts to the caller's tenant.
// Audit A6c-algorithms.
func PredictLinkScoreForTenant(graph storage.StorageReader, fromNodeID, toNodeID uint64, opts LinkPredictionOptions, tenantID string) (float64, error) {
	return predictLinkScoreView(newTenantScopedView(graph, tenantID), fromNodeID, toNodeID, opts)
}

func predictLinkScoreView(view graphView, fromNodeID, toNodeID uint64, opts LinkPredictionOptions) (float64, error) {
	setFrom := getNeighborSet(view, fromNodeID, opts.Direction, opts.EdgeTypes)
	setTo := getNeighborSet(view, toNodeID, opts.Direction, opts.EdgeTypes)
	return computeLinkScore(view, setFrom, setTo, opts), nil
}

// PredictLinksFor predicts links for a source node against all other
// nodes (tenant-blind).
func PredictLinksFor(graph storage.StorageReader, sourceNodeID uint64, opts LinkPredictionOptions) (*LinkPredictionResult, error) {
	return predictLinksForView(newTenantBlindView(graph), sourceNodeID, opts)
}

// PredictLinksForForTenant restricts to caller's tenant.
// Audit A6c-algorithms.
func PredictLinksForForTenant(graph storage.StorageReader, sourceNodeID uint64, opts LinkPredictionOptions, tenantID string) (*LinkPredictionResult, error) {
	return predictLinksForView(newTenantScopedView(graph, tenantID), sourceNodeID, opts)
}

func predictLinksForView(view graphView, sourceNodeID uint64, opts LinkPredictionOptions) (*LinkPredictionResult, error) {
	allNodes := view.AllNodes()
	nodeIDs := make([]uint64, 0, len(allNodes))
	for _, n := range allNodes {
		nodeIDs = append(nodeIDs, n.ID)
	}

	sourceSet := getNeighborSet(view, sourceNodeID, opts.Direction, opts.EdgeTypes)

	// Build set of existing neighbors for exclusion check
	var existingNeighbors map[uint64]bool
	if opts.ExcludeExisting {
		existingNeighbors = getNeighborSet(view, sourceNodeID, DirectionBoth, nil)
	}

	var predictions []LinkPrediction
	for _, otherID := range nodeIDs {
		if otherID == sourceNodeID {
			continue
		}
		if opts.ExcludeExisting && existingNeighbors[otherID] {
			continue
		}

		otherSet := getNeighborSet(view, otherID, opts.Direction, opts.EdgeTypes)
		score := computeLinkScore(view, sourceSet, otherSet, opts)
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
func computeLinkScore(view graphView, setA, setB map[uint64]bool, opts LinkPredictionOptions) float64 {
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
				degree := len(getNeighborSet(view, id, opts.Direction, opts.EdgeTypes))
				if degree > 1 {
					sum += 1.0 / math.Log(float64(degree))
				}
			}
		}
		return sum

	default:
		return 0.0
	}
}
