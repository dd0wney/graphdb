package partition

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Helper to create test storage
func newTestStorage(t *testing.T) *storage.GraphStorage {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create test storage: %v", err)
	}
	t.Cleanup(func() { gs.Close() })
	return gs
}

// --- HashPartition Tests ---

func TestNewHashPartition(t *testing.T) {
	tests := []struct {
		name           string
		partitionCount int
	}{
		{"single partition", 1},
		{"two partitions", 2},
		{"four partitions", 4},
		{"many partitions", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hp := NewHashPartition(tt.partitionCount)
			if hp == nil {
				t.Fatal("NewHashPartition returned nil")
			}
			if hp.GetPartitionCount() != tt.partitionCount {
				t.Errorf("GetPartitionCount() = %d, want %d", hp.GetPartitionCount(), tt.partitionCount)
			}
		})
	}
}

func TestHashPartition_GetPartition(t *testing.T) {
	hp := NewHashPartition(4)

	tests := []struct {
		name   string
		nodeID uint64
	}{
		{"node 0", 0},
		{"node 1", 1},
		{"node 100", 100},
		{"node max", ^uint64(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			partition := hp.GetPartition(tt.nodeID)
			if partition < 0 || partition >= 4 {
				t.Errorf("GetPartition(%d) = %d, out of range [0, 4)", tt.nodeID, partition)
			}
		})
	}
}

func TestHashPartition_Distribution(t *testing.T) {
	hp := NewHashPartition(4)
	counts := make([]int, 4)

	// Hash 1000 sequential IDs and check distribution
	for i := uint64(0); i < 1000; i++ {
		partition := hp.GetPartition(i)
		counts[partition]++
	}

	// Each partition should have roughly 250 nodes (allow 50% variance)
	for i, count := range counts {
		if count < 100 || count > 400 {
			t.Errorf("Partition %d has %d nodes, expected roughly 250", i, count)
		}
	}
}

func TestHashPartition_Deterministic(t *testing.T) {
	hp := NewHashPartition(8)

	// Same nodeID should always return same partition
	for i := uint64(0); i < 100; i++ {
		p1 := hp.GetPartition(i)
		p2 := hp.GetPartition(i)
		if p1 != p2 {
			t.Errorf("GetPartition(%d) not deterministic: %d vs %d", i, p1, p2)
		}
	}
}

// --- RangePartition Tests ---

func TestNewRangePartition(t *testing.T) {
	tests := []struct {
		name           string
		partitionCount int
		maxNodeID      uint64
	}{
		{"basic", 4, 1000},
		{"single partition", 1, 100},
		{"many partitions", 10, 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := NewRangePartition(tt.partitionCount, tt.maxNodeID)
			if rp == nil {
				t.Fatal("NewRangePartition returned nil")
			}
			if rp.GetPartitionCount() != tt.partitionCount {
				t.Errorf("GetPartitionCount() = %d, want %d", rp.GetPartitionCount(), tt.partitionCount)
			}
		})
	}
}

func TestRangePartition_GetPartition(t *testing.T) {
	rp := NewRangePartition(4, 1000) // 250 nodes per partition

	tests := []struct {
		name      string
		nodeID    uint64
		wantPart  int
	}{
		{"first in partition 0", 0, 0},
		{"last in partition 0", 249, 0},
		{"first in partition 1", 250, 1},
		{"first in partition 2", 500, 2},
		{"first in partition 3", 750, 3},
		{"beyond max still in last partition", 1500, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			partition := rp.GetPartition(tt.nodeID)
			if partition != tt.wantPart {
				t.Errorf("GetPartition(%d) = %d, want %d", tt.nodeID, partition, tt.wantPart)
			}
		})
	}
}

func TestRangePartition_BoundaryClipping(t *testing.T) {
	rp := NewRangePartition(4, 100)

	// Nodes beyond max should still fall in the last partition
	partition := rp.GetPartition(1000)
	if partition != 3 {
		t.Errorf("GetPartition(1000) = %d, want 3 (last partition)", partition)
	}
}

// --- PartitionedGraph Tests ---

func TestNewPartitionedGraph(t *testing.T) {
	gs := newTestStorage(t)
	hp := NewHashPartition(4)

	pg := NewPartitionedGraph(gs, hp, 0)
	if pg == nil {
		t.Fatal("NewPartitionedGraph returned nil")
	}
	if pg.localPart != 0 {
		t.Errorf("localPart = %d, want 0", pg.localPart)
	}
}

func TestPartitionedGraph_IsLocalNode(t *testing.T) {
	gs := newTestStorage(t)
	hp := NewHashPartition(2)

	pg0 := NewPartitionedGraph(gs, hp, 0)
	pg1 := NewPartitionedGraph(gs, hp, 1)

	// Every node should be local to exactly one partition
	for i := uint64(0); i < 100; i++ {
		isLocal0 := pg0.IsLocalNode(i)
		isLocal1 := pg1.IsLocalNode(i)

		if isLocal0 == isLocal1 {
			t.Errorf("Node %d is local to both or neither partition", i)
		}
	}
}

func TestPartitionedGraph_GetLocalNodes(t *testing.T) {
	gs := newTestStorage(t)

	// Create 10 nodes
	for i := 0; i < 10; i++ {
		_, err := gs.CreateNode([]string{"Test"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	hp := NewHashPartition(2)
	pg0 := NewPartitionedGraph(gs, hp, 0)
	pg1 := NewPartitionedGraph(gs, hp, 1)

	localNodes0, err := pg0.GetLocalNodes()
	if err != nil {
		t.Fatalf("GetLocalNodes() error: %v", err)
	}

	localNodes1, err := pg1.GetLocalNodes()
	if err != nil {
		t.Fatalf("GetLocalNodes() error: %v", err)
	}

	// Together they should cover all 10 nodes
	totalLocal := len(localNodes0) + len(localNodes1)
	if totalLocal != 10 {
		t.Errorf("Total local nodes = %d, want 10", totalLocal)
	}
}

func TestPartitionedGraph_GetEdgeCuts(t *testing.T) {
	gs := newTestStorage(t)

	// Create nodes
	nodes := make([]*storage.Node, 4)
	for i := 0; i < 4; i++ {
		node, err := gs.CreateNode([]string{"Test"}, nil)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
		nodes[i] = node
	}

	// Create edges
	gs.CreateEdge(nodes[0].ID, nodes[1].ID, "LINK", nil, 1.0)
	gs.CreateEdge(nodes[0].ID, nodes[2].ID, "LINK", nil, 1.0)
	gs.CreateEdge(nodes[2].ID, nodes[3].ID, "LINK", nil, 1.0)

	hp := NewHashPartition(2)
	pg := NewPartitionedGraph(gs, hp, 0)

	cuts, err := pg.GetEdgeCuts()
	if err != nil {
		t.Fatalf("GetEdgeCuts() error: %v", err)
	}

	// Verify all returned cuts actually cross partitions
	for _, cut := range cuts {
		fromPart := hp.GetPartition(cut.FromNodeID)
		toPart := hp.GetPartition(cut.ToNodeID)
		if fromPart == toPart {
			t.Errorf("Edge %d->%d not a cut (same partition %d)", cut.FromNodeID, cut.ToNodeID, fromPart)
		}
	}
}

// --- PartitionMetrics Tests ---

func TestComputePartitionMetrics_EmptyGraph(t *testing.T) {
	gs := newTestStorage(t)
	hp := NewHashPartition(4)

	metrics, err := ComputePartitionMetrics(gs, hp)
	if err != nil {
		t.Fatalf("ComputePartitionMetrics() error: %v", err)
	}

	// All sizes should be 0
	for i, size := range metrics.PartitionSizes {
		if size != 0 {
			t.Errorf("PartitionSizes[%d] = %d, want 0", i, size)
		}
	}

	// Cut ratio should be 0 (no edges)
	if metrics.CutRatio != 0 {
		t.Errorf("CutRatio = %f, want 0", metrics.CutRatio)
	}
}

func TestComputePartitionMetrics_WithData(t *testing.T) {
	gs := newTestStorage(t)

	// Create 20 nodes
	nodes := make([]*storage.Node, 20)
	for i := 0; i < 20; i++ {
		node, err := gs.CreateNode([]string{"Test"}, nil)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
		nodes[i] = node
	}

	// Create some edges (including cross-partition)
	for i := 0; i < 19; i++ {
		gs.CreateEdge(nodes[i].ID, nodes[i+1].ID, "NEXT", nil, 1.0)
	}

	hp := NewHashPartition(4)
	metrics, err := ComputePartitionMetrics(gs, hp)
	if err != nil {
		t.Fatalf("ComputePartitionMetrics() error: %v", err)
	}

	// Total nodes across partitions should be 20
	totalNodes := 0
	for _, size := range metrics.PartitionSizes {
		totalNodes += size
	}
	if totalNodes != 20 {
		t.Errorf("Total nodes = %d, want 20", totalNodes)
	}

	// Load balance should be > 0 (some balance)
	if metrics.LoadBalance < 0 || metrics.LoadBalance > 1 {
		t.Errorf("LoadBalance = %f, should be in [0, 1]", metrics.LoadBalance)
	}

	// Cut ratio should be in valid range
	if metrics.CutRatio < 0 || metrics.CutRatio > 1 {
		t.Errorf("CutRatio = %f, should be in [0, 1]", metrics.CutRatio)
	}
}

// --- RebalancePartitions Tests ---

func TestRebalancePartitions_BalancedGraph(t *testing.T) {
	gs := newTestStorage(t)

	// Create nodes that will be somewhat balanced
	for i := 0; i < 40; i++ {
		gs.CreateNode([]string{"Test"}, nil)
	}

	hp := NewHashPartition(4)
	migrations, err := RebalancePartitions(gs, hp)
	if err != nil {
		t.Fatalf("RebalancePartitions() error: %v", err)
	}

	// Migrations should be a valid slice (possibly empty if well-balanced)
	if migrations == nil {
		t.Error("RebalancePartitions() returned nil")
	}
}

func TestRebalancePartitions_EmptyGraph(t *testing.T) {
	gs := newTestStorage(t)
	hp := NewHashPartition(4)

	migrations, err := RebalancePartitions(gs, hp)
	if err != nil {
		t.Fatalf("RebalancePartitions() error: %v", err)
	}

	// No migrations needed for empty graph
	if len(migrations) != 0 {
		t.Errorf("Expected 0 migrations for empty graph, got %d", len(migrations))
	}
}

// --- DistributedQuery Tests ---

func TestNewDistributedQuery(t *testing.T) {
	gs := newTestStorage(t)
	hp := NewHashPartition(2)

	pg0 := NewPartitionedGraph(gs, hp, 0)
	pg1 := NewPartitionedGraph(gs, hp, 1)

	dq := NewDistributedQuery([]*PartitionedGraph{pg0, pg1})
	if dq == nil {
		t.Fatal("NewDistributedQuery returned nil")
	}
	if len(dq.partitions) != 2 {
		t.Errorf("len(partitions) = %d, want 2", len(dq.partitions))
	}
}

func TestDistributedQuery_GetNode(t *testing.T) {
	gs := newTestStorage(t)

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Test"}, map[string]storage.Value{
		"name": storage.StringValue("node1"),
	})
	node2, _ := gs.CreateNode([]string{"Test"}, map[string]storage.Value{
		"name": storage.StringValue("node2"),
	})

	hp := NewHashPartition(2)
	pg0 := NewPartitionedGraph(gs, hp, 0)
	pg1 := NewPartitionedGraph(gs, hp, 1)

	dq := NewDistributedQuery([]*PartitionedGraph{pg0, pg1})

	// Should be able to get both nodes through distributed query
	retrieved1, err := dq.GetNode(node1.ID)
	if err != nil {
		t.Errorf("GetNode(%d) error: %v", node1.ID, err)
	}
	if retrieved1 == nil || retrieved1.ID != node1.ID {
		t.Errorf("GetNode(%d) returned wrong node", node1.ID)
	}

	retrieved2, err := dq.GetNode(node2.ID)
	if err != nil {
		t.Errorf("GetNode(%d) error: %v", node2.ID, err)
	}
	if retrieved2 == nil || retrieved2.ID != node2.ID {
		t.Errorf("GetNode(%d) returned wrong node", node2.ID)
	}
}

func TestDistributedQuery_GetNode_NotFound(t *testing.T) {
	gs := newTestStorage(t)
	hp := NewHashPartition(2)

	pg0 := NewPartitionedGraph(gs, hp, 0)
	pg1 := NewPartitionedGraph(gs, hp, 1)

	dq := NewDistributedQuery([]*PartitionedGraph{pg0, pg1})

	// Non-existent node should return error
	_, err := dq.GetNode(999)
	if err == nil {
		t.Error("GetNode(999) should return error for non-existent node")
	}
}

func TestDistributedQuery_TraverseGraph(t *testing.T) {
	gs := newTestStorage(t)

	// Create a chain of nodes
	nodes := make([]*storage.Node, 5)
	for i := 0; i < 5; i++ {
		node, _ := gs.CreateNode([]string{"Test"}, map[string]storage.Value{
			"order": storage.IntValue(int64(i)),
		})
		nodes[i] = node
	}

	// Link them: 0 -> 1 -> 2 -> 3 -> 4
	for i := 0; i < 4; i++ {
		gs.CreateEdge(nodes[i].ID, nodes[i+1].ID, "NEXT", nil, 1.0)
	}

	hp := NewHashPartition(2)
	pg0 := NewPartitionedGraph(gs, hp, 0)
	pg1 := NewPartitionedGraph(gs, hp, 1)

	dq := NewDistributedQuery([]*PartitionedGraph{pg0, pg1})

	// Traverse from start with depth 5 should get all nodes
	result, err := dq.TraverseGraph(nodes[0].ID, 5)
	if err != nil {
		t.Fatalf("TraverseGraph() error: %v", err)
	}

	if len(result) != 5 {
		t.Errorf("TraverseGraph() returned %d nodes, want 5", len(result))
	}
}

func TestDistributedQuery_TraverseGraph_DepthLimit(t *testing.T) {
	gs := newTestStorage(t)

	// Create a chain of nodes
	nodes := make([]*storage.Node, 10)
	for i := 0; i < 10; i++ {
		node, _ := gs.CreateNode([]string{"Test"}, nil)
		nodes[i] = node
	}

	// Link them: 0 -> 1 -> ... -> 9
	for i := 0; i < 9; i++ {
		gs.CreateEdge(nodes[i].ID, nodes[i+1].ID, "NEXT", nil, 1.0)
	}

	hp := NewHashPartition(2)
	pg0 := NewPartitionedGraph(gs, hp, 0)
	pg1 := NewPartitionedGraph(gs, hp, 1)

	dq := NewDistributedQuery([]*PartitionedGraph{pg0, pg1})

	// Traverse with depth 3 should get at most 4 nodes (start + 3 hops)
	result, err := dq.TraverseGraph(nodes[0].ID, 3)
	if err != nil {
		t.Fatalf("TraverseGraph() error: %v", err)
	}

	if len(result) > 4 {
		t.Errorf("TraverseGraph(depth=3) returned %d nodes, want <= 4", len(result))
	}
}

func TestDistributedQuery_TraverseGraph_NotFound(t *testing.T) {
	gs := newTestStorage(t)
	hp := NewHashPartition(2)

	pg0 := NewPartitionedGraph(gs, hp, 0)
	pg1 := NewPartitionedGraph(gs, hp, 1)

	dq := NewDistributedQuery([]*PartitionedGraph{pg0, pg1})

	// Traversing from non-existent node should return error
	_, err := dq.TraverseGraph(999, 5)
	if err == nil {
		t.Error("TraverseGraph(999) should return error for non-existent start node")
	}
}

// --- NodeMigration Tests ---

func TestNodeMigration_Struct(t *testing.T) {
	migration := NodeMigration{
		FromPartition: 0,
		ToPartition:   1,
		NodeCount:     5,
		NodeIDs:       []uint64{1, 2, 3, 4, 5},
	}

	if migration.FromPartition != 0 {
		t.Errorf("FromPartition = %d, want 0", migration.FromPartition)
	}
	if migration.ToPartition != 1 {
		t.Errorf("ToPartition = %d, want 1", migration.ToPartition)
	}
	if migration.NodeCount != 5 {
		t.Errorf("NodeCount = %d, want 5", migration.NodeCount)
	}
	if len(migration.NodeIDs) != 5 {
		t.Errorf("len(NodeIDs) = %d, want 5", len(migration.NodeIDs))
	}
}

// --- Interface Compliance Tests ---

func TestHashPartition_ImplementsPartitionStrategy(t *testing.T) {
	var _ PartitionStrategy = (*HashPartition)(nil)
}

func TestRangePartition_ImplementsPartitionStrategy(t *testing.T) {
	var _ PartitionStrategy = (*RangePartition)(nil)
}
