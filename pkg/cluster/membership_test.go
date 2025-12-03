package cluster

import (
	"testing"
	"time"
)

// TestNewClusterMembership tests creation of cluster membership
func TestNewClusterMembership(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	if membership == nil {
		t.Fatal("Expected non-nil membership")
	}

	localNode := membership.GetLocalNode()
	if localNode.ID != "node-1" {
		t.Errorf("Expected node ID 'node-1', got '%s'", localNode.ID)
	}

	if localNode.Addr != "localhost:9090" {
		t.Errorf("Expected addr 'localhost:9090', got '%s'", localNode.Addr)
	}

	if localNode.Role != RoleReplica {
		t.Errorf("Expected initial role RoleReplica, got %v", localNode.Role)
	}

	if localNode.Epoch != 0 {
		t.Errorf("Expected initial epoch 0, got %d", localNode.Epoch)
	}
}

// TestAddNode tests adding nodes to membership
func TestAddNode(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Add a new node
	node := NodeInfo{
		ID:       "node-2",
		Addr:     "localhost:9091",
		Role:     RoleReplica,
		Epoch:    0,
		Term:     0,
		Priority: 1,
	}

	err := membership.AddNode(node)
	if err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	// Verify node was added
	allNodes := membership.GetAllNodes()
	if len(allNodes) != 2 { // local + added node
		t.Errorf("Expected 2 nodes, got %d", len(allNodes))
	}

	// Try to add same node again
	err = membership.AddNode(node)
	if err != ErrNodeAlreadyExists {
		t.Errorf("Expected ErrNodeAlreadyExists, got %v", err)
	}
}

// TestRemoveNode tests removing nodes from membership
func TestRemoveNode(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Add two nodes
	node2 := NodeInfo{ID: "node-2", Addr: "localhost:9091", Role: RoleReplica}
	node3 := NodeInfo{ID: "node-3", Addr: "localhost:9092", Role: RoleReplica}

	membership.AddNode(node2)
	membership.AddNode(node3)

	// Remove node-2
	err := membership.RemoveNode("node-2")
	if err != nil {
		t.Fatalf("Failed to remove node: %v", err)
	}

	// Verify removal
	allNodes := membership.GetAllNodes()
	if len(allNodes) != 2 { // local + node-3
		t.Errorf("Expected 2 nodes after removal, got %d", len(allNodes))
	}

	// Try to remove non-existent node
	err = membership.RemoveNode("node-999")
	if err != ErrNodeNotFound {
		t.Errorf("Expected ErrNodeNotFound, got %v", err)
	}
}

// TestRoleManagement tests role setting and querying
func TestRoleManagement(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Initially should be replica
	if membership.GetLocalNode().Role != RoleReplica {
		t.Error("Expected initial role to be RoleReplica")
	}

	// Set to primary
	membership.SetLocalRole(RolePrimary)
	if membership.GetLocalNode().Role != RolePrimary {
		t.Error("Failed to set role to RolePrimary")
	}

	// Add another primary node
	primaryNode := NodeInfo{ID: "node-2", Addr: "localhost:9091", Role: RolePrimary}
	membership.AddNode(primaryNode)

	// Get primary
	primary := membership.GetPrimary()
	if primary == nil {
		t.Fatal("Expected to find a primary")
	}

	// Should get the first primary (either local or node-2)
	if primary.Role != RolePrimary {
		t.Error("Primary node should have RolePrimary")
	}
}

// TestEpochManagement tests epoch increment and tracking
func TestEpochManagement(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Initial epoch should be 0
	if membership.GetEpoch() != 0 {
		t.Errorf("Expected initial epoch 0, got %d", membership.GetEpoch())
	}

	// Increment epoch
	newEpoch := membership.IncrementEpoch()
	if newEpoch != 1 {
		t.Errorf("Expected epoch 1 after increment, got %d", newEpoch)
	}

	// Verify epoch was updated
	if membership.GetEpoch() != 1 {
		t.Errorf("Expected stored epoch 1, got %d", membership.GetEpoch())
	}

	// Local node should also have updated epoch
	if membership.GetLocalNode().Epoch != 1 {
		t.Errorf("Expected local node epoch 1, got %d", membership.GetLocalNode().Epoch)
	}
}

// TestHeartbeatUpdates tests updating node heartbeat info
func TestHeartbeatUpdates(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Add a node
	node := NodeInfo{ID: "node-2", Addr: "localhost:9091", Role: RoleReplica}
	membership.AddNode(node)

	// Update heartbeat
	beforeUpdate := time.Now()
	err := membership.UpdateNodeHeartbeat("node-2", 42, 1, 5)
	if err != nil {
		t.Fatalf("Failed to update heartbeat: %v", err)
	}

	// Verify update
	updatedNode, err := membership.GetNode("node-2")
	if err != nil {
		t.Fatalf("Failed to get node: %v", err)
	}

	if updatedNode.LastHeartbeatSeq != 42 {
		t.Errorf("Expected heartbeat seq 42, got %d", updatedNode.LastHeartbeatSeq)
	}

	if updatedNode.Epoch != 1 {
		t.Errorf("Expected epoch 1, got %d", updatedNode.Epoch)
	}

	if updatedNode.Term != 5 {
		t.Errorf("Expected term 5, got %d", updatedNode.Term)
	}

	if updatedNode.LastSeen.Before(beforeUpdate) {
		t.Error("LastSeen should be updated to recent time")
	}

	// Try to update non-existent node
	err = membership.UpdateNodeHeartbeat("node-999", 1, 0, 0)
	if err != ErrNodeNotFound {
		t.Errorf("Expected ErrNodeNotFound, got %v", err)
	}
}

// TestGetHealthyNodes tests health checking
func TestGetHealthyNodes(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Add nodes - AddNode always sets LastSeen to now()
	node2 := NodeInfo{
		ID:   "node-2",
		Addr: "localhost:9091",
		Role: RoleReplica,
	}
	node3 := NodeInfo{
		ID:   "node-3",
		Addr: "localhost:9092",
		Role: RoleReplica,
	}

	membership.AddNode(node2)
	membership.AddNode(node3)

	// Get healthy nodes (last seen within 5 seconds)
	// All nodes should be healthy since they were just added
	healthyNodes := membership.GetHealthyNodes(5 * time.Second)

	// Should include all nodes (local + node-2 + node-3)
	if len(healthyNodes) != 3 {
		t.Errorf("Expected 3 healthy nodes, got %d", len(healthyNodes))
	}

	// Test with very short timeout (1 nanosecond)
	// This won't catch any nodes as healthy in practice
	shortTimeout := 1 * time.Nanosecond
	time.Sleep(2 * time.Microsecond) // Ensure some time passes
	healthyNodesShort := membership.GetHealthyNodes(shortTimeout)

	// Should have 0 nodes with such a short timeout after sleep
	if len(healthyNodesShort) != 0 {
		t.Errorf("Expected 0 healthy nodes with 1ns timeout after sleep, got %d", len(healthyNodesShort))
	}
}

// TestHasQuorum tests quorum checking
func TestHasQuorum(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")
	healthTimeout := 5 * time.Second

	// With only local node and quorum size 2, should not have quorum
	if membership.HasQuorum(2, healthTimeout) {
		t.Error("Should not have quorum with 1 node and quorum size 2")
	}

	// Add another node
	node2 := NodeInfo{ID: "node-2", Addr: "localhost:9091", Role: RoleReplica}
	membership.AddNode(node2)

	// Now should have quorum
	if !membership.HasQuorum(2, healthTimeout) {
		t.Error("Should have quorum with 2 nodes and quorum size 2")
	}

	// Add third node - will be healthy since AddNode sets LastSeen to now()
	node3 := NodeInfo{ID: "node-3", Addr: "localhost:9092", Role: RoleReplica}
	membership.AddNode(node3)

	// Should have quorum of 3 (all 3 nodes are healthy)
	if !membership.HasQuorum(3, healthTimeout) {
		t.Error("Should have quorum with 3 healthy nodes and quorum size 3")
	}

	// Should not have quorum requiring 4 nodes
	if membership.HasQuorum(4, healthTimeout) {
		t.Error("Should not have quorum requiring 4 nodes when only 3 exist")
	}
}

// TestConcurrentAccess tests thread safety
func TestConcurrentAccess(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Concurrent adds
	done := make(chan bool, 100)
	for i := 0; i < 50; i++ {
		go func(id int) {
			node := NodeInfo{
				ID:   nodeID(id),
				Addr: addr(id),
				Role: RoleReplica,
			}
			membership.AddNode(node)
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		go func() {
			_ = membership.GetAllNodes()
			_ = membership.GetHealthyNodes(5 * time.Second)
			_ = membership.GetLocalNode()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// Verify state is consistent
	allNodes := membership.GetAllNodes()
	if len(allNodes) < 1 {
		t.Error("Should have at least local node after concurrent operations")
	}
}

// TestLSNTracking tests LSN tracking for nodes
func TestLSNTracking(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Update local node LSN
	membership.SetLocalLSN(1000)
	if membership.GetLocalNode().LastLSN != 1000 {
		t.Errorf("Expected LSN 1000, got %d", membership.GetLocalNode().LastLSN)
	}

	// Add replica and update its LSN
	node2 := NodeInfo{ID: "node-2", Addr: "localhost:9091", Role: RoleReplica}
	membership.AddNode(node2)

	membership.UpdateNodeLSN("node-2", 950)
	node, _ := membership.GetNode("node-2")
	if node.LastLSN != 950 {
		t.Errorf("Expected node-2 LSN 950, got %d", node.LastLSN)
	}
}

// TestTermManagement tests term tracking
func TestTermManagement(t *testing.T) {
	membership := NewClusterMembership("node-1", "localhost:9090")

	// Set local term
	membership.SetLocalTerm(10)
	if membership.GetLocalNode().Term != 10 {
		t.Errorf("Expected term 10, got %d", membership.GetLocalNode().Term)
	}
}

// Helper functions
func nodeID(i int) string {
	return "node-" + string(rune('0'+i))
}

func addr(i int) string {
	return "localhost:909" + string(rune('0'+i))
}
