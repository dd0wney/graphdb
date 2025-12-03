package visualization

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Position represents a 2D coordinate
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// LayoutConfig configures layout parameters
type LayoutConfig struct {
	Width      float64 // Canvas width
	Height     float64 // Canvas height
	Iterations int     // Number of iterations for iterative algorithms
	Padding    float64 // Padding from edges
}

// Layout interface for different layout algorithms
type Layout interface {
	ComputeLayout(gs *storage.GraphStorage, nodeIDs []uint64) (map[uint64]Position, error)
}

// Visualization represents a graph visualization with layout
type Visualization struct {
	Nodes     []*storage.Node
	Edges     []*storage.Edge
	Positions map[uint64]Position
}
