package admin

import (
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
)

// ReplicaInterface defines the interface for replica operations
type ReplicaInterface interface {
	GetReplicaStatus() replication.ReplicaStatusInfo
	CalculateLagLSN(primaryCurrentLSN uint64) uint64
	Stop() error
	Start() error
	GetReplicationState() replication.ReplicationState
}

// UpgradeConfig contains configuration for the upgrade manager
type UpgradeConfig struct {
	ReplicationPort int `json:"replication_port"`
}

// UpgradeStatus represents the current upgrade state
type UpgradeStatus struct {
	Phase             string    `json:"phase"`
	Ready             bool      `json:"ready"`
	ReplicationLag    int64     `json:"replication_lag_ms"`
	HeartbeatLag      uint64    `json:"heartbeat_lag"`
	Message           string    `json:"message"`
	Timestamp         time.Time `json:"timestamp"`
	CanPromote        bool      `json:"can_promote"`
	CurrentRole       string    `json:"current_role"`
	ConnectedReplicas int       `json:"connected_replicas,omitempty"`
}

// PromoteRequest represents a promotion request
type PromoteRequest struct {
	WaitForSync bool          `json:"wait_for_sync"`
	Timeout     time.Duration `json:"timeout"`
}

// PromoteResponse represents the promotion result
type PromoteResponse struct {
	Success       bool      `json:"success"`
	NewRole       string    `json:"new_role"`
	PreviousRole  string    `json:"previous_role"`
	Message       string    `json:"message"`
	PromotedAt    time.Time `json:"promoted_at"`
	WaitedSeconds float64   `json:"waited_seconds"`
}

// StepDownRequest represents a step-down request for primary
type StepDownRequest struct {
	NewPrimaryID string        `json:"new_primary_id"`
	Timeout      time.Duration `json:"timeout"`
}

// StepDownResponse represents the step-down result
type StepDownResponse struct {
	Success       bool      `json:"success"`
	NewRole       string    `json:"new_role"`
	PreviousRole  string    `json:"previous_role"`
	Message       string    `json:"message"`
	SteppedDownAt time.Time `json:"stepped_down_at"`
}
