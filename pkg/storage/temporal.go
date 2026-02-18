package storage

import (
	"time"
)

// TemporalEdge is an edge with time validity
type TemporalEdge struct {
	*Edge
	ValidFrom int64 // Unix timestamp
	ValidTo   int64 // Unix timestamp (0 = infinity)
}

// TemporalQuery operations for time-based graph queries
type TemporalQuery struct {
	graph *GraphStorage
}

// NewTemporalQuery creates a temporal query interface
func NewTemporalQuery(graph *GraphStorage) *TemporalQuery {
	return &TemporalQuery{graph: graph}
}

// GetEdgesAtTime returns all edges valid at a specific timestamp
func (tq *TemporalQuery) GetEdgesAtTime(nodeID uint64, timestamp int64) ([]*TemporalEdge, error) {
	// Get all edges
	edges, err := tq.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		return nil, err
	}

	// Filter by time
	temporalEdges := make([]*TemporalEdge, 0)
	for _, edge := range edges {
		// Check if edge has temporal properties
		validFrom, hasFrom := edge.Properties["valid_from"]
		validTo, hasTo := edge.Properties["valid_to"]

		if !hasFrom {
			// No temporal info - always valid
			temporalEdges = append(temporalEdges, &TemporalEdge{
				Edge:      edge,
				ValidFrom: 0,
				ValidTo:   0,
			})
			continue
		}

		from, _ := validFrom.AsInt()
		to := int64(0)
		if hasTo {
			to, _ = validTo.AsInt()
		}

		// Check if edge is valid at timestamp
		if timestamp >= from && (to == 0 || timestamp <= to) {
			temporalEdges = append(temporalEdges, &TemporalEdge{
				Edge:      edge,
				ValidFrom: from,
				ValidTo:   to,
			})
		}
	}

	return temporalEdges, nil
}

// GetEdgesInTimeRange returns edges valid in a time range
func (tq *TemporalQuery) GetEdgesInTimeRange(nodeID uint64, start, end int64) ([]*TemporalEdge, error) {
	edges, err := tq.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		return nil, err
	}

	temporalEdges := make([]*TemporalEdge, 0)
	for _, edge := range edges {
		validFrom, hasFrom := edge.Properties["valid_from"]
		validTo, hasTo := edge.Properties["valid_to"]

		if !hasFrom {
			// No temporal info - always valid
			temporalEdges = append(temporalEdges, &TemporalEdge{
				Edge:      edge,
				ValidFrom: 0,
				ValidTo:   0,
			})
			continue
		}

		from, _ := validFrom.AsInt()
		to := int64(0)
		if hasTo {
			to, _ = validTo.AsInt()
		}

		// Check if edge overlaps with time range
		if (to == 0 || to >= start) && from <= end {
			temporalEdges = append(temporalEdges, &TemporalEdge{
				Edge:      edge,
				ValidFrom: from,
				ValidTo:   to,
			})
		}
	}

	return temporalEdges, nil
}

// CreateTemporalEdge creates an edge with time validity
func (tq *TemporalQuery) CreateTemporalEdge(
	fromID, toID uint64,
	edgeType string,
	properties map[string]Value,
	weight float64,
	validFrom, validTo int64,
) (*Edge, error) {
	// Add temporal properties
	if properties == nil {
		properties = make(map[string]Value)
	}

	properties["valid_from"] = IntValue(validFrom)
	if validTo > 0 {
		properties["valid_to"] = IntValue(validTo)
	}

	return tq.graph.CreateEdge(fromID, toID, edgeType, properties, weight)
}

// TimeTravel returns a snapshot of the graph at a specific time
type GraphSnapshot struct {
	timestamp int64
	graph     *GraphStorage
}

// NewGraphSnapshot creates a point-in-time view of the graph
func NewGraphSnapshot(graph *GraphStorage, timestamp int64) *GraphSnapshot {
	return &GraphSnapshot{
		timestamp: timestamp,
		graph:     graph,
	}
}

// GetOutgoingEdges returns edges valid at snapshot time
func (gs *GraphSnapshot) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) {
	tq := NewTemporalQuery(gs.graph)
	temporalEdges, err := tq.GetEdgesAtTime(nodeID, gs.timestamp)
	if err != nil {
		return nil, err
	}

	edges := make([]*Edge, len(temporalEdges))
	for i, te := range temporalEdges {
		edges[i] = te.Edge
	}

	return edges, nil
}

// TemporalMetrics computes temporal graph metrics
type TemporalMetrics struct {
	AverageEdgeLifetime float64
	ActiveEdgesAtTime   map[int64]int
	EdgeCreationRate    float64
	EdgeDeletionRate    float64
}

// ComputeTemporalMetrics analyzes temporal patterns
func ComputeTemporalMetrics(graph *GraphStorage, startTime, endTime int64) (*TemporalMetrics, error) {
	stats := graph.GetStatistics()
	nodeCount := int(stats.NodeCount)

	totalLifetime := int64(0)
	edgeCount := 0       // edges with temporal data created in range
	deletedCount := 0    // edges with valid_to in range (tombstoned)

	duration := endTime - startTime
	if duration <= 0 {
		duration = 1 // Avoid division by zero
	}

	// Analyze all edges
	for nodeID := uint64(1); nodeID <= uint64(nodeCount); nodeID++ {
		edges, err := graph.GetOutgoingEdges(nodeID)
		if err != nil {
			continue
		}

		for _, edge := range edges {
			validFrom, hasFrom := edge.Properties["valid_from"]
			if !hasFrom {
				continue
			}

			from, _ := validFrom.AsInt()

			// Count edges created within the analysis time range
			if from >= startTime && from <= endTime {
				edgeCount++
			}

			to := time.Now().Unix()
			if validTo, hasTo := edge.Properties["valid_to"]; hasTo {
				to, _ = validTo.AsInt()

				// Count edges deleted (tombstoned) within the analysis time range
				if to >= startTime && to <= endTime {
					deletedCount++
				}
			}

			totalLifetime += (to - from)
		}
	}

	avgLifetime := float64(0)
	if edgeCount > 0 {
		avgLifetime = float64(totalLifetime) / float64(edgeCount)
	}

	return &TemporalMetrics{
		AverageEdgeLifetime: avgLifetime,
		ActiveEdgesAtTime:   make(map[int64]int),
		EdgeCreationRate:    float64(edgeCount) / float64(duration),
		EdgeDeletionRate:    float64(deletedCount) / float64(duration),
	}, nil
}

// TemporalTraversal performs BFS with time constraints
func TemporalTraversal(graph *GraphStorage, startID uint64, timestamp int64, maxDepth int) ([]*Node, error) {
	visited := make(map[uint64]bool)
	result := make([]*Node, 0)
	tq := NewTemporalQuery(graph)

	var traverse func(nodeID uint64, depth int) error
	traverse = func(nodeID uint64, depth int) error {
		if depth > maxDepth || visited[nodeID] {
			return nil
		}

		visited[nodeID] = true

		node, err := graph.GetNode(nodeID)
		if err != nil {
			return err
		}
		result = append(result, node)

		// Get temporal edges
		edges, err := tq.GetEdgesAtTime(nodeID, timestamp)
		if err != nil {
			return err
		}

		for _, edge := range edges {
			if err := traverse(edge.ToNodeID, depth+1); err != nil {
				return err
			}
		}

		return nil
	}

	if err := traverse(startID, 0); err != nil {
		return nil, err
	}

	return result, nil
}
