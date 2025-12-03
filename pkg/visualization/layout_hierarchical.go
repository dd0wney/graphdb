package visualization

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// HierarchicalLayout arranges nodes in a tree hierarchy
type HierarchicalLayout struct {
	config *LayoutConfig
}

// NewHierarchicalLayout creates a new hierarchical layout
func NewHierarchicalLayout(config *LayoutConfig) *HierarchicalLayout {
	if config.Padding == 0 {
		config.Padding = 50
	}
	return &HierarchicalLayout{config: config}
}

// ComputeLayout arranges nodes hierarchically
func (hl *HierarchicalLayout) ComputeLayout(gs *storage.GraphStorage, nodeIDs []uint64) (map[uint64]Position, error) {
	positions := make(map[uint64]Position)

	if len(nodeIDs) == 0 {
		return positions, nil
	}

	// Find root nodes (nodes with no incoming edges)
	roots := make([]uint64, 0)
	for _, nodeID := range nodeIDs {
		incoming, _ := gs.GetIncomingEdges(nodeID)
		if len(incoming) == 0 {
			roots = append(roots, nodeID)
		}
	}

	if len(roots) == 0 {
		// No clear root, use first node
		roots = []uint64{nodeIDs[0]}
	}

	// Build levels using BFS
	levels := make([][]uint64, 0)
	visited := make(map[uint64]bool)
	currentLevel := roots

	for len(currentLevel) > 0 {
		levels = append(levels, currentLevel)
		nextLevel := make([]uint64, 0)

		for _, nodeID := range currentLevel {
			visited[nodeID] = true
			outgoing, _ := gs.GetOutgoingEdges(nodeID)
			for _, edge := range outgoing {
				if !visited[edge.ToNodeID] {
					nextLevel = append(nextLevel, edge.ToNodeID)
					visited[edge.ToNodeID] = true
				}
			}
		}

		currentLevel = nextLevel
	}

	// Add unvisited nodes to last level
	for _, nodeID := range nodeIDs {
		if !visited[nodeID] {
			if len(levels) == 0 {
				levels = append(levels, []uint64{})
			}
			levels[len(levels)-1] = append(levels[len(levels)-1], nodeID)
		}
	}

	// Position nodes
	levelHeight := (hl.config.Height - 2*hl.config.Padding) / float64(len(levels))

	for levelIdx, level := range levels {
		y := hl.config.Padding + float64(levelIdx)*levelHeight + levelHeight/2
		levelWidth := hl.config.Width - 2*hl.config.Padding
		spacing := levelWidth / float64(len(level)+1)

		for nodeIdx, nodeID := range level {
			x := hl.config.Padding + spacing*float64(nodeIdx+1)
			positions[nodeID] = Position{X: x, Y: y}
		}
	}

	return positions, nil
}
