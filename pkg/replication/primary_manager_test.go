package replication

import (
	"testing"
	"time"
)

// --- DefaultPrimaryManagerConfig Tests ---

func TestDefaultPrimaryManagerConfig(t *testing.T) {
	cfg := DefaultPrimaryManagerConfig()

	if cfg.WALBufferSize != 1000 {
		t.Errorf("WALBufferSize = %d, want 1000", cfg.WALBufferSize)
	}
	if cfg.SurveyInterval != 5*time.Second {
		t.Errorf("SurveyInterval = %v, want 5s", cfg.SurveyInterval)
	}
	if cfg.SurveyTimeout != 2*time.Second {
		t.Errorf("SurveyTimeout = %v, want 2s", cfg.SurveyTimeout)
	}

	// Check embedded transport config
	if cfg.Transport.WALPublishAddr != "tcp://*:9090" {
		t.Errorf("Transport.WALPublishAddr = %q, want %q", cfg.Transport.WALPublishAddr, "tcp://*:9090")
	}
	if cfg.Transport.HealthSurveyAddr != "tcp://*:9091" {
		t.Errorf("Transport.HealthSurveyAddr = %q, want %q", cfg.Transport.HealthSurveyAddr, "tcp://*:9091")
	}
}

// --- replicaRegistry Tests ---

func TestNewReplicaRegistry(t *testing.T) {
	r := newReplicaRegistry()
	if r == nil {
		t.Fatal("newReplicaRegistry returned nil")
	}
	if r.replicas == nil {
		t.Error("replicas map should be initialized")
	}
	if len(r.replicas) != 0 {
		t.Errorf("replicas map should be empty, got %d", len(r.replicas))
	}
}

func TestReplicaRegistry_UpdateReplica(t *testing.T) {
	r := newReplicaRegistry()

	r.UpdateReplica("replica-1", 100)

	replicas := r.GetReplicas()
	if len(replicas) != 1 {
		t.Fatalf("Expected 1 replica, got %d", len(replicas))
	}

	if replicas[0].ReplicaID != "replica-1" {
		t.Errorf("ReplicaID = %q, want %q", replicas[0].ReplicaID, "replica-1")
	}
	if replicas[0].LastAppliedLSN != 100 {
		t.Errorf("LastAppliedLSN = %d, want 100", replicas[0].LastAppliedLSN)
	}
	if !replicas[0].Healthy {
		t.Error("Replica should be healthy after update")
	}
}

func TestReplicaRegistry_UpdateReplica_Overwrite(t *testing.T) {
	r := newReplicaRegistry()

	r.UpdateReplica("replica-1", 100)
	r.UpdateReplica("replica-1", 200) // overwrite

	replicas := r.GetReplicas()
	if len(replicas) != 1 {
		t.Fatalf("Expected 1 replica, got %d", len(replicas))
	}

	if replicas[0].LastAppliedLSN != 200 {
		t.Errorf("LastAppliedLSN = %d, want 200", replicas[0].LastAppliedLSN)
	}
}

func TestReplicaRegistry_MarkUnhealthy(t *testing.T) {
	r := newReplicaRegistry()

	// Add a replica
	r.UpdateReplica("replica-1", 100)

	// Mark it unhealthy
	r.MarkUnhealthy("replica-1")

	replicas := r.GetReplicas()
	if len(replicas) != 1 {
		t.Fatalf("Expected 1 replica, got %d", len(replicas))
	}

	if replicas[0].Healthy {
		t.Error("Replica should be unhealthy after MarkUnhealthy")
	}
}

func TestReplicaRegistry_MarkUnhealthy_NonExistent(t *testing.T) {
	r := newReplicaRegistry()

	// Should not panic when marking non-existent replica
	r.MarkUnhealthy("non-existent")

	replicas := r.GetReplicas()
	if len(replicas) != 0 {
		t.Errorf("Expected 0 replicas, got %d", len(replicas))
	}
}

func TestReplicaRegistry_GetReplicas_Multiple(t *testing.T) {
	r := newReplicaRegistry()

	r.UpdateReplica("replica-1", 100)
	r.UpdateReplica("replica-2", 200)
	r.UpdateReplica("replica-3", 300)

	replicas := r.GetReplicas()
	if len(replicas) != 3 {
		t.Fatalf("Expected 3 replicas, got %d", len(replicas))
	}

	// Verify all replicas exist (order may vary)
	found := make(map[string]bool)
	for _, rep := range replicas {
		found[rep.ReplicaID] = true
	}

	for _, id := range []string{"replica-1", "replica-2", "replica-3"} {
		if !found[id] {
			t.Errorf("Missing replica %q", id)
		}
	}
}

func TestReplicaRegistry_GetReplicas_ReturnsCopy(t *testing.T) {
	r := newReplicaRegistry()

	r.UpdateReplica("replica-1", 100)

	replicas := r.GetReplicas()
	// Modify the returned slice
	replicas[0].LastAppliedLSN = 999

	// Original should be unchanged
	newReplicas := r.GetReplicas()
	if newReplicas[0].LastAppliedLSN == 999 {
		t.Error("GetReplicas should return a copy")
	}
}

// --- primaryStateProvider Tests ---

// mockStateProvider implements StateProvider for testing
type mockStateProvider struct {
	id         string
	currentLSN uint64
	nodeCount  uint64
	edgeCount  uint64
}

func (m *mockStateProvider) GetID() string         { return m.id }
func (m *mockStateProvider) GetCurrentLSN() uint64 { return m.currentLSN }
func (m *mockStateProvider) GetNodeCount() uint64  { return m.nodeCount }
func (m *mockStateProvider) GetEdgeCount() uint64  { return m.edgeCount }

func TestPrimaryStateProvider(t *testing.T) {
	provider := &mockStateProvider{
		id:         "inner-id", // This won't be used by primaryStateProvider
		currentLSN: 42,
		nodeCount:  100,
		edgeCount:  250,
	}

	psp := &primaryStateProvider{
		id:       "primary-123",
		provider: provider,
	}

	// primaryStateProvider provides its own ID, not the wrapped provider's
	if psp.GetID() != "primary-123" {
		t.Errorf("GetID() = %q, want %q", psp.GetID(), "primary-123")
	}
	// LSN and counts delegate to wrapped provider
	if psp.GetCurrentLSN() != 42 {
		t.Errorf("GetCurrentLSN() = %d, want 42", psp.GetCurrentLSN())
	}
	if psp.GetNodeCount() != 100 {
		t.Errorf("GetNodeCount() = %d, want 100", psp.GetNodeCount())
	}
	if psp.GetEdgeCount() != 250 {
		t.Errorf("GetEdgeCount() = %d, want 250", psp.GetEdgeCount())
	}
}
