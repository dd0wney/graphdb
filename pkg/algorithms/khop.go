package algorithms

import (
	"fmt"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// KHopOptions configures the k-hop neighbourhood traversal.
type KHopOptions struct {
	MaxHops    int // must be >= 1
	Direction  NeighborDirection
	EdgeTypes  []string // nil means all edge types
	MaxResults int      // 0 = unlimited; BFS order gives closer nodes priority
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
// returning all discovered nodes grouped by distance. Tenant-blind —
// runs across every tenant. Multi-tenant API callers must use
// KHopNeighboursForTenant. The source node is never included in results.
func KHopNeighbours(graph storage.Storage, sourceNodeID uint64, opts KHopOptions) (*KHopResult, error) {
	return kHopNeighboursView(newTenantBlindView(graph), sourceNodeID, opts)
}

// KHopNeighboursForTenant performs the same BFS as KHopNeighbours but
// restricted to the caller's tenant subgraph. Audit A6c-algorithms:
// expansion uses the tenant-scoped *ForTenant edge accessors, so
// foreign-tenant node IDs never appear in ByHop / Distances. Pairs
// with the tenant-strict fix to the /algorithms khop handler.
func KHopNeighboursForTenant(graph storage.Storage, sourceNodeID uint64, opts KHopOptions, tenantID string) (*KHopResult, error) {
	return kHopNeighboursView(newTenantScopedView(graph, tenantID), sourceNodeID, opts)
}

// kHopNeighboursView is the shared algorithm body operating against
// a graphView — see pkg/algorithms/view.go for the abstraction that
// lets the tenant-blind and tenant-scoped public functions share one
// implementation.
func kHopNeighboursView(view graphView, sourceNodeID uint64, opts KHopOptions) (*KHopResult, error) {
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

		// Defensive: if a future graphView implementation returns an
		// error, proceed with whatever edges we did read. The BFS will
		// under-cover, never over-cover.
		if opts.Direction == DirectionOut || opts.Direction == DirectionBoth {
			outEdges, err := view.OutgoingEdges(current.nodeID)
			if err != nil {
				outEdges = nil
			}
			for _, e := range outEdges {
				if filterByType && !edgeTypeSet[e.Type] {
					continue
				}
				neighborIDs = append(neighborIDs, e.ToNodeID)
			}
		}

		if opts.Direction == DirectionIn || opts.Direction == DirectionBoth {
			inEdges, err := view.IncomingEdges(current.nodeID)
			if err != nil {
				inEdges = nil
			}
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
