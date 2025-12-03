package replication

import "github.com/dd0wney/cluso-graphdb/pkg/wal"

// HandshakeRequest is sent by replica to primary during connection
type HandshakeRequest struct {
	ReplicaID    string   `json:"replica_id"`
	LastLSN      uint64   `json:"last_lsn"` // Last LSN received by replica
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	Epoch        uint64   `json:"epoch"` // Cluster generation number for fencing
	Term         uint64   `json:"term"`  // Election term
}

// HandshakeResponse is sent by primary to replica
type HandshakeResponse struct {
	PrimaryID    string `json:"primary_id"`
	CurrentLSN   uint64 `json:"current_lsn"`
	Version      string `json:"version"`
	Accepted     bool   `json:"accepted"`
	ErrorMessage string `json:"error_message,omitempty"`
	Epoch        uint64 `json:"epoch"` // Current cluster epoch
	Term         uint64 `json:"term"`  // Current election term
}

// HeartbeatMessage keeps connection alive and reports status
type HeartbeatMessage struct {
	From       string `json:"from"`
	Sequence   uint64 `json:"sequence"` // Monotonically increasing sequence number
	CurrentLSN uint64 `json:"current_lsn"`
	NodeCount  uint64 `json:"node_count"`
	EdgeCount  uint64 `json:"edge_count"`
	Lag        int64  `json:"lag_ms"` // Replication lag in milliseconds
	Epoch      uint64 `json:"epoch"`  // Current cluster epoch for fencing
	Term       uint64 `json:"term"`   // Current election term
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
	LastAppliedLSN    uint64 `json:"last_applied_lsn"`
	ReplicaID         string `json:"replica_id"`
	HeartbeatSequence uint64 `json:"heartbeat_sequence"` // ACKs this heartbeat sequence
}

// ErrorMessage reports errors
type ErrorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal"`
}
