package replication

import "time"

// ReplicaStatus represents the status of a replica
type ReplicaStatus struct {
	ReplicaID      string        `json:"replica_id"`
	Connected      bool          `json:"connected"`
	LastSeen       time.Time     `json:"last_seen"` // Primary's local time of last response
	LastAppliedLSN uint64        `json:"last_applied_lsn"`
	CurrentLSN     uint64        `json:"current_lsn"`
	LagLSN         uint64        `json:"lag_lsn"`
	LagMs          int64         `json:"lag_ms"`        // Milliseconds since last response (monotonic)
	HeartbeatLag   uint64        `json:"heartbeat_lag"` // Logical heartbeat lag (sequence numbers)
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
