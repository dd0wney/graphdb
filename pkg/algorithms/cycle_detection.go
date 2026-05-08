package algorithms

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Cycle represents a detected cycle as a sequence of node IDs
type Cycle []uint64

// DetectCycles finds all cycles in the graph using DFS with
// three-color marking. Tenant-blind. Multi-tenant API callers must
// use DetectCyclesForTenant.
//
// Algorithm: depth-first search with three colors:
//   - WHITE (0): Unvisited node
//   - GRAY (1): Currently visiting (in recursion stack)
//   - BLACK (2): Finished visiting (all descendants explored)
//
// A GRAY node found during DFS is a back edge, indicating a cycle.
func DetectCycles(graph *storage.GraphStorage) ([]Cycle, error) {
	return detectCyclesView(newTenantBlindView(graph))
}

// DetectCyclesForTenant finds cycles within the caller's tenant
// subgraph. Audit A6c-algorithms (2026-05-08).
func DetectCyclesForTenant(graph *storage.GraphStorage, tenantID string) ([]Cycle, error) {
	return detectCyclesView(newTenantScopedView(graph, tenantID))
}

func detectCyclesView(view graphView) ([]Cycle, error) {
	const (
		WHITE = 0
		GRAY  = 1
		BLACK = 2
	)
	_ = GRAY
	_ = BLACK

	color := make(map[uint64]int)
	parent := make(map[uint64]uint64)
	cycles := make([]Cycle, 0)

	allNodes := view.AllNodes()
	if len(allNodes) == 0 {
		return cycles, nil
	}

	for _, n := range allNodes {
		if color[n.ID] == WHITE {
			dfsDetectCycleView(view, n.ID, color, parent, &cycles)
		}
	}

	return cycles, nil
}

func dfsDetectCycleView(
	view graphView,
	nodeID uint64,
	color map[uint64]int,
	parent map[uint64]uint64,
	cycles *[]Cycle,
) {
	const (
		WHITE = 0
		GRAY  = 1
		BLACK = 2
	)

	color[nodeID] = GRAY

	edges, err := view.OutgoingEdges(nodeID)
	if err != nil {
		color[nodeID] = BLACK
		return
	}

	for _, edge := range edges {
		neighborID := edge.ToNodeID

		// Self-loop detected
		if neighborID == nodeID {
			*cycles = append(*cycles, Cycle{nodeID})
			continue
		}

		if color[neighborID] == WHITE {
			parent[neighborID] = nodeID
			dfsDetectCycleView(view, neighborID, color, parent, cycles)
		} else if color[neighborID] == GRAY {
			cycle := extractCycle(neighborID, nodeID, parent)
			*cycles = append(*cycles, cycle)
		}
	}

	color[nodeID] = BLACK
}

// extractCycle reconstructs the cycle from parent pointers
// Given a back edge from 'end' to 'start', we trace back from 'end' to 'start' using parent pointers
func extractCycle(start, end uint64, parent map[uint64]uint64) Cycle {
	cycle := make(Cycle, 0)
	cycle = append(cycle, start)

	current := end
	for current != start {
		cycle = append(cycle, current)
		if p, exists := parent[current]; exists {
			current = p
		} else {
			// Safety: shouldn't happen if algorithm is correct
			break
		}
	}

	return cycle
}

// CycleDetectionOptions configures cycle detection behavior
type CycleDetectionOptions struct {
	MinCycleLength int                      // Minimum cycle length to report (0 = all)
	MaxCycleLength int                      // Maximum cycle length to report (0 = unlimited)
	NodePredicate  func(*storage.Node) bool // Only include cycles with nodes matching predicate
	EdgeTypes      []string                 // Only follow edges of these types (empty = all types)
}

// DetectCyclesWithOptions finds cycles matching the given criteria
// (tenant-blind).
func DetectCyclesWithOptions(graph *storage.GraphStorage, opts CycleDetectionOptions) ([]Cycle, error) {
	return detectCyclesWithOptionsView(newTenantBlindView(graph), opts)
}

// DetectCyclesWithOptionsForTenant restricts the cycle search and
// filtering to the caller's tenant. Audit A6c-algorithms.
func DetectCyclesWithOptionsForTenant(graph *storage.GraphStorage, opts CycleDetectionOptions, tenantID string) ([]Cycle, error) {
	return detectCyclesWithOptionsView(newTenantScopedView(graph, tenantID), opts)
}

func detectCyclesWithOptionsView(view graphView, opts CycleDetectionOptions) ([]Cycle, error) {
	allCycles, err := detectCyclesView(view)
	if err != nil {
		return nil, err
	}

	filtered := make([]Cycle, 0)
	for _, cycle := range allCycles {
		// Filter by length
		if opts.MinCycleLength > 0 && len(cycle) < opts.MinCycleLength {
			continue
		}
		if opts.MaxCycleLength > 0 && len(cycle) > opts.MaxCycleLength {
			continue
		}

		// Filter by node predicate
		if opts.NodePredicate != nil {
			allMatch := true
			for _, nodeID := range cycle {
				node, err := view.Node(nodeID)
				if err != nil || !opts.NodePredicate(node) {
					allMatch = false
					break
				}
			}
			if !allMatch {
				continue
			}
		}

		filtered = append(filtered, cycle)
	}

	return filtered, nil
}

// CycleStats provides statistics about detected cycles
type CycleStats struct {
	TotalCycles   int
	ShortestCycle int
	LongestCycle  int
	AverageLength float64
	SelfLoops     int // Number of self-referencing nodes
}

// AnalyzeCycles computes statistics about detected cycles
func AnalyzeCycles(cycles []Cycle) CycleStats {
	if len(cycles) == 0 {
		return CycleStats{}
	}

	stats := CycleStats{
		TotalCycles:   len(cycles),
		ShortestCycle: len(cycles[0]),
		LongestCycle:  len(cycles[0]),
	}

	totalLength := 0
	for _, cycle := range cycles {
		length := len(cycle)
		totalLength += length

		if length == 1 {
			stats.SelfLoops++
		}

		if length < stats.ShortestCycle {
			stats.ShortestCycle = length
		}
		if length > stats.LongestCycle {
			stats.LongestCycle = length
		}
	}

	stats.AverageLength = float64(totalLength) / float64(len(cycles))
	return stats
}

// HasCycle checks if the graph contains any cycles (tenant-blind,
// faster than detecting all cycles).
func HasCycle(graph *storage.GraphStorage) (bool, error) {
	return hasCycleView(newTenantBlindView(graph))
}

// HasCycleForTenant checks if the caller's tenant subgraph contains
// any cycles. Audit A6c-algorithms.
func HasCycleForTenant(graph *storage.GraphStorage, tenantID string) (bool, error) {
	return hasCycleView(newTenantScopedView(graph, tenantID))
}

func hasCycleView(view graphView) (bool, error) {
	const WHITE = 0

	color := make(map[uint64]int)
	allNodes := view.AllNodes()
	if len(allNodes) == 0 {
		return false, nil
	}

	for _, n := range allNodes {
		if color[n.ID] == WHITE {
			if hasCycleDFSView(view, n.ID, color) {
				return true, nil
			}
		}
	}

	return false, nil
}

// hasCycleDFSView performs DFS to check for cycles (returns true on first cycle found)
func hasCycleDFSView(view graphView, nodeID uint64, color map[uint64]int) bool {
	const (
		WHITE = 0
		GRAY  = 1
		BLACK = 2
	)

	color[nodeID] = GRAY

	edges, err := view.OutgoingEdges(nodeID)
	if err != nil {
		color[nodeID] = BLACK
		return false
	}

	for _, edge := range edges {
		neighborID := edge.ToNodeID

		// Self-loop is a cycle
		if neighborID == nodeID {
			return true
		}

		if color[neighborID] == WHITE {
			if hasCycleDFSView(view, neighborID, color) {
				return true
			}
		} else if color[neighborID] == GRAY {
			return true
		}
	}

	color[nodeID] = BLACK
	return false
}
