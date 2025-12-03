package cluster

import (
	"testing"
	"time"
)

// TestFullClusterSetup tests setting up a complete 3-node cluster
func TestFullClusterSetup(t *testing.T) {
	// Create 3 nodes with different configurations
	configs := []ClusterConfig{
		{
			NodeID:             "node-1",
			NodeAddr:           "localhost:9090",
			SeedNodes:          []string{"localhost:9090", "localhost:9091", "localhost:9092"},
			ElectionTimeout:    2 * time.Second,
			HeartbeatInterval:  500 * time.Millisecond,
			MinQuorumSize:      2,
			Priority:           1,
			EnableAutoFailover: true,
			VoteRequestTimeout: 1 * time.Second,
		},
		{
			NodeID:             "node-2",
			NodeAddr:           "localhost:9091",
			SeedNodes:          []string{"localhost:9090", "localhost:9091", "localhost:9092"},
			ElectionTimeout:    2 * time.Second,
			HeartbeatInterval:  500 * time.Millisecond,
			MinQuorumSize:      2,
			Priority:           1,
			EnableAutoFailover: true,
			VoteRequestTimeout: 1 * time.Second,
		},
		{
			NodeID:             "node-3",
			NodeAddr:           "localhost:9092",
			SeedNodes:          []string{"localhost:9090", "localhost:9091", "localhost:9092"},
			ElectionTimeout:    2 * time.Second,
			HeartbeatInterval:  500 * time.Millisecond,
			MinQuorumSize:      2,
			Priority:           1,
			EnableAutoFailover: true,
			VoteRequestTimeout: 1 * time.Second,
		},
	}

	// Create membership and election managers for each node
	memberships := make([]*ClusterMembership, 3)
	elections := make([]*ElectionManager, 3)

	for i, config := range configs {
		memberships[i] = NewClusterMembership(config.NodeID, config.NodeAddr)
		elections[i] = NewElectionManager(config, memberships[i])
	}

	// Verify all nodes created successfully
	for i, election := range elections {
		if election == nil {
			t.Fatalf("Failed to create election manager for node %d", i)
		}
		if election.GetState() != StateFollower {
			t.Errorf("Node %d should start as follower", i)
		}
	}

	// Verify each node knows its own ID
	for i, membership := range memberships {
		localNode := membership.GetLocalNode()
		if localNode.ID != configs[i].NodeID {
			t.Errorf("Node %d has wrong ID: expected %s, got %s",
				i, configs[i].NodeID, localNode.ID)
		}
	}
}

// TestElectionWithQuorum tests election succeeds with quorum
func TestElectionWithQuorum(t *testing.T) {
	config := ClusterConfig{
		NodeID:             "node-1",
		NodeAddr:           "localhost:9090",
		ElectionTimeout:    1 * time.Second,
		HeartbeatInterval:  300 * time.Millisecond,
		MinQuorumSize:      2,
		Priority:           1,
		EnableAutoFailover: true,
		VoteRequestTimeout: 500 * time.Millisecond,
	}

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)

	// Add 2 other nodes to reach quorum of 3 total
	membership.AddNode(NodeInfo{
		ID:   "node-2",
		Addr: "localhost:9091",
		Role: RoleReplica,
	})
	membership.AddNode(NodeInfo{
		ID:   "node-3",
		Addr: "localhost:9092",
		Role: RoleReplica,
	})

	election := NewElectionManager(config, membership)

	// Verify we have quorum
	if !membership.HasQuorum(2, 5*time.Second) {
		t.Fatal("Should have quorum with 3 healthy nodes")
	}

	// Start election
	err := election.StartElection()
	if err != nil {
		t.Fatalf("Failed to start election: %v", err)
	}

	// Should be candidate
	if election.GetState() != StateCandidate {
		t.Error("Should be candidate after starting election")
	}

	// Term should be 1
	if election.GetCurrentTerm() != 1 {
		t.Errorf("Expected term 1, got %d", election.GetCurrentTerm())
	}
}

// TestMembershipDiscoveryIntegration tests node discovery integration
func TestMembershipDiscoveryIntegration(t *testing.T) {
	config1 := ClusterConfig{
		NodeID:   "node-1",
		NodeAddr: "localhost:9090",
	}
	config2 := ClusterConfig{
		NodeID:   "node-2",
		NodeAddr: "localhost:9091",
	}

	membership1 := NewClusterMembership(config1.NodeID, config1.NodeAddr)
	membership2 := NewClusterMembership(config2.NodeID, config2.NodeAddr)

	// Simulate node-1 learning about node-2 via discovery
	node2Info := NodeInfo{
		ID:       config2.NodeID,
		Addr:     config2.NodeAddr,
		Role:     RoleReplica,
		Epoch:    0,
		Term:     0,
		Priority: 1,
	}

	err := membership1.AddNode(node2Info)
	if err != nil {
		t.Fatalf("Failed to add node-2 to node-1's membership: %v", err)
	}

	// Verify node-1 now knows about node-2
	allNodes := membership1.GetAllNodes()
	if len(allNodes) != 2 {
		t.Errorf("Expected 2 nodes in membership1, got %d", len(allNodes))
	}

	// Simulate node-2 learning about node-1
	node1Info := NodeInfo{
		ID:       config1.NodeID,
		Addr:     config1.NodeAddr,
		Role:     RoleReplica,
		Epoch:    0,
		Term:     0,
		Priority: 1,
	}

	err = membership2.AddNode(node1Info)
	if err != nil {
		t.Fatalf("Failed to add node-1 to node-2's membership: %v", err)
	}

	// Verify bidirectional knowledge
	allNodes2 := membership2.GetAllNodes()
	if len(allNodes2) != 2 {
		t.Errorf("Expected 2 nodes in membership2, got %d", len(allNodes2))
	}
}

// TestEpochFencing tests epoch-based split-brain prevention
func TestEpochFencing(t *testing.T) {
	config := ClusterConfig{
		NodeID:   "node-1",
		NodeAddr: "localhost:9090",
	}

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	election := NewElectionManager(config, membership)

	// Initial epoch should be 0
	if membership.GetEpoch() != 0 {
		t.Errorf("Expected initial epoch 0, got %d", membership.GetEpoch())
	}

	// Simulate becoming leader and incrementing epoch
	newEpoch := membership.IncrementEpoch()
	if newEpoch != 1 {
		t.Errorf("Expected epoch 1 after increment, got %d", newEpoch)
	}

	// Simulate another node with higher epoch (e.g., from split brain recovery)
	higherEpochNode := NodeInfo{
		ID:    "node-2",
		Addr:  "localhost:9091",
		Role:  RolePrimary,
		Epoch: 5, // Much higher epoch
		Term:  10,
	}

	membership.AddNode(higherEpochNode)

	// In production, seeing higher epoch should trigger step-down
	// Verify we can detect the epoch difference
	node2, _ := membership.GetNode("node-2")
	if node2.Epoch <= membership.GetEpoch() {
		t.Error("Node-2 should have higher epoch than local node")
	}

	// Verify term comparison
	if node2.Term <= election.GetCurrentTerm() {
		t.Error("Node-2 should have higher term than local node")
	}
}

// TestPriorityBasedElection tests that higher priority nodes are preferred
func TestPriorityBasedElection(t *testing.T) {
	// Create two nodes with different priorities
	config1 := ClusterConfig{
		NodeID:   "node-1",
		NodeAddr: "localhost:9090",
		Priority: 1, // Lower priority
	}

	config2 := ClusterConfig{
		NodeID:   "node-2",
		NodeAddr: "localhost:9091",
		Priority: 10, // Higher priority
	}

	membership1 := NewClusterMembership(config1.NodeID, config1.NodeAddr)
	membership2 := NewClusterMembership(config2.NodeID, config2.NodeAddr)

	election1 := NewElectionManager(config1, membership1)
	election2 := NewElectionManager(config2, membership2)

	// Both nodes know about each other
	membership1.AddNode(NodeInfo{
		ID:       config2.NodeID,
		Addr:     config2.NodeAddr,
		Role:     RoleReplica,
		Priority: config2.Priority,
	})

	membership2.AddNode(NodeInfo{
		ID:       config1.NodeID,
		Addr:     config1.NodeAddr,
		Role:     RoleReplica,
		Priority: config1.Priority,
	})

	// In production, node-2 with higher priority would be more likely to become leader
	// For this test, we just verify the priority is tracked correctly
	node2Info, _ := membership1.GetNode("node-2")
	node1Info, _ := membership2.GetNode("node-1")

	if node2Info.Priority != 10 {
		t.Errorf("Expected node-2 priority 10, got %d", node2Info.Priority)
	}

	if node1Info.Priority != 1 {
		t.Errorf("Expected node-1 priority 1, got %d", node1Info.Priority)
	}

	// Verify election managers are initialized
	if election1 == nil || election2 == nil {
		t.Fatal("Election managers should be initialized")
	}
}

// TestRoleTransitions tests state transitions during election
func TestRoleTransitions(t *testing.T) {
	config := ClusterConfig{
		NodeID:             "node-1",
		NodeAddr:           "localhost:9090",
		ElectionTimeout:    1 * time.Second,
		HeartbeatInterval:  300 * time.Millisecond,
		MinQuorumSize:      1,
		EnableAutoFailover: true,
	}

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	election := NewElectionManager(config, membership)

	// Start as follower
	if membership.GetLocalNode().Role != RoleReplica {
		t.Error("Should start with RoleReplica")
	}

	if election.GetState() != StateFollower {
		t.Error("Should start in StateFollower")
	}

	// Start election
	election.StartElection()

	// Should transition to candidate
	if election.GetState() != StateCandidate {
		t.Error("Should be StateCandidate after starting election")
	}

	if membership.GetLocalNode().Role != RoleCandidate {
		t.Error("Role should be RoleCandidate")
	}

	// Step down
	election.StepDown(election.GetCurrentTerm() + 1)

	// Should be back to follower
	if election.GetState() != StateFollower {
		t.Error("Should be StateFollower after stepping down")
	}

	if membership.GetLocalNode().Role != RoleReplica {
		t.Error("Role should be RoleReplica after stepping down")
	}
}

// TestHeartbeatSequenceTracking tests heartbeat sequence number tracking
func TestHeartbeatSequenceTracking(t *testing.T) {
	config := ClusterConfig{
		NodeID:   "node-1",
		NodeAddr: "localhost:9090",
	}

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)

	// Add a remote node
	membership.AddNode(NodeInfo{
		ID:   "node-2",
		Addr: "localhost:9091",
		Role: RoleReplica,
	})

	// Update heartbeat multiple times with increasing sequence numbers
	for seq := uint64(1); seq <= 10; seq++ {
		err := membership.UpdateNodeHeartbeat("node-2", seq, 0, 0)
		if err != nil {
			t.Fatalf("Failed to update heartbeat %d: %v", seq, err)
		}

		node, _ := membership.GetNode("node-2")
		if node.LastHeartbeatSeq != seq {
			t.Errorf("Expected heartbeat seq %d, got %d", seq, node.LastHeartbeatSeq)
		}
	}

	// Verify final sequence number
	node, _ := membership.GetNode("node-2")
	if node.LastHeartbeatSeq != 10 {
		t.Errorf("Expected final heartbeat seq 10, got %d", node.LastHeartbeatSeq)
	}
}

// TestLSNProgression tests LSN tracking during replication
func TestLSNProgression(t *testing.T) {
	config := ClusterConfig{
		NodeID:   "node-1",
		NodeAddr: "localhost:9090",
	}

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)

	// Simulate LSN progression on local node
	lsnSequence := []uint64{10, 20, 30, 40, 50}

	for _, lsn := range lsnSequence {
		membership.SetLocalLSN(lsn)

		if membership.GetLocalNode().LastLSN != lsn {
			t.Errorf("Expected LSN %d, got %d", lsn, membership.GetLocalNode().LastLSN)
		}
	}

	// Add replica and track its LSN
	membership.AddNode(NodeInfo{
		ID:   "replica-1",
		Addr: "localhost:9091",
		Role: RoleReplica,
	})

	// Simulate replica catching up
	replicaLSNs := []uint64{10, 15, 25, 40, 50}
	for _, lsn := range replicaLSNs {
		membership.UpdateNodeLSN("replica-1", lsn)

		node, _ := membership.GetNode("replica-1")
		if node.LastLSN != lsn {
			t.Errorf("Expected replica LSN %d, got %d", lsn, node.LastLSN)
		}
	}

	// Verify replica is caught up
	replicaNode, _ := membership.GetNode("replica-1")
	if replicaNode.LastLSN != membership.GetLocalNode().LastLSN {
		t.Error("Replica should be caught up with primary")
	}
}

// TestClusterHealthMonitoring tests overall cluster health tracking
func TestClusterHealthMonitoring(t *testing.T) {
	config := ClusterConfig{
		NodeID:   "primary",
		NodeAddr: "localhost:9090",
	}

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)

	// Add 5 replicas
	for i := 1; i <= 5; i++ {
		membership.AddNode(NodeInfo{
			ID:   nodeID(i),
			Addr: addr(i),
			Role: RoleReplica,
		})
	}

	// All should be healthy (just added)
	healthyNodes := membership.GetHealthyNodes(5 * time.Second)
	if len(healthyNodes) != 6 { // 5 replicas + 1 local
		t.Errorf("Expected 6 healthy nodes, got %d", len(healthyNodes))
	}

	// Test quorum with all healthy
	if !membership.HasQuorum(4, 5*time.Second) {
		t.Error("Should have quorum of 4 with 6 healthy nodes")
	}

	if !membership.HasQuorum(6, 5*time.Second) {
		t.Error("Should have quorum of 6 with 6 healthy nodes")
	}

	// Should not have quorum requiring 7 nodes
	if membership.HasQuorum(7, 5*time.Second) {
		t.Error("Should not have quorum requiring 7 nodes with only 6")
	}
}
