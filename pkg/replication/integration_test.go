package replication

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// TestPrimaryReplicaConnection tests basic primary-replica connection
func TestPrimaryReplicaConnection(t *testing.T) {
	// Create primary storage
	primaryStore := newTestStorage(t)
	defer primaryStore.Close()

	// Create replica storage
	replicaStore := newTestStorage(t)
	defer replicaStore.Close()

	// Start primary
	primaryConfig := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        "127.0.0.1:0", // Random port
		MaxReplicas:       5,
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    500 * time.Millisecond,
		WALBufferSize:     100,
	}

	primary := NewReplicationManager(primaryConfig, primaryStore)
	if err := primary.Start(); err != nil {
		t.Fatalf("Failed to start primary: %v", err)
	}
	defer primary.Stop()

	// Get the actual address primary is listening on
	primaryAddr := primary.listener.Addr().String()

	// Start replica
	replicaConfig := ReplicationConfig{
		IsPrimary:         false,
		PrimaryAddr:       primaryAddr,
		ReplicaID:         "test-replica-1",
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    500 * time.Millisecond,
		WALBufferSize:     100,
	}

	replica := NewReplicaNode(replicaConfig, replicaStore)
	if err := replica.Start(); err != nil {
		t.Fatalf("Failed to start replica: %v", err)
	}
	defer replica.Stop()

	// Wait for connection to establish
	time.Sleep(500 * time.Millisecond)

	// Check replica is connected
	if !replica.isConnected() {
		t.Error("Replica should be connected to primary")
	}

	// Check primary has 1 replica
	state := primary.GetReplicationState()
	if state.ReplicaCount != 1 {
		t.Errorf("Expected 1 replica, got %d", state.ReplicaCount)
	}
}

// TestPrimaryReplicaWALStreaming tests WAL entry streaming from primary to replica
func TestPrimaryReplicaWALStreaming(t *testing.T) {
	// Create storages
	primaryStore := newTestStorage(t)
	defer primaryStore.Close()

	replicaStore := newTestStorage(t)
	defer replicaStore.Close()

	// Start primary
	primaryConfig := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       5,
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    500 * time.Millisecond,
		WALBufferSize:     100,
		SyncMode:          SyncModeAsync,
	}

	primary := NewReplicationManager(primaryConfig, primaryStore)
	if err := primary.Start(); err != nil {
		t.Fatalf("Failed to start primary: %v", err)
	}
	defer primary.Stop()

	primaryAddr := primary.listener.Addr().String()

	// Start replica
	replicaConfig := ReplicationConfig{
		IsPrimary:         false,
		PrimaryAddr:       primaryAddr,
		ReplicaID:         "test-replica-wal",
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    500 * time.Millisecond,
		WALBufferSize:     100,
	}

	replica := NewReplicaNode(replicaConfig, replicaStore)
	if err := replica.Start(); err != nil {
		t.Fatalf("Failed to start replica: %v", err)
	}
	defer replica.Stop()

	// Wait for connection
	time.Sleep(500 * time.Millisecond)

	// Create a node on primary (this will generate a WAL entry)
	node, err := primaryStore.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Create WAL entry
	nodeData, _ := json.Marshal(node)
	walEntry := &wal.Entry{
		LSN:       1,
		OpType:    wal.OpCreateNode,
		Timestamp: time.Now().UnixNano(),
		Data:      nodeData,
	}

	// Stream to replicas
	primary.StreamWALEntry(walEntry)

	// Wait for replication with retries (network can be slow)
	var stats storage.Statistics
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		stats = replicaStore.GetStatistics()
		if stats.NodeCount >= 1 {
			break
		}
	}

	// Verify replica received the WAL entry
	// Replica should have applied it and have at least 1 node
	if stats.NodeCount < 1 {
		t.Errorf("Expected replica to have at least 1 node, got %d", stats.NodeCount)
	}
}

// TestPrimaryReplicaHeartbeat tests heartbeat exchange
func TestPrimaryReplicaHeartbeat(t *testing.T) {
	// Create storages
	primaryStore := newTestStorage(t)
	defer primaryStore.Close()

	replicaStore := newTestStorage(t)
	defer replicaStore.Close()

	// Start primary with short heartbeat interval
	primaryConfig := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       5,
		HeartbeatInterval: 50 * time.Millisecond,
		ReconnectDelay:    500 * time.Millisecond,
		WALBufferSize:     100,
	}

	primary := NewReplicationManager(primaryConfig, primaryStore)
	if err := primary.Start(); err != nil {
		t.Fatalf("Failed to start primary: %v", err)
	}
	defer primary.Stop()

	primaryAddr := primary.listener.Addr().String()

	// Start replica
	replicaConfig := ReplicationConfig{
		IsPrimary:         false,
		PrimaryAddr:       primaryAddr,
		ReplicaID:         "test-replica-hb",
		HeartbeatInterval: 50 * time.Millisecond,
		ReconnectDelay:    500 * time.Millisecond,
		WALBufferSize:     100,
	}

	replica := NewReplicaNode(replicaConfig, replicaStore)
	if err := replica.Start(); err != nil {
		t.Fatalf("Failed to start replica: %v", err)
	}
	defer replica.Stop()

	// Wait for heartbeats to be exchanged
	time.Sleep(300 * time.Millisecond)

	// Get replica status to check heartbeat sequence
	status := replica.GetReplicaStatus()
	if status.LastHeartbeatSeq == 0 {
		t.Error("Expected replica to have received heartbeats")
	}

	// Check primary has replica info
	state := primary.GetReplicationState()
	if state.ReplicaCount == 0 {
		t.Error("Primary should have at least one replica")
	}
}

// TestMultipleReplicas tests primary with multiple replicas
func TestMultipleReplicas(t *testing.T) {
	// Create storages
	primaryStore := newTestStorage(t)
	defer primaryStore.Close()

	// Start primary
	primaryConfig := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       5,
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    500 * time.Millisecond,
		WALBufferSize:     100,
	}

	primary := NewReplicationManager(primaryConfig, primaryStore)
	if err := primary.Start(); err != nil {
		t.Fatalf("Failed to start primary: %v", err)
	}
	defer primary.Stop()

	primaryAddr := primary.listener.Addr().String()

	// Start 3 replicas
	replicas := make([]*ReplicaNode, 3)
	for i := 0; i < 3; i++ {
		replicaStore := newTestStorage(t)
		defer replicaStore.Close()

		replicaConfig := ReplicationConfig{
			IsPrimary:         false,
			PrimaryAddr:       primaryAddr,
			ReplicaID:         "", // Will be auto-generated
			HeartbeatInterval: 100 * time.Millisecond,
			ReconnectDelay:    500 * time.Millisecond,
			WALBufferSize:     100,
		}

		replica := NewReplicaNode(replicaConfig, replicaStore)
		if err := replica.Start(); err != nil {
			t.Fatalf("Failed to start replica %d: %v", i, err)
		}
		defer replica.Stop()

		replicas[i] = replica
	}

	// Wait for all connections
	time.Sleep(1 * time.Second)

	// Check primary has 3 replicas
	state := primary.GetReplicationState()
	if state.ReplicaCount != 3 {
		t.Errorf("Expected 3 replicas, got %d", state.ReplicaCount)
	}

	// Check all replicas are connected
	for i, replica := range replicas {
		if !replica.isConnected() {
			t.Errorf("Replica %d should be connected", i)
		}
	}
}

// TestReplicaReconnection tests replica reconnecting after disconnect
func TestReplicaReconnection(t *testing.T) {
	// Create storages
	primaryStore := newTestStorage(t)
	defer primaryStore.Close()

	replicaStore := newTestStorage(t)
	defer replicaStore.Close()

	// Start primary
	primaryConfig := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       5,
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    200 * time.Millisecond,
		WALBufferSize:     100,
	}

	primary := NewReplicationManager(primaryConfig, primaryStore)
	if err := primary.Start(); err != nil {
		t.Fatalf("Failed to start primary: %v", err)
	}
	defer primary.Stop()

	primaryAddr := primary.listener.Addr().String()

	// Start replica
	replicaConfig := ReplicationConfig{
		IsPrimary:         false,
		PrimaryAddr:       primaryAddr,
		ReplicaID:         "test-replica-reconnect",
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    200 * time.Millisecond,
		WALBufferSize:     100,
	}

	replica := NewReplicaNode(replicaConfig, replicaStore)
	if err := replica.Start(); err != nil {
		t.Fatalf("Failed to start replica: %v", err)
	}
	defer replica.Stop()

	// Wait for connection
	time.Sleep(500 * time.Millisecond)

	if !replica.isConnected() {
		t.Fatal("Replica should be connected initially")
	}

	// Forcefully disconnect replica
	replica.disconnect()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Replica should not be connected
	if replica.isConnected() {
		t.Error("Replica should be disconnected after manual disconnect")
	}

	// Wait for reconnection (ReconnectDelay + connection time)
	time.Sleep(800 * time.Millisecond)

	// Replica should have reconnected
	if !replica.isConnected() {
		t.Error("Replica should have reconnected automatically")
	}
}

// TestDirectMessageHandling tests message handling functions directly
func TestDirectMessageHandling(t *testing.T) {
	// Create test replica
	replicaStore := newTestStorage(t)
	defer replicaStore.Close()

	replicaConfig := ReplicationConfig{
		IsPrimary:         false,
		PrimaryAddr:       "127.0.0.1:9999", // Doesn't matter for this test
		ReplicaID:         "test-replica-msg",
		HeartbeatInterval: 100 * time.Millisecond,
		WALBufferSize:     100,
	}

	replica := NewReplicaNode(replicaConfig, replicaStore)

	// Test handleMessage with heartbeat
	hbMsg := HeartbeatMessage{
		From:       "primary-1",
		Sequence:   1,
		CurrentLSN: 100,
		NodeCount:  10,
		EdgeCount:  20,
	}

	msg, err := NewMessage(MsgHeartbeat, hbMsg)
	if err != nil {
		t.Fatalf("Failed to create heartbeat message: %v", err)
	}

	err = replica.handleMessage(msg)
	if err != nil {
		t.Errorf("handleMessage should not fail for heartbeat: %v", err)
	}

	// Test handleMessage with WAL entry
	node := &storage.Node{
		ID:     1,
		Labels: []string{"Person"},
		Properties: map[string]storage.Value{
			"name": storage.StringValue("Bob"),
		},
	}
	nodeData, _ := json.Marshal(node)

	walEntry := &wal.Entry{
		LSN:       1,
		OpType:    wal.OpCreateNode,
		Timestamp: time.Now().UnixNano(),
		Data:      nodeData,
	}

	walMsg := WALEntryMessage{
		Entry: walEntry,
	}

	msg, err = NewMessage(MsgWALEntry, walMsg)
	if err != nil {
		t.Fatalf("Failed to create WAL message: %v", err)
	}

	err = replica.handleMessage(msg)
	if err != nil {
		t.Errorf("handleMessage should not fail for WAL entry: %v", err)
	}

	// Verify node was created
	stats := replicaStore.GetStatistics()
	if stats.NodeCount == 0 {
		t.Error("Expected node to be created from WAL entry")
	}

	// Test handleMessage with snapshot
	snapMsg := SnapshotMessage{
		SnapshotID: "snapshot-123",
		Size:       1024,
		Compressed: true,
	}

	msg, err = NewMessage(MsgSnapshot, snapMsg)
	if err != nil {
		t.Fatalf("Failed to create snapshot message: %v", err)
	}

	err = replica.handleMessage(msg)
	// Should not fail (currently just logs)
	if err != nil {
		t.Errorf("handleMessage should not fail for snapshot: %v", err)
	}

	// Test handleMessage with error
	errMsg := ErrorMessage{
		Code:    "ERR_TEST",
		Message: "Test error",
		Fatal:   false,
	}

	msg, err = NewMessage(MsgError, errMsg)
	if err != nil {
		t.Fatalf("Failed to create error message: %v", err)
	}

	err = replica.handleMessage(msg)
	// Should return an error
	if err == nil {
		t.Error("handleMessage should return error for error message")
	}
}

// TestHandshakeRejection tests replica handshake rejection
func TestHandshakeRejection(t *testing.T) {
	// Create a mock primary server that rejects handshakes
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		decoder := json.NewDecoder(conn)
		encoder := json.NewEncoder(conn)

		// Receive handshake request
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			return
		}

		// Send rejection
		response := HandshakeResponse{
			PrimaryID:    "mock-primary",
			CurrentLSN:   0,
			Version:      "1.0",
			Accepted:     false,
			ErrorMessage: "Test rejection",
		}

		respMsg, _ := NewMessage(MsgHandshake, response)
		encoder.Encode(respMsg)
	}()

	// Create replica
	replicaStore := newTestStorage(t)
	defer replicaStore.Close()

	replicaConfig := ReplicationConfig{
		IsPrimary:         false,
		PrimaryAddr:       listener.Addr().String(),
		ReplicaID:         "test-replica-reject",
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    1 * time.Second,
		WALBufferSize:     100,
	}

	replica := NewReplicaNode(replicaConfig, replicaStore)
	if err := replica.Start(); err != nil {
		t.Fatalf("Failed to start replica: %v", err)
	}
	defer replica.Stop()

	// Wait for connection attempt
	time.Sleep(500 * time.Millisecond)

	// Replica should not be connected (handshake was rejected)
	if replica.isConnected() {
		t.Error("Replica should not be connected after handshake rejection")
	}
}

// TestCalculateLagLSN tests LSN lag calculation
func TestCalculateLagLSN(t *testing.T) {
	replicaStore := newTestStorage(t)
	defer replicaStore.Close()

	replicaConfig := ReplicationConfig{
		IsPrimary:   false,
		PrimaryAddr: "127.0.0.1:9999",
		ReplicaID:   "test-replica-lag",
	}

	replica := NewReplicaNode(replicaConfig, replicaStore)

	// Replica at LSN 50, primary at LSN 100
	replica.lastAppliedLSN = 50
	lag := replica.CalculateLagLSN(100)
	if lag != 50 {
		t.Errorf("Expected lag 50, got %d", lag)
	}

	// Replica caught up
	replica.lastAppliedLSN = 100
	lag = replica.CalculateLagLSN(100)
	if lag != 0 {
		t.Errorf("Expected lag 0, got %d", lag)
	}

	// Replica ahead (shouldn't happen, but handle it)
	replica.lastAppliedLSN = 150
	lag = replica.CalculateLagLSN(100)
	if lag != 0 {
		t.Errorf("Expected lag 0 when ahead, got %d", lag)
	}
}
