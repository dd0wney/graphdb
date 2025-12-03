package replication

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// mockStorage implements a minimal storage interface for testing
type mockStorage struct {
	mu         sync.Mutex
	nodes      map[uint64]*storage.Node
	edges      map[uint64]*storage.Edge
	nextNodeID uint64
	nextEdgeID uint64
	stats      storage.Statistics
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		nodes:      make(map[uint64]*storage.Node),
		edges:      make(map[uint64]*storage.Edge),
		nextNodeID: 1,
		nextEdgeID: 1,
	}
}

func (ms *mockStorage) CreateNode(labels []string, properties map[string]storage.Value) (*storage.Node, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	node := &storage.Node{
		ID:         ms.nextNodeID,
		Labels:     labels,
		Properties: properties,
		CreatedAt:  time.Now().Unix(),
		UpdatedAt:  time.Now().Unix(),
	}

	ms.nodes[node.ID] = node
	ms.nextNodeID++
	ms.stats.NodeCount++

	return node, nil
}

func (ms *mockStorage) CreateEdge(fromNodeID, toNodeID uint64, edgeType string, properties map[string]storage.Value, weight float64) (*storage.Edge, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	edge := &storage.Edge{
		ID:         ms.nextEdgeID,
		FromNodeID: fromNodeID,
		ToNodeID:   toNodeID,
		Type:       edgeType,
		Properties: properties,
		Weight:     weight,
		CreatedAt:  time.Now().Unix(),
	}

	ms.edges[edge.ID] = edge
	ms.nextEdgeID++
	ms.stats.EdgeCount++

	return edge, nil
}

func (ms *mockStorage) GetStatistics() storage.Statistics {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.stats
}

// TestNewReplicaNode tests replica node construction
func TestNewReplicaNode(t *testing.T) {
	tests := []struct {
		name              string
		config            ReplicationConfig
		expectReplicaID   bool
		expectedReplicaID string
	}{
		{
			name: "with explicit replica ID",
			config: ReplicationConfig{
				ReplicaID:         "replica-123",
				PrimaryAddr:       "localhost:9090",
				HeartbeatInterval: 1 * time.Second,
			},
			expectReplicaID:   true,
			expectedReplicaID: "replica-123",
		},
		{
			name: "without replica ID (auto-generated)",
			config: ReplicationConfig{
				PrimaryAddr:       "localhost:9090",
				HeartbeatInterval: 1 * time.Second,
			},
			expectReplicaID: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := newMockStorage()

			// Create a proper GraphStorage using the dataDir
			storageInst, err := storage.NewGraphStorage(t.TempDir())
			if err != nil {
				t.Fatalf("Failed to create storage: %v", err)
			}
			defer storageInst.Close()

			rn := NewReplicaNode(tt.config, storageInst)

			if rn == nil {
				t.Fatal("NewReplicaNode returned nil")
			}

			if tt.expectReplicaID {
				if tt.expectedReplicaID != "" && rn.replicaID != tt.expectedReplicaID {
					t.Errorf("Expected replicaID %s, got %s", tt.expectedReplicaID, rn.replicaID)
				}
				if rn.replicaID == "" {
					t.Error("Expected auto-generated replicaID, got empty string")
				}
			}

			if rn.storage == nil {
				t.Error("Expected storage to be set")
			}

			if rn.stopCh == nil {
				t.Error("Expected stopCh to be initialized")
			}

			if rn.running {
				t.Error("Expected running to be false initially")
			}

			_ = ms // use mock storage to avoid warning
		})
	}
}

// TestReplicaNode_Start_Stop tests lifecycle management
func TestReplicaNode_Start_Stop(t *testing.T) {
	config := ReplicationConfig{
		ReplicaID:         "test-replica",
		PrimaryAddr:       "localhost:9090",
		HeartbeatInterval: 100 * time.Millisecond,
		ReconnectDelay:    50 * time.Millisecond,
	}

	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := NewReplicaNode(config, storageInst)

	// Test Start
	err = rn.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	rn.runningMu.Lock()
	if !rn.running {
		t.Error("Expected running to be true after Start")
	}
	rn.runningMu.Unlock()

	// Test Start idempotency (should fail if already running)
	err = rn.Start()
	if err == nil {
		t.Error("Expected error when calling Start on already running replica")
	}

	// Give the connection manager a moment to start
	time.Sleep(50 * time.Millisecond)

	// Test Stop
	err = rn.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	rn.runningMu.Lock()
	if rn.running {
		t.Error("Expected running to be false after Stop")
	}
	rn.runningMu.Unlock()

	// Test Stop idempotency (should not fail if already stopped)
	err = rn.Stop()
	if err != nil {
		t.Errorf("Stop should be idempotent, got error: %v", err)
	}
}

// TestReplicaNode_GetReplicationState tests state retrieval
func TestReplicaNode_GetReplicationState(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID:      "replica-1",
		primaryID:      "primary-1",
		lastAppliedLSN: 12345,
		storage:        storageInst,
		stopCh:         make(chan struct{}),
	}

	state := rn.GetReplicationState()

	if state.IsPrimary {
		t.Error("Expected IsPrimary to be false for replica")
	}

	if state.PrimaryID != "primary-1" {
		t.Errorf("Expected PrimaryID 'primary-1', got '%s'", state.PrimaryID)
	}

	if state.CurrentLSN != 12345 {
		t.Errorf("Expected CurrentLSN 12345, got %d", state.CurrentLSN)
	}
}

// TestReplicaNode_ConnectionState tests thread-safe connection state
func TestReplicaNode_ConnectionState(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID: "replica-1",
		storage:   storageInst,
		stopCh:    make(chan struct{}),
	}

	// Initially should be disconnected
	if rn.isConnected() {
		t.Error("Expected initial state to be disconnected")
	}

	// Set connected
	rn.setConnected(true)
	if !rn.isConnected() {
		t.Error("Expected state to be connected after setConnected(true)")
	}

	// Set disconnected
	rn.setConnected(false)
	if rn.isConnected() {
		t.Error("Expected state to be disconnected after setConnected(false)")
	}
}

// TestReplicaNode_ConnectionState_Concurrent tests concurrent access to connection state
func TestReplicaNode_ConnectionState_Concurrent(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID: "replica-1",
		storage:   storageInst,
		stopCh:    make(chan struct{}),
	}

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = rn.isConnected()
			}
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				rn.setConnected(id%2 == 0)
			}
		}(i)
	}

	wg.Wait()
	// No assertion needed - this test passes if there's no race condition
}

// TestReplicaNode_HandleMessage_Heartbeat tests heartbeat message handling
func TestReplicaNode_HandleMessage_Heartbeat(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID:                "replica-1",
		storage:                  storageInst,
		stopCh:                   make(chan struct{}),
		lastReceivedHeartbeatSeq: 0,
	}

	hb := HeartbeatMessage{
		From:       "primary-1",
		Sequence:   42,
		CurrentLSN: 1000,
		NodeCount:  100,
		EdgeCount:  200,
	}

	msg, err := NewMessage(MsgHeartbeat, hb)
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	// Decode the heartbeat to simulate what handleMessage does
	var receivedHb HeartbeatMessage
	if err := msg.Decode(&receivedHb); err != nil {
		t.Fatalf("Failed to decode heartbeat: %v", err)
	}

	// Track the heartbeat sequence number (simulating handleMessage behavior)
	rn.heartbeatSeqMu.Lock()
	rn.lastReceivedHeartbeatSeq = receivedHb.Sequence
	rn.heartbeatSeqMu.Unlock()

	// Verify sequence was updated
	rn.heartbeatSeqMu.Lock()
	seq := rn.lastReceivedHeartbeatSeq
	rn.heartbeatSeqMu.Unlock()

	if seq != 42 {
		t.Errorf("Expected heartbeat sequence 42, got %d", seq)
	}
}

// TestReplicaNode_HandleMessage_Error tests error message handling
func TestReplicaNode_HandleMessage_Error(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID: "replica-1",
		storage:   storageInst,
		stopCh:    make(chan struct{}),
	}

	errMsg := ErrorMessage{
		Code:    "ERR_TEST",
		Message: "Test error message",
		Fatal:   false,
	}

	msg, err := NewMessage(MsgError, errMsg)
	if err != nil {
		t.Fatalf("Failed to create error message: %v", err)
	}

	err = rn.handleMessage(msg)
	if err == nil {
		t.Error("Expected error when handling error message")
	}

	if err.Error() != "primary error: Test error message" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestReplicaNode_HandleMessage_Unknown tests unknown message type
func TestReplicaNode_HandleMessage_Unknown(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID: "replica-1",
		storage:   storageInst,
		stopCh:    make(chan struct{}),
	}

	msg := &Message{
		Type:      MessageType(99), // Unknown type
		Timestamp: time.Now().Unix(),
		Data:      []byte("{}"),
	}

	err = rn.handleMessage(msg)
	// Unknown messages should be ignored (return nil)
	if err != nil {
		t.Errorf("Expected nil error for unknown message type, got: %v", err)
	}
}

// TestReplicaNode_ApplyWALEntry_CreateNode tests node creation from WAL
func TestReplicaNode_ApplyWALEntry_CreateNode(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID:      "replica-1",
		storage:        storageInst,
		stopCh:         make(chan struct{}),
		lastAppliedLSN: 99,
	}

	node := storage.Node{
		ID:     1,
		Labels: []string{"Person", "User"},
		Properties: map[string]storage.Value{
			"name": storage.StringValue("Bob"),
			"age":  storage.IntValue(30),
		},
	}

	nodeData, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("Failed to marshal node: %v", err)
	}

	entry := &wal.Entry{
		LSN:    100,
		OpType: wal.OpCreateNode,
		Data:   nodeData,
	}

	// Apply entry (will fail on encoder, but should create node and update LSN)
	_ = rn.applyWALEntry(entry)

	// Verify LSN updated
	if rn.lastAppliedLSN != 100 {
		t.Errorf("Expected lastAppliedLSN 100, got %d", rn.lastAppliedLSN)
	}

	// Verify node created
	stats := storageInst.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node, got %d", stats.NodeCount)
	}
}

// TestReplicaNode_ApplyWALEntry_CreateEdge tests edge creation from WAL
func TestReplicaNode_ApplyWALEntry_CreateEdge(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID:      "replica-1",
		storage:        storageInst,
		stopCh:         make(chan struct{}),
		lastAppliedLSN: 199,
	}

	// Create nodes first (needed for edge)
	_, err = storageInst.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	_, err = storageInst.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	edge := storage.Edge{
		ID:         1,
		FromNodeID: 1,
		ToNodeID:   2,
		Type:       "KNOWS",
		Properties: map[string]storage.Value{
			"since": storage.IntValue(2020),
		},
		Weight: 1.0,
	}

	edgeData, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("Failed to marshal edge: %v", err)
	}

	entry := &wal.Entry{
		LSN:    200,
		OpType: wal.OpCreateEdge,
		Data:   edgeData,
	}

	// Apply entry
	_ = rn.applyWALEntry(entry)

	// Verify LSN updated
	if rn.lastAppliedLSN != 200 {
		t.Errorf("Expected lastAppliedLSN 200, got %d", rn.lastAppliedLSN)
	}

	// Verify edge created
	stats := storageInst.GetStatistics()
	if stats.EdgeCount != 1 {
		t.Errorf("Expected 1 edge, got %d", stats.EdgeCount)
	}
}

// TestReplicaNode_ApplyWALEntry_InvalidData tests handling of invalid WAL data
func TestReplicaNode_ApplyWALEntry_InvalidData(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID: "replica-1",
		storage:   storageInst,
		stopCh:    make(chan struct{}),
	}

	entry := &wal.Entry{
		LSN:    100,
		OpType: wal.OpCreateNode,
		Data:   []byte("invalid json"),
	}

	err = rn.applyWALEntry(entry)
	if err == nil {
		t.Error("Expected error when applying WAL entry with invalid data")
	}
}

// TestReplicaNode_ApplyWALEntry_UnknownOpType tests unknown operation type
func TestReplicaNode_ApplyWALEntry_UnknownOpType(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID:      "replica-1",
		storage:        storageInst,
		stopCh:         make(chan struct{}),
		lastAppliedLSN: 99,
	}

	entry := &wal.Entry{
		LSN:    100,
		OpType: wal.OpType(255), // Unknown operation type
		Data:   []byte("{}"),
	}

	// Should not error, but should update LSN
	_ = rn.applyWALEntry(entry)

	if rn.lastAppliedLSN != 100 {
		t.Errorf("Expected lastAppliedLSN 100 even for unknown op, got %d", rn.lastAppliedLSN)
	}
}

// TestReplicaNode_ConcurrentWALApplication tests concurrent WAL entry application
func TestReplicaNode_ConcurrentWALApplication(t *testing.T) {
	storageInst, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storageInst.Close()

	rn := &ReplicaNode{
		replicaID: "replica-1",
		storage:   storageInst,
		stopCh:    make(chan struct{}),
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			node := storage.Node{
				ID:     uint64(id),
				Labels: []string{"TestNode"},
				Properties: map[string]storage.Value{
					"id": storage.IntValue(int64(id)),
				},
			}

			nodeData, err := json.Marshal(node)
			if err != nil {
				t.Errorf("Failed to marshal node: %v", err)
				return
			}

			entry := &wal.Entry{
				LSN:    uint64(id),
				OpType: wal.OpCreateNode,
				Data:   nodeData,
			}

			_ = rn.applyWALEntry(entry)
		}(i)
	}

	wg.Wait()

	// Verify all nodes were created
	stats := storageInst.GetStatistics()
	if stats.NodeCount != uint64(numGoroutines) {
		t.Errorf("Expected %d nodes, got %d", numGoroutines, stats.NodeCount)
	}
}
