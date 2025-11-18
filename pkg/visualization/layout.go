package visualization

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"

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

// normalizePositions scales positions to fit within bounds
func normalizePositions(positions map[uint64]Position, width, height, padding float64) map[uint64]Position {
	if len(positions) == 0 {
		return positions
	}

	// Find bounds
	minX, maxX := math.MaxFloat64, -math.MaxFloat64
	minY, maxY := math.MaxFloat64, -math.MaxFloat64

	for _, pos := range positions {
		minX = math.Min(minX, pos.X)
		maxX = math.Max(maxX, pos.X)
		minY = math.Min(minY, pos.Y)
		maxY = math.Max(maxY, pos.Y)
	}

	rangeX := maxX - minX
	rangeY := maxY - minY

	if rangeX < 0.01 {
		rangeX = 1
	}
	if rangeY < 0.01 {
		rangeY = 1
	}

	// Scale to fit bounds with padding
	targetWidth := width - 2*padding
	targetHeight := height - 2*padding

	normalized := make(map[uint64]Position)
	for nodeID, pos := range positions {
		normalized[nodeID] = Position{
			X: padding + ((pos.X-minX)/rangeX)*targetWidth,
			Y: padding + ((pos.Y-minY)/rangeY)*targetHeight,
		}
	}

	return normalized
}

// Visualization represents a graph visualization with layout
type Visualization struct {
	Nodes     []*storage.Node
	Edges     []*storage.Edge
	Positions map[uint64]Position
}

// ExportJSON exports the visualization to JSON
func (v *Visualization) ExportJSON() ([]byte, error) {
	type NodeViz struct {
		ID         uint64            `json:"id"`
		Labels     []string          `json:"labels"`
		Properties map[string]string `json:"properties"`
		X          float64           `json:"x"`
		Y          float64           `json:"y"`
	}

	type EdgeViz struct {
		ID         uint64  `json:"id"`
		FromNodeID uint64  `json:"from"`
		ToNodeID   uint64  `json:"to"`
		Type       string  `json:"type"`
		Weight     float64 `json:"weight"`
	}

	type VizData struct {
		Nodes []NodeViz `json:"nodes"`
		Edges []EdgeViz `json:"edges"`
	}

	data := VizData{
		Nodes: make([]NodeViz, 0, len(v.Nodes)),
		Edges: make([]EdgeViz, 0, len(v.Edges)),
	}

	// Convert nodes
	for _, node := range v.Nodes {
		pos := v.Positions[node.ID]
		props := make(map[string]string)

		for key, val := range node.Properties {
			if val.Type == storage.TypeString {
				if str, err := val.AsString(); err == nil {
					props[key] = str
				}
			} else {
				props[key] = fmt.Sprintf("%v", val.Data)
			}
		}

		data.Nodes = append(data.Nodes, NodeViz{
			ID:         node.ID,
			Labels:     node.Labels,
			Properties: props,
			X:          pos.X,
			Y:          pos.Y,
		})
	}

	// Convert edges
	for _, edge := range v.Edges {
		data.Edges = append(data.Edges, EdgeViz{
			ID:         edge.ID,
			FromNodeID: edge.FromNodeID,
			ToNodeID:   edge.ToNodeID,
			Type:       edge.Type,
			Weight:     edge.Weight,
		})
	}

	return json.Marshal(data)
}
