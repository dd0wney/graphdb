package visualization

import "math"

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
