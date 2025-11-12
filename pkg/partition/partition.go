package partition

import (
	"fmt"
	"hash/fnv"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

// PartitionStrategy defines how to partition a graph
type PartitionStrategy interface {
	GetPartition(nodeID uint64) int
	GetPartitionCount() int
}

// HashPartition partitions nodes by hash (simplest, good load balance)
type HashPartition struct {
	partitionCount int
}

// NewHashPartition creates a hash-based partitioning strategy
func NewHashPartition(partitionCount int) *HashPartition {
	return &HashPartition{
		partitionCount: partitionCount,
	}
}

// GetPartition returns which partition a node belongs to
func (hp *HashPartition) GetPartition(nodeID uint64) int {
	h := fnv.New64a()
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(nodeID >> (i * 8))
	}
	h.Write(b)
	return int(h.Sum64() % uint64(hp.partitionCount))
}

// GetPartitionCount returns total number of partitions
func (hp *HashPartition) GetPartitionCount() int {
	return hp.partitionCount
}

// RangePartition partitions by node ID ranges
type RangePartition struct {
	partitionCount int
	rangeSize      uint64
}

// NewRangePartition creates range-based partitioning
func NewRangePartition(partitionCount int, maxNodeID uint64) *RangePartition {
	return &RangePartition{
		partitionCount: partitionCount,
		rangeSize:      maxNodeID / uint64(partitionCount),
	}
}

// GetPartition returns partition for a node
func (rp *RangePartition) GetPartition(nodeID uint64) int {
	partition := int(nodeID / rp.rangeSize)
	if partition >= rp.partitionCount {
		partition = rp.partitionCount - 1
	}
	return partition
}

// GetPartitionCount returns total partitions
func (rp *RangePartition) GetPartitionCount() int {
	return rp.partitionCount
}

// PartitionedGraph wraps a graph with partitioning
type PartitionedGraph struct {
	graph     *storage.GraphStorage
	strategy  PartitionStrategy
	localPart int // Which partition this instance manages

	// Edge cuts (edges crossing partitions)
	edgeCuts map[uint64]*storage.Edge
}

// NewPartitionedGraph creates a partitioned graph view
func NewPartitionedGraph(
	graph *storage.GraphStorage,
	strategy PartitionStrategy,
	localPartition int,
) *PartitionedGraph {
	return &PartitionedGraph{
		graph:     graph,
		strategy:  strategy,
		localPart: localPartition,
		edgeCuts:  make(map[uint64]*storage.Edge),
	}
}

// IsLocalNode checks if a node belongs to this partition
func (pg *PartitionedGraph) IsLocalNode(nodeID uint64) bool {
	return pg.strategy.GetPartition(nodeID) == pg.localPart
}

// GetLocalNodes returns all nodes in this partition
func (pg *PartitionedGraph) GetLocalNodes() ([]*storage.Node, error) {
	stats := pg.graph.GetStatistics()
	nodeCount := int(stats.NodeCount)

	localNodes := make([]*storage.Node, 0)

	for nodeID := uint64(1); nodeID <= uint64(nodeCount); nodeID++ {
		if pg.IsLocalNode(nodeID) {
			if node, err := pg.graph.GetNode(nodeID); err == nil {
				localNodes = append(localNodes, node)
			}
		}
	}

	return localNodes, nil
}

// GetEdgeCuts returns edges that cross partition boundaries
func (pg *PartitionedGraph) GetEdgeCuts() ([]*storage.Edge, error) {
	localNodes, err := pg.GetLocalNodes()
	if err != nil {
		return nil, err
	}

	cuts := make([]*storage.Edge, 0)

	for _, node := range localNodes {
		edges, err := pg.graph.GetOutgoingEdges(node.ID)
		if err != nil {
			continue
		}

		for _, edge := range edges {
			// Edge crosses partition if target is in different partition
			if !pg.IsLocalNode(edge.ToNodeID) {
				cuts = append(cuts, edge)
			}
		}
	}

	return cuts, nil
}

// PartitionMetrics contains partitioning quality metrics
type PartitionMetrics struct {
	PartitionSizes []int     // Nodes per partition
	EdgeCuts       []int     // Cut edges per partition
	LoadBalance    float64   // 0-1 (1 = perfect balance)
	CutRatio       float64   // Fraction of edges that are cuts
}

// ComputePartitionMetrics analyzes partition quality
func ComputePartitionMetrics(graph *storage.GraphStorage, strategy PartitionStrategy) (*PartitionMetrics, error) {
	partCount := strategy.GetPartitionCount()
	sizes := make([]int, partCount)
	cuts := make([]int, partCount)

	stats := graph.GetStatistics()
	nodeCount := int(stats.NodeCount)
	totalEdges := 0
	totalCuts := 0

	// Count nodes and edge cuts per partition
	for nodeID := uint64(1); nodeID <= uint64(nodeCount); nodeID++ {
		partition := strategy.GetPartition(nodeID)
		sizes[partition]++

		edges, err := graph.GetOutgoingEdges(nodeID)
		if err != nil {
			continue
		}

		totalEdges += len(edges)

		for _, edge := range edges {
			targetPartition := strategy.GetPartition(edge.ToNodeID)
			if targetPartition != partition {
				cuts[partition]++
				totalCuts++
			}
		}
	}

	// Calculate load balance (stddev from perfect balance)
	avgSize := float64(nodeCount) / float64(partCount)
	variance := 0.0
	for _, size := range sizes {
		diff := float64(size) - avgSize
		variance += diff * diff
	}
	variance /= float64(partCount)
	stddev := 0.0
	if variance > 0 {
		stddev = 1.0 / (1.0 + variance/avgSize) // Normalize to 0-1
	}

	cutRatio := 0.0
	if totalEdges > 0 {
		cutRatio = float64(totalCuts) / float64(totalEdges)
	}

	return &PartitionMetrics{
		PartitionSizes: sizes,
		EdgeCuts:       cuts,
		LoadBalance:    stddev,
		CutRatio:       cutRatio,
	}, nil
}

// RebalancePartitions suggests node migrations to improve balance
func RebalancePartitions(graph *storage.GraphStorage, strategy PartitionStrategy) ([]NodeMigration, error) {
	metrics, err := ComputePartitionMetrics(graph, strategy)
	if err != nil {
		return nil, err
	}

	migrations := make([]NodeMigration, 0)

	// Find overloaded and underloaded partitions
	partCount := strategy.GetPartitionCount()
	avgSize := 0
	for _, size := range metrics.PartitionSizes {
		avgSize += size
	}
	avgSize /= partCount

	threshold := float64(avgSize) * 0.1 // 10% tolerance

	for partition, size := range metrics.PartitionSizes {
		if float64(size) > float64(avgSize)+threshold {
			// Overloaded - suggest migrations out
			// This is simplified - real impl would consider edge cuts
			excessNodes := size - avgSize
			migrations = append(migrations, NodeMigration{
				FromPartition: partition,
				ToPartition:   -1, // TBD based on underloaded partitions
				NodeCount:     excessNodes,
			})
		}
	}

	return migrations, nil
}

// NodeMigration represents a suggested rebalancing operation
type NodeMigration struct {
	FromPartition int
	ToPartition   int
	NodeCount     int
	NodeIDs       []uint64
}

// DistributedQuery coordinates queries across partitions
type DistributedQuery struct {
	partitions []*PartitionedGraph
}

// NewDistributedQuery creates a distributed query coordinator
func NewDistributedQuery(partitions []*PartitionedGraph) *DistributedQuery {
	return &DistributedQuery{
		partitions: partitions,
	}
}

// GetNode retrieves a node from the correct partition
func (dq *DistributedQuery) GetNode(nodeID uint64) (*storage.Node, error) {
	for _, partition := range dq.partitions {
		if partition.IsLocalNode(nodeID) {
			return partition.graph.GetNode(nodeID)
		}
	}
	return nil, fmt.Errorf("node %d not found in any partition", nodeID)
}

// TraverseGraph performs distributed graph traversal
func (dq *DistributedQuery) TraverseGraph(startID uint64, maxDepth int) ([]*storage.Node, error) {
	visited := make(map[uint64]bool)
	result := make([]*storage.Node, 0)

	var traverse func(nodeID uint64, depth int) error
	traverse = func(nodeID uint64, depth int) error {
		if depth > maxDepth || visited[nodeID] {
			return nil
		}

		visited[nodeID] = true

		// Get node from correct partition
		node, err := dq.GetNode(nodeID)
		if err != nil {
			return err
		}
		result = append(result, node)

		// Get edges (may cross partitions)
		for _, partition := range dq.partitions {
			if partition.IsLocalNode(nodeID) {
				edges, err := partition.graph.GetOutgoingEdges(nodeID)
				if err != nil {
					continue
				}

				for _, edge := range edges {
					if err := traverse(edge.ToNodeID, depth+1); err != nil {
						return err
					}
				}
				break
			}
		}

		return nil
	}

	if err := traverse(startID, 0); err != nil {
		return nil, err
	}

	return result, nil
}
