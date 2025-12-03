package replication

import (
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// TestNewMessage tests message creation
func TestNewMessage(t *testing.T) {
	tests := []struct {
		name    string
		msgType MessageType
		data    any
		wantErr bool
	}{
		{
			name:    "heartbeat message",
			msgType: MsgHeartbeat,
			data: HeartbeatMessage{
				From:       "primary-1",
				Sequence:   42,
				CurrentLSN: 1000,
				NodeCount:  100,
				EdgeCount:  200,
			},
			wantErr: false,
		},
		{
			name:    "WAL entry message",
			msgType: MsgWALEntry,
			data: WALEntryMessage{
				Entry: &wal.Entry{
					LSN:    100,
					OpType: wal.OpCreateNode,
					Data:   []byte("test"),
				},
			},
			wantErr: false,
		},
		{
			name:    "error message",
			msgType: MsgError,
			data: ErrorMessage{
				Code:    "ERR_TEST",
				Message: "Test error",
				Fatal:   false,
			},
			wantErr: false,
		},
		{
			name:    "ack message",
			msgType: MsgAck,
			data: AckMessage{
				LastAppliedLSN:    1000,
				ReplicaID:         "replica-1",
				HeartbeatSequence: 42,
			},
			wantErr: false,
		},
		{
			name:    "handshake request",
			msgType: MsgHandshake,
			data: HandshakeRequest{
				ReplicaID:    "replica-1",
				LastLSN:      100,
				Version:      "1.0",
				Capabilities: []string{"wal-streaming"},
			},
			wantErr: false,
		},
		{
			name:    "snapshot message",
			msgType: MsgSnapshot,
			data: SnapshotMessage{
				SnapshotID: "snapshot-123",
				Size:       1024,
				Compressed: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := NewMessage(tt.msgType, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				if msg.Type != tt.msgType {
					t.Errorf("Expected message type %v, got %v", tt.msgType, msg.Type)
				}

				if msg.Timestamp == 0 {
					t.Error("Expected non-zero timestamp")
				}

				if len(msg.Data) == 0 {
					t.Error("Expected non-empty data")
				}
			}
		})
	}
}

// TestMessage_Decode tests message decoding
func TestMessage_Decode(t *testing.T) {
	// Create a heartbeat message
	hb := HeartbeatMessage{
		From:       "primary-1",
		Sequence:   42,
		CurrentLSN: 1000,
		NodeCount:  100,
		EdgeCount:  200,
		Lag:        10,
	}

	msg, err := NewMessage(MsgHeartbeat, hb)
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	// Decode it back
	var decoded HeartbeatMessage
	err = msg.Decode(&decoded)
	if err != nil {
		t.Fatalf("Failed to decode message: %v", err)
	}

	// Verify decoded values
	if decoded.From != hb.From {
		t.Errorf("Expected From %s, got %s", hb.From, decoded.From)
	}

	if decoded.Sequence != hb.Sequence {
		t.Errorf("Expected Sequence %d, got %d", hb.Sequence, decoded.Sequence)
	}

	if decoded.CurrentLSN != hb.CurrentLSN {
		t.Errorf("Expected CurrentLSN %d, got %d", hb.CurrentLSN, decoded.CurrentLSN)
	}

	if decoded.NodeCount != hb.NodeCount {
		t.Errorf("Expected NodeCount %d, got %d", hb.NodeCount, decoded.NodeCount)
	}

	if decoded.EdgeCount != hb.EdgeCount {
		t.Errorf("Expected EdgeCount %d, got %d", hb.EdgeCount, decoded.EdgeCount)
	}

	if decoded.Lag != hb.Lag {
		t.Errorf("Expected Lag %d, got %d", hb.Lag, decoded.Lag)
	}
}

// TestMessage_DecodeError tests decoding with wrong type
func TestMessage_DecodeError(t *testing.T) {
	// Create a heartbeat message
	hb := HeartbeatMessage{
		From:       "primary-1",
		Sequence:   42,
		CurrentLSN: 1000,
	}

	msg, err := NewMessage(MsgHeartbeat, hb)
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	// Try to decode as wrong type
	var errMsg ErrorMessage
	err = msg.Decode(&errMsg)
	// Should succeed but fields won't match (JSON is flexible)
	if err != nil {
		t.Logf("Decode with wrong type returned error (expected): %v", err)
	}
}

// TestDefaultReplicationConfig tests default configuration
func TestDefaultReplicationConfig(t *testing.T) {
	config := DefaultReplicationConfig()

	if config.IsPrimary {
		t.Error("Expected IsPrimary to be false by default")
	}

	if config.ListenAddr != ":9090" {
		t.Errorf("Expected ListenAddr ':9090', got '%s'", config.ListenAddr)
	}

	if config.MaxReplicas != 10 {
		t.Errorf("Expected MaxReplicas 10, got %d", config.MaxReplicas)
	}

	if config.HeartbeatInterval != 1*time.Second {
		t.Errorf("Expected HeartbeatInterval 1s, got %v", config.HeartbeatInterval)
	}

	if config.ReconnectDelay != 5*time.Second {
		t.Errorf("Expected ReconnectDelay 5s, got %v", config.ReconnectDelay)
	}

	if config.SyncMode != SyncModeAsync {
		t.Errorf("Expected SyncMode Async, got %v", config.SyncMode)
	}

	if config.WALBufferSize != 1000 {
		t.Errorf("Expected WALBufferSize 1000, got %d", config.WALBufferSize)
	}
}

// TestHeartbeatTimeout tests heartbeat timeout calculation
func TestHeartbeatTimeout(t *testing.T) {
	tests := []struct {
		name             string
		heartbeatInt     time.Duration
		expectedTimeout  time.Duration
	}{
		{
			name:            "1 second interval",
			heartbeatInt:    1 * time.Second,
			expectedTimeout: 3 * time.Second,
		},
		{
			name:            "500ms interval",
			heartbeatInt:    500 * time.Millisecond,
			expectedTimeout: 1500 * time.Millisecond,
		},
		{
			name:            "5 second interval",
			heartbeatInt:    5 * time.Second,
			expectedTimeout: 15 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ReplicationConfig{
				HeartbeatInterval: tt.heartbeatInt,
			}

			timeout := config.HeartbeatTimeout()

			if timeout != tt.expectedTimeout {
				t.Errorf("Expected timeout %v, got %v", tt.expectedTimeout, timeout)
			}
		})
	}
}

// TestReplicaDeadThreshold tests replica dead threshold calculation
func TestReplicaDeadThreshold(t *testing.T) {
	config := ReplicationConfig{
		HeartbeatInterval: 1 * time.Second,
	}

	threshold := config.ReplicaDeadThreshold()

	// ReplicaDeadThreshold returns a multiplier (int), not a duration
	if threshold != 5 {
		t.Errorf("Expected threshold multiplier 5, got %d", threshold)
	}

	// Verify it's always 5 regardless of heartbeat interval
	config.HeartbeatInterval = 500 * time.Millisecond
	threshold = config.ReplicaDeadThreshold()
	if threshold != 5 {
		t.Errorf("Expected threshold multiplier 5, got %d", threshold)
	}
}

// TestMessageTypes tests message type constants
func TestMessageTypes(t *testing.T) {
	// Ensure message types are distinct
	types := []MessageType{
		MsgHandshake,
		MsgHeartbeat,
		MsgAck,
		MsgSync,
		MsgWALEntry,
		MsgSnapshot,
		MsgError,
	}

	seen := make(map[MessageType]bool)
	for _, mt := range types {
		if seen[mt] {
			t.Errorf("Duplicate message type: %v", mt)
		}
		seen[mt] = true
	}
}

// TestSyncModes tests sync mode constants
func TestSyncModes(t *testing.T) {
	modes := []SyncMode{
		SyncModeAsync,
		SyncModeSync,
		SyncModeQuorum,
	}

	seen := make(map[SyncMode]bool)
	for _, mode := range modes {
		if seen[mode] {
			t.Errorf("Duplicate sync mode: %v", mode)
		}
		seen[mode] = true
	}

	// Verify they have expected values
	if SyncModeAsync != 0 {
		t.Error("SyncModeAsync should be 0 (iota starts at 0)")
	}
	if SyncModeSync != 1 {
		t.Error("SyncModeSync should be 1")
	}
	if SyncModeQuorum != 2 {
		t.Error("SyncModeQuorum should be 2")
	}
}

// TestHandshakeResponse tests handshake response structure
func TestHandshakeResponse(t *testing.T) {
	resp := HandshakeResponse{
		PrimaryID:    "primary-123",
		CurrentLSN:   1000,
		Version:      "1.0",
		Accepted:     true,
		ErrorMessage: "",
	}

	msg, err := NewMessage(MsgHandshake, resp)
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	var decoded HandshakeResponse
	err = msg.Decode(&decoded)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if decoded.PrimaryID != resp.PrimaryID {
		t.Errorf("Expected PrimaryID %s, got %s", resp.PrimaryID, decoded.PrimaryID)
	}

	if decoded.Accepted != resp.Accepted {
		t.Errorf("Expected Accepted %v, got %v", resp.Accepted, decoded.Accepted)
	}
}

// TestHandshakeRequest tests handshake request structure
func TestHandshakeRequest(t *testing.T) {
	req := HandshakeRequest{
		ReplicaID:    "replica-456",
		LastLSN:      500,
		Version:      "1.0",
		Capabilities: []string{"wal-streaming", "snapshots"},
	}

	msg, err := NewMessage(MsgHandshake, req)
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	var decoded HandshakeRequest
	err = msg.Decode(&decoded)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if decoded.ReplicaID != req.ReplicaID {
		t.Errorf("Expected ReplicaID %s, got %s", req.ReplicaID, decoded.ReplicaID)
	}

	if decoded.LastLSN != req.LastLSN {
		t.Errorf("Expected LastLSN %d, got %d", req.LastLSN, decoded.LastLSN)
	}

	if len(decoded.Capabilities) != len(req.Capabilities) {
		t.Errorf("Expected %d capabilities, got %d", len(req.Capabilities), len(decoded.Capabilities))
	}
}
