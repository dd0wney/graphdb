package replication

import (
	"encoding/json"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/wal"
)

// MessageType represents the type of replication message
type MessageType uint8

const (
	// Control messages
	MsgHandshake MessageType = iota
	MsgHeartbeat
	MsgAck
	MsgSync

	// Data messages
	MsgWALEntry
	MsgSnapshot

	// Error messages
	MsgError
)

// Message is the base replication message
type Message struct {
	Type      MessageType `json:"type"`
	Timestamp int64       `json:"timestamp"`
	Data      []byte      `json:"data,omitempty"`
}

// HandshakeRequest is sent by replica to primary during connection
type HandshakeRequest struct {
	ReplicaID    string   `json:"replica_id"`
	LastLSN      uint64   `json:"last_lsn"` // Last LSN received by replica
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

// HandshakeResponse is sent by primary to replica
type HandshakeResponse struct {
	PrimaryID    string `json:"primary_id"`
	CurrentLSN   uint64 `json:"current_lsn"`
	Version      string `json:"version"`
	Accepted     bool   `json:"accepted"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// HeartbeatMessage keeps connection alive and reports status
type HeartbeatMessage struct {
	From       string `json:"from"`
	CurrentLSN uint64 `json:"current_lsn"`
	NodeCount  uint64 `json:"node_count"`
	EdgeCount  uint64 `json:"edge_count"`
	Lag        int64  `json:"lag_ms"` // Replication lag in milliseconds
}

// WALEntryMessage contains a single WAL entry
type WALEntryMessage struct {
	Entry *wal.Entry `json:"entry"`
}

// SnapshotMessage indicates a full snapshot is being sent
type SnapshotMessage struct {
	SnapshotID string `json:"snapshot_id"`
	Size       int64  `json:"size"`
	Compressed bool   `json:"compressed"`
}

// AckMessage acknowledges receipt of messages
type AckMessage struct {
	LastAppliedLSN uint64 `json:"last_applied_lsn"`
	ReplicaID      string `json:"replica_id"`
}

// ErrorMessage reports errors
type ErrorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal"`
}

// NewMessage creates a new message with the given type and data
func NewMessage(msgType MessageType, data interface{}) (*Message, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return &Message{
		Type:      msgType,
		Timestamp: time.Now().Unix(),
		Data:      dataBytes,
	}, nil
}

// Decode decodes message data into the provided interface
func (m *Message) Decode(v interface{}) error {
	return json.Unmarshal(m.Data, v)
}

// ReplicationConfig holds replication configuration
type ReplicationConfig struct {
	// Primary configuration
	IsPrimary         bool
	ListenAddr        string
	MaxReplicas       int
	HeartbeatInterval time.Duration

	// Replica configuration
	PrimaryAddr    string
	ReplicaID      string
	ReconnectDelay time.Duration

	// Common
	SyncMode      SyncMode
	WALBufferSize int
}

// SyncMode defines replication synchronization mode
type SyncMode uint8

const (
	// Async mode - primary doesn't wait for replica acknowledgment
	SyncModeAsync SyncMode = iota

	// Sync mode - primary waits for at least one replica acknowledgment
	SyncModeSync

	// Quorum mode - primary waits for majority of replicas
	SyncModeQuorum
)

// DefaultReplicationConfig returns default configuration
func DefaultReplicationConfig() ReplicationConfig {
	return ReplicationConfig{
		IsPrimary:         false,
		ListenAddr:        ":9090",
		MaxReplicas:       10,
		HeartbeatInterval: 1 * time.Second,
		ReconnectDelay:    5 * time.Second,
		SyncMode:          SyncModeAsync,
		WALBufferSize:     1000,
	}
}

// ReplicaStatus represents the status of a replica
type ReplicaStatus struct {
	ReplicaID      string        `json:"replica_id"`
	Connected      bool          `json:"connected"`
	LastSeen       time.Time     `json:"last_seen"`
	LastAppliedLSN uint64        `json:"last_applied_lsn"`
	CurrentLSN     uint64        `json:"current_lsn"`
	LagLSN         uint64        `json:"lag_lsn"`
	LagDuration    time.Duration `json:"lag_duration"`
}

// ReplicationState represents overall replication state
type ReplicationState struct {
	IsPrimary        bool            `json:"is_primary"`
	PrimaryID        string          `json:"primary_id,omitempty"`
	ReplicaCount     int             `json:"replica_count"`
	Replicas         []ReplicaStatus `json:"replicas"`
	CurrentLSN       uint64          `json:"current_lsn"`
	OldestReplicaLSN uint64          `json:"oldest_replica_lsn"`
}
