package algorithms

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Cycle represents a detected cycle as a sequence of node IDs
type Cycle []uint64

// DetectCycles finds all cycles in the graph using DFS with three-color marking
// Returns a slice of cycles, where each cycle is a slice of node IDs
//
// Algorithm: Uses depth-first search with three colors:
//   - WHITE (0): Unvisited node
//   - GRAY (1): Currently visiting (node is in the recursion stack)
//   - BLACK (2): Finished visiting (all descendants have been explored)
//
// When we encounter a GRAY node during DFS, we've found a back edge, which indicates a cycle.
func DetectCycles(graph *storage.GraphStorage) ([]Cycle, error) {
	const (
		WHITE = 0 // Unvisited
		GRAY  = 1 // Currently visiting (in recursion stack)
		BLACK = 2 // Finished visiting
	)

	color := make(map[uint64]int)
	parent := make(map[uint64]uint64)
	cycles := make([]Cycle, 0)

	// Get all node IDs by iterating through possible IDs
	stats := graph.GetStatistics()
	if stats.NodeCount == 0 {
		return cycles, nil
	}

	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	// DFS from each unvisited node to ensure we cover disconnected components
	for _, nodeID := range nodeIDs {
		if color[nodeID] == WHITE {
			dfsDetectCycle(graph, nodeID, color, parent, &cycles)
		}
	}

	return cycles, nil
}

// dfsDetectCycle performs DFS to detect cycles
func dfsDetectCycle(
	graph *storage.GraphStorage,
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

	// Get outgoing edges
	edges, err := graph.GetOutgoingEdges(nodeID)
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
			// Tree edge - continue DFS
			parent[neighborID] = nodeID
			dfsDetectCycle(graph, neighborID, color, parent, cycles)
		} else if color[neighborID] == GRAY {
			// Back edge found - cycle detected!
			cycle := extractCycle(neighborID, nodeID, parent)
			*cycles = append(*cycles, cycle)
		}
		// If BLACK, it's a forward/cross edge - no cycle from this edge
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
func DetectCyclesWithOptions(graph *storage.GraphStorage, opts CycleDetectionOptions) ([]Cycle, error) {
	allCycles, err := DetectCycles(graph)
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
				node, err := graph.GetNode(nodeID)
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

// HasCycle checks if the graph contains any cycles (faster than detecting all cycles)
func HasCycle(graph *storage.GraphStorage) (bool, error) {
	const (
		WHITE = 0
		GRAY  = 1
		BLACK = 2
	)

	color := make(map[uint64]int)
	stats := graph.GetStatistics()
	if stats.NodeCount == 0 {
		return false, nil
	}

	nodeIDs := make([]uint64, 0, stats.NodeCount)
	for i := uint64(1); i <= stats.NodeCount; i++ {
		if node, err := graph.GetNode(i); err == nil && node != nil {
			nodeIDs = append(nodeIDs, i)
		}
	}

	for _, nodeID := range nodeIDs {
		if color[nodeID] == WHITE {
			if hasCycleDFS(graph, nodeID, color) {
				return true, nil
			}
		}
	}

	return false, nil
}

// hasCycleDFS performs DFS to check for cycles (returns true on first cycle found)
func hasCycleDFS(graph *storage.GraphStorage, nodeID uint64, color map[uint64]int) bool {
	const (
		WHITE = 0
		GRAY  = 1
		BLACK = 2
	)

	color[nodeID] = GRAY

	edges, err := graph.GetOutgoingEdges(nodeID)
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
			if hasCycleDFS(graph, neighborID, color) {
				return true
			}
		} else if color[neighborID] == GRAY {
			// Back edge found - cycle!
			return true
		}
	}

	color[nodeID] = BLACK
	return false
}
