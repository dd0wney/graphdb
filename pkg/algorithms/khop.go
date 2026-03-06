package algorithms

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// KHopOptions configures the k-hop neighbourhood traversal.
type KHopOptions struct {
	MaxHops    int               // must be >= 1
	Direction  NeighborDirection
	EdgeTypes  []string          // nil means all edge types
	MaxResults int               // 0 = unlimited; BFS order gives closer nodes priority
}

// KHopResult holds the BFS neighbourhood of a source node.
type KHopResult struct {
	SourceNodeID   uint64
	ByHop          map[int][]uint64 // hop distance → node IDs at that distance
	Distances      map[uint64]int   // node ID → shortest hop count
	TotalReachable int
}

// DefaultKHopOptions returns sensible defaults.
func DefaultKHopOptions() KHopOptions {
	return KHopOptions{
		MaxHops:   2,
		Direction: DirectionOut,
	}
}

type bfsEntry struct {
	nodeID uint64
	hop    int
}

// KHopNeighbours performs a BFS from sourceNodeID up to MaxHops levels,
// returning all discovered nodes grouped by distance.
// The source node is never included in results.
func KHopNeighbours(graph *storage.GraphStorage, sourceNodeID uint64, opts KHopOptions) (*KHopResult, error) {
	if opts.MaxHops < 1 {
		return nil, fmt.Errorf("MaxHops must be >= 1, got %d", opts.MaxHops)
	}

	edgeTypeSet := make(map[string]bool, len(opts.EdgeTypes))
	for _, et := range opts.EdgeTypes {
		edgeTypeSet[et] = true
	}
	filterByType := len(opts.EdgeTypes) > 0

	visited := map[uint64]bool{sourceNodeID: true}
	distances := make(map[uint64]int)
	byHop := make(map[int][]uint64)
	totalReachable := 0

	queue := []bfsEntry{{nodeID: sourceNodeID, hop: 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.hop >= opts.MaxHops {
			continue
		}

		nextHop := current.hop + 1

		// Collect neighbors based on direction
		var neighborIDs []uint64

		if opts.Direction == DirectionOut || opts.Direction == DirectionBoth {
			outEdges, _ := graph.GetOutgoingEdges(current.nodeID)
			for _, e := range outEdges {
				if filterByType && !edgeTypeSet[e.Type] {
					continue
				}
				neighborIDs = append(neighborIDs, e.ToNodeID)
			}
		}

		if opts.Direction == DirectionIn || opts.Direction == DirectionBoth {
			inEdges, _ := graph.GetIncomingEdges(current.nodeID)
			for _, e := range inEdges {
				if filterByType && !edgeTypeSet[e.Type] {
					continue
				}
				neighborIDs = append(neighborIDs, e.FromNodeID)
			}
		}

		for _, neighborID := range neighborIDs {
			if visited[neighborID] {
				continue
			}
			visited[neighborID] = true
			distances[neighborID] = nextHop
			byHop[nextHop] = append(byHop[nextHop], neighborID)
			totalReachable++

			if opts.MaxResults > 0 && totalReachable >= opts.MaxResults {
				return &KHopResult{
					SourceNodeID:   sourceNodeID,
					ByHop:          byHop,
					Distances:      distances,
					TotalReachable: totalReachable,
				}, nil
			}

			queue = append(queue, bfsEntry{nodeID: neighborID, hop: nextHop})
		}
	}

	return &KHopResult{
		SourceNodeID:   sourceNodeID,
		ByHop:          byHop,
		Distances:      distances,
		TotalReachable: totalReachable,
	}, nil
}
