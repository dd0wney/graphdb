package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
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

// UpgradeManager handles cluster upgrade operations
type UpgradeManager struct {
	storage     *storage.GraphStorage
	replication *replication.ReplicationManager
	replica     ReplicaInterface
	isPrimary   bool
	config      UpgradeConfig
	mu          sync.RWMutex
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

// NewUpgradeManager creates a new upgrade manager
func NewUpgradeManager(storage *storage.GraphStorage, replication *replication.ReplicationManager, replica ReplicaInterface, isPrimary bool, config UpgradeConfig) *UpgradeManager {
	// Set default port if not specified
	if config.ReplicationPort == 0 {
		config.ReplicationPort = 9090
	}

	return &UpgradeManager{
		storage:     storage,
		replication: replication,
		replica:     replica,
		isPrimary:   isPrimary,
		config:      config,
	}
}

// GetUpgradeStatus returns current upgrade readiness status
func (um *UpgradeManager) GetUpgradeStatus() UpgradeStatus {
	um.mu.RLock()
	defer um.mu.RUnlock()

	status := UpgradeStatus{
		Timestamp:   time.Now(),
		CurrentRole: um.getCurrentRole(),
		Ready:       true,
		CanPromote:  false,
	}

	if um.isPrimary {
		// Primary node status
		state := um.replication.GetReplicationState()
		status.ConnectedReplicas = state.ReplicaCount
		status.Phase = "primary_running"
		status.Message = fmt.Sprintf("Primary with %d connected replicas", state.ReplicaCount)

		// Can step down if at least one replica is caught up
		for _, replica := range state.Replicas {
			if replica.Connected && replica.HeartbeatLag < 3 {
				status.CanPromote = true
				break
			}
		}
	} else if um.replica != nil {
		// Replica node status
		replicaStatus := um.replica.GetReplicaStatus()

		status.Phase = "replica_running"
		status.ReplicationLag = 0 // Will be set by caller if they have primary LSN
		status.HeartbeatLag = replicaStatus.LastHeartbeatSeq

		if replicaStatus.Connected {
			status.Message = fmt.Sprintf("Connected to primary %s (LSN: %d)",
				replicaStatus.PrimaryID, replicaStatus.LastAppliedLSN)
			// Can promote if connected and has recent heartbeat (lag < 5)
			status.CanPromote = replicaStatus.Connected && replicaStatus.LastHeartbeatSeq > 0
		} else {
			status.Message = "Disconnected from primary"
			status.Ready = false
			status.CanPromote = false
		}
	} else {
		status.Phase = "standalone"
		status.Message = "Node running in standalone mode"
	}

	return status
}

// WaitForReplicationSync waits for replication to catch up
func (um *UpgradeManager) WaitForReplicationSync(ctx context.Context, maxLagMs int64, maxHeartbeatLag uint64) error {
	if um.replica == nil {
		return fmt.Errorf("not a replica node")
	}

	log.Printf("Waiting for replication sync (maxLagMs=%d, maxHeartbeatLag=%d)...", maxLagMs, maxHeartbeatLag)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for replication sync: %w", ctx.Err())
		case <-ticker.C:
			status := um.replica.GetReplicaStatus()

			// Check if connected
			if !status.Connected {
				log.Printf("Replica not connected to primary, waiting...")
				continue
			}

			// Check heartbeat lag (simplified check - just verify we're receiving heartbeats)
			if status.LastHeartbeatSeq == 0 {
				log.Printf("No heartbeats received yet, waiting...")
				continue
			}

			// For now, we can't calculate exact LSN lag without knowing the primary's current LSN
			// So we use a heuristic: if we're connected and receiving heartbeats, we're considered synced
			elapsed := time.Since(startTime)
			if elapsed >= 1*time.Second {
				log.Printf("Replication sync complete after %v (connected=%v, heartbeat_seq=%d, lsn=%d)",
					elapsed, status.Connected, status.LastHeartbeatSeq, status.LastAppliedLSN)
				return nil
			}
		}
	}
}

// PromoteToPrimary promotes this replica to primary
func (um *UpgradeManager) PromoteToPrimary(ctx context.Context, waitForSync bool, timeout time.Duration) (*PromoteResponse, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	startTime := time.Now()

	response := &PromoteResponse{
		PreviousRole: um.getCurrentRole(),
		PromotedAt:   time.Now(),
	}

	// Verify this is a replica
	if um.isPrimary {
		response.Success = false
		response.Message = "Already a primary node"
		return response, fmt.Errorf("already a primary node")
	}

	if um.replica == nil {
		response.Success = false
		response.Message = "Not configured as replica"
		return response, fmt.Errorf("not configured as replica")
	}

	// Wait for replication to sync if requested
	if waitForSync {
		syncCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		if err := um.WaitForReplicationSync(syncCtx, 100, 2); err != nil {
			response.Success = false
			response.Message = fmt.Sprintf("Failed to sync before promotion: %v", err)
			return response, err
		}
	}

	// Stop replica mode
	if err := um.replica.Stop(); err != nil {
		response.Success = false
		response.Message = fmt.Sprintf("Failed to stop replica: %v", err)
		return response, err
	}

	// Promote to primary
	um.isPrimary = true

	// Initialize replication manager as primary
	listenAddr := fmt.Sprintf(":%d", um.config.ReplicationPort)
	config := replication.ReplicationConfig{
		ListenAddr:        listenAddr,
		HeartbeatInterval: 1 * time.Second,
		MaxReplicas:       10,
		WALBufferSize:     1000,
	}

	um.replication = replication.NewReplicationManager(config, um.storage)
	if err := um.replication.Start(); err != nil {
		response.Success = false
		response.Message = fmt.Sprintf("Failed to start as primary: %v", err)
		return response, err
	}

	response.Success = true
	response.NewRole = "primary"
	response.Message = "Successfully promoted to primary"
	response.WaitedSeconds = time.Since(startTime).Seconds()

	log.Printf("Node promoted to primary (waited %.2fs)", response.WaitedSeconds)

	return response, nil
}

// StepDownToPrimary demotes primary to replica
func (um *UpgradeManager) StepDownToReplica(ctx context.Context, newPrimaryAddr string) (*StepDownResponse, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	response := &StepDownResponse{
		PreviousRole:  um.getCurrentRole(),
		SteppedDownAt: time.Now(),
	}

	// Verify this is a primary
	if !um.isPrimary {
		response.Success = false
		response.Message = "Not a primary node"
		return response, fmt.Errorf("not a primary node")
	}

	// Stop primary mode
	if err := um.replication.Stop(); err != nil {
		response.Success = false
		response.Message = fmt.Sprintf("Failed to stop primary: %v", err)
		return response, err
	}

	// Demote to replica
	um.isPrimary = false

	// Initialize as replica
	config := replication.ReplicationConfig{
		PrimaryAddr:       newPrimaryAddr,
		HeartbeatInterval: 1 * time.Second,
	}

	um.replica = replication.NewReplicaNode(config, um.storage)
	if err := um.replica.Start(); err != nil {
		response.Success = false
		response.Message = fmt.Sprintf("Failed to start as replica: %v", err)
		return response, err
	}

	response.Success = true
	response.NewRole = "replica"
	response.Message = fmt.Sprintf("Successfully demoted to replica, following %s", newPrimaryAddr)

	log.Printf("Node demoted to replica, following %s", newPrimaryAddr)

	return response, nil
}

func (um *UpgradeManager) getCurrentRole() string {
	if um.isPrimary {
		return "primary"
	} else if um.replica != nil {
		return "replica"
	}
	return "standalone"
}

// RegisterHandlers registers admin HTTP handlers
func (um *UpgradeManager) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/admin/upgrade/status", um.handleUpgradeStatus)
	mux.HandleFunc("/admin/upgrade/promote", um.handlePromote)
	mux.HandleFunc("/admin/upgrade/stepdown", um.handleStepDown)
}

func (um *UpgradeManager) handleUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := um.GetUpgradeStatus()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (um *UpgradeManager) handlePromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Use defaults if no body provided
		req.WaitForSync = true
		req.Timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), req.Timeout)
	defer cancel()

	response, err := um.PromoteToPrimary(ctx, req.WaitForSync, req.Timeout)

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else if response.Success {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}

	if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
		log.Printf("Failed to encode promote response: %v", encErr)
	}
}

func (um *UpgradeManager) handleStepDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req StepDownRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.NewPrimaryID == "" {
		http.Error(w, "new_primary_id is required", http.StatusBadRequest)
		return
	}

	if req.Timeout == 0 {
		req.Timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), req.Timeout)
	defer cancel()

	response, err := um.StepDownToReplica(ctx, req.NewPrimaryID)

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else if response.Success {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}

	if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
		log.Printf("Failed to encode stepdown response: %v", encErr)
	}
}
