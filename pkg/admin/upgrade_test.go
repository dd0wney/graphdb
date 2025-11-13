package admin

import (
	"context"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// mockReplicaNode is a mock implementation for testing
type mockReplicaNode struct {
	replicaStatus  replication.ReplicaStatusInfo
	primaryLSN     uint64
	lagLSN         uint64
	stopCalled     bool
}

func (m *mockReplicaNode) GetReplicaStatus() replication.ReplicaStatusInfo {
	return m.replicaStatus
}

func (m *mockReplicaNode) CalculateLagLSN(primaryCurrentLSN uint64) uint64 {
	return m.lagLSN
}

func (m *mockReplicaNode) Stop() error {
	m.stopCalled = true
	return nil
}

func (m *mockReplicaNode) Start() error {
	return nil
}

func (m *mockReplicaNode) GetReplicationState() replication.ReplicationState {
	return replication.ReplicationState{
		IsPrimary:  false,
		PrimaryID:  m.replicaStatus.PrimaryID,
		CurrentLSN: m.replicaStatus.LastAppliedLSN,
	}
}

// TestUpgradeManager_GetUpgradeStatus_Replica tests replica status tracking
func TestUpgradeManager_GetUpgradeStatus_Replica(t *testing.T) {
	tests := []struct {
		name              string
		replicaStatus     replication.ReplicaStatusInfo
		expectedPhase     string
		expectedReady     bool
		expectedCanPromote bool
		expectedMessage   string
	}{
		{
			name: "connected replica ready for promotion",
			replicaStatus: replication.ReplicaStatusInfo{
				ReplicaID:        "replica-1",
				PrimaryID:        "primary-123",
				Connected:        true,
				LastAppliedLSN:   1000,
				LastHeartbeatSeq: 10,
				Timestamp:        time.Now(),
			},
			expectedPhase:      "replica_running",
			expectedReady:      true,
			expectedCanPromote: true,
			expectedMessage:    "Connected to primary primary-123 (LSN: 1000)",
		},
		{
			name: "disconnected replica not ready",
			replicaStatus: replication.ReplicaStatusInfo{
				ReplicaID:        "replica-1",
				PrimaryID:        "",
				Connected:        false,
				LastAppliedLSN:   500,
				LastHeartbeatSeq: 5,
				Timestamp:        time.Now(),
			},
			expectedPhase:      "replica_running",
			expectedReady:      false,
			expectedCanPromote: false,
			expectedMessage:    "Disconnected from primary",
		},
		{
			name: "connected but no heartbeat received",
			replicaStatus: replication.ReplicaStatusInfo{
				ReplicaID:        "replica-1",
				PrimaryID:        "primary-123",
				Connected:        true,
				LastAppliedLSN:   1000,
				LastHeartbeatSeq: 0,
				Timestamp:        time.Now(),
			},
			expectedPhase:      "replica_running",
			expectedReady:      true,
			expectedCanPromote: false,
			expectedMessage:    "Connected to primary primary-123 (LSN: 1000)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReplica := &mockReplicaNode{
				replicaStatus: tt.replicaStatus,
			}

			um := &UpgradeManager{
				storage:   &storage.GraphStorage{},
				replica:   mockReplica,
				isPrimary: false,
				config:    UpgradeConfig{ReplicationPort: 9090},
			}

			status := um.GetUpgradeStatus()

			if status.Phase != tt.expectedPhase {
				t.Errorf("GetUpgradeStatus() phase = %v, want %v", status.Phase, tt.expectedPhase)
			}

			if status.Ready != tt.expectedReady {
				t.Errorf("GetUpgradeStatus() ready = %v, want %v", status.Ready, tt.expectedReady)
			}

			if status.CanPromote != tt.expectedCanPromote {
				t.Errorf("GetUpgradeStatus() canPromote = %v, want %v", status.CanPromote, tt.expectedCanPromote)
			}

			if status.Message != tt.expectedMessage {
				t.Errorf("GetUpgradeStatus() message = %v, want %v", status.Message, tt.expectedMessage)
			}
		})
	}
}

// TestUpgradeManager_WaitForReplicationSync tests replication sync waiting
func TestUpgradeManager_WaitForReplicationSync(t *testing.T) {
	tests := []struct {
		name           string
		primaryLSN     uint64
		replicaLSN     uint64
		lagLSN         uint64
		maxLagMs       int64
		maxHeartbeatLag uint64
		shouldTimeout  bool
		expectError    bool
	}{
		{
			name:            "already synced",
			primaryLSN:      1000,
			replicaLSN:      1000,
			lagLSN:          0,
			maxLagMs:        100,
			maxHeartbeatLag: 2,
			shouldTimeout:   false,
			expectError:     false,
		},
		{
			name:            "small lag within threshold",
			primaryLSN:      1005,
			replicaLSN:      1000,
			lagLSN:          5,
			maxLagMs:        100,
			maxHeartbeatLag: 2,
			shouldTimeout:   false,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReplica := &mockReplicaNode{
				replicaStatus: replication.ReplicaStatusInfo{
					ReplicaID:        "replica-1",
					PrimaryID:        "primary-123",
					Connected:        true,
					LastAppliedLSN:   tt.replicaLSN,
					LastHeartbeatSeq: 10,
					Timestamp:        time.Now(),
				},
				primaryLSN: tt.primaryLSN,
				lagLSN:     tt.lagLSN,
			}

			um := &UpgradeManager{
				storage:   &storage.GraphStorage{},
				replica:   mockReplica,
				isPrimary: false,
				config:    UpgradeConfig{ReplicationPort: 9090},
			}

			ctx := context.Background()
			if tt.shouldTimeout {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			} else {
				ctx = context.Background()
			}

			err := um.WaitForReplicationSync(ctx, tt.maxLagMs, tt.maxHeartbeatLag)

			if tt.expectError && err == nil {
				t.Errorf("WaitForReplicationSync() expected error, got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("WaitForReplicationSync() unexpected error: %v", err)
			}
		})
	}
}
