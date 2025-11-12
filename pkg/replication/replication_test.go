package replication

import (
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// Helper function to create test storage
func newTestStorage(t *testing.T) *storage.GraphStorage {
	store, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	return store
}

// TestReplicationManagerStartStop tests basic start/stop functionality
func TestReplicationManagerStartStop(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0", // Random port
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     100,
	}

	rm := NewReplicationManager(config, store)

	// Start should succeed
	if err := rm.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should succeed
	if err := rm.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestReplicationManagerDoubleStart tests starting twice
func TestReplicationManagerDoubleStart(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     100,
	}

	rm := NewReplicationManager(config, store)

	// First start should succeed
	if err := rm.Start(); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	// Second start should fail
	if err := rm.Start(); err == nil {
		t.Error("Second Start should fail but succeeded")
	}

	rm.Stop()
}

// TestReplicationManagerDoubleStop tests stopping twice
func TestReplicationManagerDoubleStop(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     100,
	}

	rm := NewReplicationManager(config, store)

	rm.Start()

	// First stop should succeed
	if err := rm.Stop(); err != nil {
		t.Fatalf("First Stop failed: %v", err)
	}

	// Second stop should also succeed (idempotent)
	if err := rm.Stop(); err != nil {
		t.Error("Second Stop should be idempotent")
	}
}

// TestReplicationManagerRunningFlagRace tests the running flag race condition fix
// This validates that concurrent Start/Stop/StreamWALEntry calls don't race
func TestReplicationManagerRunningFlagRace(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 100 * time.Millisecond,
		WALBufferSize:     100,
	}

	for iteration := 0; iteration < 50; iteration++ {
		rm := NewReplicationManager(config, store)

		// Start replication
		rm.Start()

		// Concurrently stream WAL entries while starting/stopping
		var wg sync.WaitGroup
		numStreamers := 10

		for i := 0; i < numStreamers; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					entry := &wal.Entry{
						LSN:       uint64(id*20 + j),
						OpType:    wal.OpCreateNode,
						Timestamp: time.Now().UnixNano(),
					}
					rm.StreamWALEntry(entry)
					time.Sleep(1 * time.Millisecond)
				}
			}(i)
		}

		// Stop concurrently with streaming
		time.Sleep(50 * time.Millisecond)
		rm.Stop()

		wg.Wait()
		// If we reach here without panic or race, the fix works
	}
}

// TestReplicationManagerStreamAfterStop tests streaming after stop
func TestReplicationManagerStreamAfterStop(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     100,
	}

	rm := NewReplicationManager(config, store)
	rm.Start()

	// Stream an entry while running
	entry1 := &wal.Entry{
		LSN:       1,
		OpType:    wal.OpCreateNode,
		Timestamp: time.Now().UnixNano(),
	}
	rm.StreamWALEntry(entry1)

	// Stop
	rm.Stop()

	// Try to stream after stop - should not panic
	entry2 := &wal.Entry{
		LSN:       2,
		OpType:    wal.OpCreateNode,
		Timestamp: time.Now().UnixNano(),
	}
	rm.StreamWALEntry(entry2)
}

// TestReplicationManagerConcurrentStartStop tests concurrent start/stop calls
func TestReplicationManagerConcurrentStartStop(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     100,
	}

	rm := NewReplicationManager(config, store)

	var wg sync.WaitGroup

	// Concurrent starts
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rm.Start()
		}()
	}

	// Concurrent stops
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			rm.Stop()
		}()
	}

	wg.Wait()
}

// TestReplicationManagerGetState tests getting replication state
func TestReplicationManagerGetState(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     100,
	}

	rm := NewReplicationManager(config, store)
	rm.Start()
	defer rm.Stop()

	state := rm.GetReplicationState()

	if !state.IsPrimary {
		t.Error("Expected IsPrimary to be true")
	}

	if state.PrimaryID == "" {
		t.Error("Expected non-empty PrimaryID")
	}

	if state.ReplicaCount != 0 {
		t.Errorf("Expected 0 replicas, got %d", state.ReplicaCount)
	}
}

// TestReplicationManagerConcurrentGetState tests concurrent state reads
func TestReplicationManagerConcurrentGetState(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     100,
	}

	rm := NewReplicationManager(config, store)
	rm.Start()
	defer rm.Stop()

	var wg sync.WaitGroup
	numReaders := 20

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				state := rm.GetReplicationState()
				if !state.IsPrimary {
					t.Error("Expected IsPrimary to be true")
				}
			}
		}()
	}

	wg.Wait()
}

// TestReplicationManagerStreamBufferFull tests WAL stream buffer full scenario
func TestReplicationManagerStreamBufferFull(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     10, // Small buffer
	}

	rm := NewReplicationManager(config, store)
	rm.Start()
	defer rm.Stop()

	// Try to overflow the buffer
	for i := 0; i < 100; i++ {
		entry := &wal.Entry{
			LSN:       uint64(i),
			OpType:    wal.OpCreateNode,
			Timestamp: time.Now().UnixNano(),
		}
		rm.StreamWALEntry(entry)
	}

	// Should not panic even if buffer is full
}

// TestReplicationManagerStopBeforeStart tests stopping before starting
func TestReplicationManagerStopBeforeStart(t *testing.T) {
	store := newTestStorage(t)
	defer store.Close()

	config := ReplicationConfig{
		ListenAddr:        "127.0.0.1:0",
		MaxReplicas:       3,
		HeartbeatInterval: 1 * time.Second,
		WALBufferSize:     100,
	}

	rm := NewReplicationManager(config, store)

	// Stop before start - should not panic
	if err := rm.Stop(); err != nil {
		t.Errorf("Stop before start failed: %v", err)
	}
}
