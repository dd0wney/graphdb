package visualization

import (
	"math"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// CircularLayout arranges nodes in a circle
type CircularLayout struct {
	config *LayoutConfig
}

// NewCircularLayout creates a new circular layout
func NewCircularLayout(config *LayoutConfig) *CircularLayout {
	if config.Padding == 0 {
		config.Padding = 50
	}
	return &CircularLayout{config: config}
}

// ComputeLayout arranges nodes in a circle
func (cl *CircularLayout) ComputeLayout(gs *storage.GraphStorage, nodeIDs []uint64) (map[uint64]Position, error) {
	positions := make(map[uint64]Position)

	if len(nodeIDs) == 0 {
		return positions, nil
	}

	centerX := cl.config.Width / 2
	centerY := cl.config.Height / 2
	radius := math.Min(centerX, centerY) - cl.config.Padding

	angleStep := 2 * math.Pi / float64(len(nodeIDs))

	for i, nodeID := range nodeIDs {
		angle := float64(i) * angleStep
		positions[nodeID] = Position{
			X: centerX + radius*math.Cos(angle),
			Y: centerY + radius*math.Sin(angle),
		}
	}

	return positions, nil
}
