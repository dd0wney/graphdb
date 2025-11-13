package replication

import (
	"testing"
	"time"
)

// TestReplicaNode_GetReplicaStatus tests the GetReplicaStatus method
func TestReplicaNode_GetReplicaStatus(t *testing.T) {
	tests := []struct {
		name              string
		setupReplica      func(*ReplicaNode)
		expectedConnected bool
		expectedPrimaryID string
		checkLSN          bool
		expectedLSN       uint64
		checkHeartbeat    bool
		expectedHeartbeat uint64
	}{
		{
			name: "disconnected replica",
			setupReplica: func(rn *ReplicaNode) {
				rn.setConnected(false)
				rn.primaryID = ""
			},
			expectedConnected: false,
			expectedPrimaryID: "",
			checkLSN:          true,
			expectedLSN:       0,
		},
		{
			name: "connected replica with LSN",
			setupReplica: func(rn *ReplicaNode) {
				rn.setConnected(true)
				rn.primaryID = "primary-123"
				rn.lastAppliedLSN = 1000
			},
			expectedConnected: true,
			expectedPrimaryID: "primary-123",
			checkLSN:          true,
			expectedLSN:       1000,
		},
		{
			name: "replica with heartbeat tracking",
			setupReplica: func(rn *ReplicaNode) {
				rn.setConnected(true)
				rn.primaryID = "primary-456"
				rn.lastAppliedLSN = 2500
				rn.heartbeatSeqMu.Lock()
				rn.lastReceivedHeartbeatSeq = 42
				rn.heartbeatSeqMu.Unlock()
			},
			expectedConnected: true,
			expectedPrimaryID: "primary-456",
			checkLSN:          true,
			expectedLSN:       2500,
			checkHeartbeat:    true,
			expectedHeartbeat: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create minimal replica node
			rn := &ReplicaNode{
				replicaID: "test-replica",
				stopCh:    make(chan struct{}),
			}

			// Setup the replica state
			tt.setupReplica(rn)

			// Get the status
			status := rn.GetReplicaStatus()

			// Verify connection status
			if status.Connected != tt.expectedConnected {
				t.Errorf("GetReplicaStatus() connected = %v, want %v", status.Connected, tt.expectedConnected)
			}

			// Verify primary ID
			if status.PrimaryID != tt.expectedPrimaryID {
				t.Errorf("GetReplicaStatus() primaryID = %v, want %v", status.PrimaryID, tt.expectedPrimaryID)
			}

			// Verify LSN if needed
			if tt.checkLSN && status.LastAppliedLSN != tt.expectedLSN {
				t.Errorf("GetReplicaStatus() lastAppliedLSN = %v, want %v", status.LastAppliedLSN, tt.expectedLSN)
			}

			// Verify heartbeat if needed
			if tt.checkHeartbeat && status.LastHeartbeatSeq != tt.expectedHeartbeat {
				t.Errorf("GetReplicaStatus() lastHeartbeatSeq = %v, want %v", status.LastHeartbeatSeq, tt.expectedHeartbeat)
			}

			// Verify replica ID is set
			if status.ReplicaID != rn.replicaID {
				t.Errorf("GetReplicaStatus() replicaID = %v, want %v", status.ReplicaID, rn.replicaID)
			}

			// Verify timestamp is recent
			if time.Since(status.Timestamp) > 1*time.Second {
				t.Errorf("GetReplicaStatus() timestamp is too old: %v", status.Timestamp)
			}
		})
	}
}

// TestReplicaNode_CalculateLag tests lag calculation
func TestReplicaNode_CalculateLag(t *testing.T) {
	tests := []struct {
		name              string
		lastAppliedLSN    uint64
		primaryCurrentLSN uint64
		expectedLagLSN    uint64
	}{
		{
			name:              "no lag - caught up",
			lastAppliedLSN:    1000,
			primaryCurrentLSN: 1000,
			expectedLagLSN:    0,
		},
		{
			name:              "small lag",
			lastAppliedLSN:    995,
			primaryCurrentLSN: 1000,
			expectedLagLSN:    5,
		},
		{
			name:              "large lag",
			lastAppliedLSN:    500,
			primaryCurrentLSN: 1000,
			expectedLagLSN:    500,
		},
		{
			name:              "ahead of primary (edge case)",
			lastAppliedLSN:    1100,
			primaryCurrentLSN: 1000,
			expectedLagLSN:    0, // Should not be negative
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rn := &ReplicaNode{
				lastAppliedLSN: tt.lastAppliedLSN,
			}

			lagLSN := rn.CalculateLagLSN(tt.primaryCurrentLSN)

			if lagLSN != tt.expectedLagLSN {
				t.Errorf("CalculateLagLSN() = %v, want %v", lagLSN, tt.expectedLagLSN)
			}
		})
	}
}
