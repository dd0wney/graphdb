package visualization

import (
	"math"
	"math/rand"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ForceDirectedLayout implements force-directed graph layout
type ForceDirectedLayout struct {
	config *LayoutConfig
}

// NewForceDirectedLayout creates a new force-directed layout
func NewForceDirectedLayout(config *LayoutConfig) *ForceDirectedLayout {
	if config.Iterations == 0 {
		config.Iterations = 50
	}
	if config.Padding == 0 {
		config.Padding = 50
	}
	return &ForceDirectedLayout{config: config}
}

// ComputeLayout computes positions using force-directed algorithm
func (fdl *ForceDirectedLayout) ComputeLayout(gs *storage.GraphStorage, nodeIDs []uint64) (map[uint64]Position, error) {
	if len(nodeIDs) == 0 {
		return make(map[uint64]Position), nil
	}

	// Single node - center it
	if len(nodeIDs) == 1 {
		return map[uint64]Position{
			nodeIDs[0]: {
				X: fdl.config.Width / 2,
				Y: fdl.config.Height / 2,
			},
		}, nil
	}

	// Initialize random positions
	positions := make(map[uint64]Position)
	velocities := make(map[uint64]Position)

	for _, nodeID := range nodeIDs {
		positions[nodeID] = Position{
			X: rand.Float64() * (fdl.config.Width - 2*fdl.config.Padding) + fdl.config.Padding,
			Y: rand.Float64() * (fdl.config.Height - 2*fdl.config.Padding) + fdl.config.Padding,
		}
		velocities[nodeID] = Position{X: 0, Y: 0}
	}

	// Build edge map for fast lookup
	edgeMap := make(map[uint64]map[uint64]bool)
	for _, nodeID := range nodeIDs {
		edgeMap[nodeID] = make(map[uint64]bool)
		outgoing, _ := gs.GetOutgoingEdges(nodeID)
		for _, edge := range outgoing {
			edgeMap[nodeID][edge.ToNodeID] = true
		}
		incoming, _ := gs.GetIncomingEdges(nodeID)
		for _, edge := range incoming {
			edgeMap[nodeID][edge.FromNodeID] = true
		}
	}

	// Force-directed iterations
	k := math.Sqrt((fdl.config.Width * fdl.config.Height) / float64(len(nodeIDs))) // Optimal distance
	temperature := fdl.config.Width / 10.0

	for iter := 0; iter < fdl.config.Iterations; iter++ {
		// Calculate repulsive forces between all pairs
		forces := make(map[uint64]Position)
		for _, nodeID := range nodeIDs {
			forces[nodeID] = Position{X: 0, Y: 0}
		}

		// Repulsion between all nodes
		for i, nodeID1 := range nodeIDs {
			for j := i + 1; j < len(nodeIDs); j++ {
				nodeID2 := nodeIDs[j]
				dx := positions[nodeID1].X - positions[nodeID2].X
				dy := positions[nodeID1].Y - positions[nodeID2].Y
				dist := math.Sqrt(dx*dx + dy*dy)

				if dist < 0.01 {
					dist = 0.01
				}

				// Repulsive force
				force := (k * k) / dist
				fx := (dx / dist) * force
				fy := (dy / dist) * force

				forces[nodeID1] = Position{
					X: forces[nodeID1].X + fx,
					Y: forces[nodeID1].Y + fy,
				}
				forces[nodeID2] = Position{
					X: forces[nodeID2].X - fx,
					Y: forces[nodeID2].Y - fy,
				}
			}
		}

		// Attraction between connected nodes
		for _, nodeID1 := range nodeIDs {
			for nodeID2 := range edgeMap[nodeID1] {
				if _, exists := positions[nodeID2]; !exists {
					continue
				}

				dx := positions[nodeID1].X - positions[nodeID2].X
				dy := positions[nodeID1].Y - positions[nodeID2].Y
				dist := math.Sqrt(dx*dx + dy*dy)

				if dist < 0.01 {
					continue
				}

				// Attractive force
				force := (dist * dist) / k
				fx := (dx / dist) * force
				fy := (dy / dist) * force

				forces[nodeID1] = Position{
					X: forces[nodeID1].X - fx,
					Y: forces[nodeID1].Y - fy,
				}
			}
		}

		// Apply forces with cooling
		cool := 1.0 - float64(iter)/float64(fdl.config.Iterations)
		for _, nodeID := range nodeIDs {
			fx := forces[nodeID].X
			fy := forces[nodeID].Y
			force := math.Sqrt(fx*fx + fy*fy)

			if force > 0 {
				dx := (fx / force) * math.Min(force, temperature) * cool
				dy := (fy / force) * math.Min(force, temperature) * cool

				positions[nodeID] = Position{
					X: positions[nodeID].X + dx,
					Y: positions[nodeID].Y + dy,
				}
			}
		}

		temperature *= 0.95
	}

	// Normalize positions to bounds
	return normalizePositions(positions, fdl.config.Width, fdl.config.Height, fdl.config.Padding), nil
}
