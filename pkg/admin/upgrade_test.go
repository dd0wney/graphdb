package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// mockReplicaNode is a mock implementation for testing
type mockReplicaNode struct {
	replicaStatus replication.ReplicaStatusInfo
	primaryLSN    uint64
	lagLSN        uint64
	stopCalled    bool
}

func (m *mockReplicaNode) GetReplicaStatus() replication.ReplicaStatusInfo {
	return m.replicaStatus
}

func (m *mockReplicaNode) CalculateLagLSN(primaryCurrentLSN uint64) uint64 {
	return m.lagLSN
}

func (m *mockReplicaNode) Stop() error {
	if m != nil {
		m.stopCalled = true
	}
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
		name               string
		replicaStatus      replication.ReplicaStatusInfo
		expectedPhase      string
		expectedReady      bool
		expectedCanPromote bool
		expectedMessage    string
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
		name            string
		primaryLSN      uint64
		replicaLSN      uint64
		lagLSN          uint64
		maxLagMs        int64
		maxHeartbeatLag uint64
		shouldTimeout   bool
		expectError     bool
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

// TestUpgradeManager_PromoteToPrimary tests promoting replica to primary
func TestUpgradeManager_PromoteToPrimary(t *testing.T) {
	tests := []struct {
		name          string
		isPrimary     bool
		hasReplica    bool
		waitForSync   bool
		expectSuccess bool
		expectError   bool
		errorContains string
	}{
		{
			name:          "successful promotion without sync",
			isPrimary:     false,
			hasReplica:    true,
			waitForSync:   false,
			expectSuccess: true,
			expectError:   false,
		},
		{
			name:          "successful promotion with sync",
			isPrimary:     false,
			hasReplica:    true,
			waitForSync:   true,
			expectSuccess: true,
			expectError:   false,
		},
		{
			name:          "already primary",
			isPrimary:     true,
			hasReplica:    false,
			waitForSync:   false,
			expectSuccess: false,
			expectError:   true,
			errorContains: "already a primary",
		},
		{
			name:          "not configured as replica",
			isPrimary:     false,
			hasReplica:    false,
			waitForSync:   false,
			expectSuccess: false,
			expectError:   true,
			errorContains: "not configured as replica",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			um := &UpgradeManager{
				storage:   &storage.GraphStorage{},
				replica:   nil,
				isPrimary: tt.isPrimary,
				config:    UpgradeConfig{ReplicationPort: 0}, // Use port 0 for testing
			}

			if tt.hasReplica {
				um.replica = &mockReplicaNode{
					replicaStatus: replication.ReplicaStatusInfo{
						ReplicaID:        "replica-1",
						PrimaryID:        "primary-123",
						Connected:        true,
						LastAppliedLSN:   1000,
						LastHeartbeatSeq: 10,
						Timestamp:        time.Now(),
					},
					lagLSN: 0,
				}
			}

			ctx := context.Background()
			timeout := 5 * time.Second

			response, err := um.PromoteToPrimary(ctx, tt.waitForSync, timeout)

			if tt.expectError {
				if err == nil {
					t.Errorf("PromoteToPrimary() expected error, got nil")
				}
				if tt.errorContains != "" && err != nil {
					if !contains(err.Error(), tt.errorContains) {
						t.Errorf("PromoteToPrimary() error = %v, want error containing %v", err, tt.errorContains)
					}
				}
			} else {
				if err != nil {
					t.Errorf("PromoteToPrimary() unexpected error: %v", err)
				}
			}

			if response != nil {
				if response.Success != tt.expectSuccess {
					t.Errorf("PromoteToPrimary() success = %v, want %v", response.Success, tt.expectSuccess)
				}

				if tt.expectSuccess {
					if response.NewRole != "primary" {
						t.Errorf("PromoteToPrimary() newRole = %v, want primary", response.NewRole)
					}
					if !um.isPrimary {
						t.Errorf("PromoteToPrimary() manager still not marked as primary")
					}
					if mockRep, ok := um.replica.(*mockReplicaNode); ok && mockRep != nil {
						if !mockRep.stopCalled {
							t.Errorf("PromoteToPrimary() replica.Stop() not called")
						}
					}
					if um.replication != nil {
						um.replication.Stop()
					}
				}
			}
		})
	}
}

// TestUpgradeManager_StepDownToReplica tests demoting primary to replica
func TestUpgradeManager_StepDownToReplica(t *testing.T) {
	tests := []struct {
		name           string
		isPrimary      bool
		hasReplication bool
		expectSuccess  bool
		expectError    bool
		errorContains  string
	}{
		{
			name:           "already replica",
			isPrimary:      false,
			hasReplication: false,
			expectSuccess:  false,
			expectError:    true,
			errorContains:  "not a primary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			um := &UpgradeManager{
				storage:     &storage.GraphStorage{},
				isPrimary:   tt.isPrimary,
				replication: nil,
				config:      UpgradeConfig{ReplicationPort: 0},
			}

			ctx := context.Background()
			newPrimaryAddr := "localhost:9999"

			response, err := um.StepDownToReplica(ctx, newPrimaryAddr)

			if tt.expectError {
				if err == nil {
					t.Errorf("StepDownToReplica() expected error, got nil")
				}
				if tt.errorContains != "" && err != nil {
					if !contains(err.Error(), tt.errorContains) {
						t.Errorf("StepDownToReplica() error = %v, want error containing %v", err, tt.errorContains)
					}
				}
			} else {
				if err != nil {
					t.Errorf("StepDownToReplica() unexpected error: %v", err)
				}
			}

			if response != nil {
				if response.Success != tt.expectSuccess {
					t.Errorf("StepDownToReplica() success = %v, want %v", response.Success, tt.expectSuccess)
				}
			}
		})
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && hasSubstring(s, substr)))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- NewUpgradeManager Tests ---

func TestNewUpgradeManager(t *testing.T) {
	storage := &storage.GraphStorage{}
	config := UpgradeConfig{ReplicationPort: 9090}

	um := NewUpgradeManager(storage, nil, nil, true, config)

	if um == nil {
		t.Fatal("NewUpgradeManager returned nil")
	}
	if um.storage != storage {
		t.Error("storage not set correctly")
	}
	if !um.isPrimary {
		t.Error("isPrimary should be true")
	}
	if um.config.ReplicationPort != 9090 {
		t.Errorf("ReplicationPort = %d, want 9090", um.config.ReplicationPort)
	}
}

func TestNewUpgradeManager_DefaultPort(t *testing.T) {
	// When port is 0, should default to 9090
	config := UpgradeConfig{ReplicationPort: 0}

	um := NewUpgradeManager(&storage.GraphStorage{}, nil, nil, false, config)

	if um.config.ReplicationPort != 9090 {
		t.Errorf("ReplicationPort = %d, want default 9090", um.config.ReplicationPort)
	}
}

func TestNewUpgradeManager_WithReplica(t *testing.T) {
	mockReplica := &mockReplicaNode{
		replicaStatus: replication.ReplicaStatusInfo{
			ReplicaID: "replica-1",
		},
	}
	config := UpgradeConfig{ReplicationPort: 9091}

	um := NewUpgradeManager(&storage.GraphStorage{}, nil, mockReplica, false, config)

	if um.replica == nil {
		t.Error("replica should be set")
	}
	if um.isPrimary {
		t.Error("isPrimary should be false")
	}
}

// --- SetElectionManager Tests ---

func TestSetElectionManager_Enable(t *testing.T) {
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: true,
	}

	// Since ElectionManager requires actual cluster setup, test with nil
	// to verify the disable path
	um.SetElectionManager(nil)

	if um.clusterEnabled {
		t.Error("clusterEnabled should be false when electionMgr is nil")
	}
	if um.electionMgr != nil {
		t.Error("electionMgr should be nil")
	}
}

// --- HTTP Handler Tests ---

func TestRegisterHandlers(t *testing.T) {
	// Use standalone mode (no replica, not primary) to avoid nil pointer
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: false,
		replica:   nil, // standalone mode
		config:    UpgradeConfig{ReplicationPort: 9090},
	}

	mux := http.NewServeMux()
	um.RegisterHandlers(mux)

	// Test status endpoint (should work in standalone mode)
	req := httptest.NewRequest(http.MethodGet, "/admin/upgrade/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("Handler not registered for /admin/upgrade/status")
	}

	// Test promote with wrong method (verifies registration)
	req = httptest.NewRequest(http.MethodGet, "/admin/upgrade/promote", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("Handler not registered for /admin/upgrade/promote")
	}

	// Test stepdown with wrong method (verifies registration)
	req = httptest.NewRequest(http.MethodGet, "/admin/upgrade/stepdown", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("Handler not registered for /admin/upgrade/stepdown")
	}
}

func TestHandleUpgradeStatus_Success(t *testing.T) {
	// Test in standalone mode to avoid nil replication pointer
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: false,
		replica:   nil, // standalone mode
		config:    UpgradeConfig{ReplicationPort: 9090},
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/upgrade/status", nil)
	w := httptest.NewRecorder()

	um.handleUpgradeStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handleUpgradeStatus() status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}

	var status UpgradeStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if status.CurrentRole != "standalone" {
		t.Errorf("CurrentRole = %q, want standalone", status.CurrentRole)
	}
	if status.Phase != "standalone" {
		t.Errorf("Phase = %q, want standalone", status.Phase)
	}
}

func TestHandleUpgradeStatus_ReplicaMode(t *testing.T) {
	mockReplica := &mockReplicaNode{
		replicaStatus: replication.ReplicaStatusInfo{
			ReplicaID:        "replica-1",
			PrimaryID:        "primary-123",
			Connected:        true,
			LastAppliedLSN:   1000,
			LastHeartbeatSeq: 10,
			Timestamp:        time.Now(),
		},
	}

	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: false,
		replica:   mockReplica,
		config:    UpgradeConfig{ReplicationPort: 9090},
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/upgrade/status", nil)
	w := httptest.NewRecorder()

	um.handleUpgradeStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handleUpgradeStatus() status = %d, want %d", w.Code, http.StatusOK)
	}

	var status UpgradeStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if status.CurrentRole != "replica" {
		t.Errorf("CurrentRole = %q, want replica", status.CurrentRole)
	}
	if status.Phase != "replica_running" {
		t.Errorf("Phase = %q, want replica_running", status.Phase)
	}
}

func TestHandleUpgradeStatus_MethodNotAllowed(t *testing.T) {
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: false,
		replica:   nil, // standalone mode
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/upgrade/status", nil)
	w := httptest.NewRecorder()

	um.handleUpgradeStatus(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleUpgradeStatus() status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandlePromote_MethodNotAllowed(t *testing.T) {
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: false,
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/upgrade/promote", nil)
	w := httptest.NewRecorder()

	um.handlePromote(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("handlePromote() status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandlePromote_Success(t *testing.T) {
	mockReplica := &mockReplicaNode{
		replicaStatus: replication.ReplicaStatusInfo{
			ReplicaID:        "replica-1",
			PrimaryID:        "primary-123",
			Connected:        true,
			LastAppliedLSN:   1000,
			LastHeartbeatSeq: 10,
			Timestamp:        time.Now(),
		},
		lagLSN: 0,
	}

	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: false,
		replica:   mockReplica,
		config:    UpgradeConfig{ReplicationPort: 0},
	}

	body := bytes.NewBufferString(`{"wait_for_sync": false, "timeout": 5000000000}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/upgrade/promote", body)
	w := httptest.NewRecorder()

	um.handlePromote(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handlePromote() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp PromoteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("Response success = %v, want true", resp.Success)
	}
}

func TestHandlePromote_AlreadyPrimary(t *testing.T) {
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: true,
		config:    UpgradeConfig{ReplicationPort: 9090},
	}

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/upgrade/promote", body)
	w := httptest.NewRecorder()

	um.handlePromote(w, req)

	// Should return error status since already primary
	if w.Code == http.StatusOK {
		t.Error("handlePromote() should fail for primary node")
	}
}

func TestHandleStepDown_MethodNotAllowed(t *testing.T) {
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: true,
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/upgrade/stepdown", nil)
	w := httptest.NewRecorder()

	um.handleStepDown(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("handleStepDown() status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleStepDown_InvalidBody(t *testing.T) {
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: true,
	}

	body := bytes.NewBufferString(`invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/admin/upgrade/stepdown", body)
	w := httptest.NewRecorder()

	um.handleStepDown(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleStepDown() status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleStepDown_MissingNewPrimaryID(t *testing.T) {
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: true,
	}

	body := bytes.NewBufferString(`{"timeout": 5000000000}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/upgrade/stepdown", body)
	w := httptest.NewRecorder()

	um.handleStepDown(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("handleStepDown() status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleStepDown_NotPrimary(t *testing.T) {
	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: false,
		config:    UpgradeConfig{ReplicationPort: 9090},
	}

	body := bytes.NewBufferString(`{"new_primary_id": "new-primary-addr:9090"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/upgrade/stepdown", body)
	w := httptest.NewRecorder()

	um.handleStepDown(w, req)

	// Should return error status since not primary
	if w.Code == http.StatusOK {
		t.Error("handleStepDown() should fail for non-primary node")
	}
}

func TestHandlePromote_NoBody(t *testing.T) {
	mockReplica := &mockReplicaNode{
		replicaStatus: replication.ReplicaStatusInfo{
			ReplicaID:        "replica-1",
			PrimaryID:        "primary-123",
			Connected:        true,
			LastAppliedLSN:   1000,
			LastHeartbeatSeq: 10,
			Timestamp:        time.Now(),
		},
		lagLSN: 0,
	}

	um := &UpgradeManager{
		storage:   &storage.GraphStorage{},
		isPrimary: false,
		replica:   mockReplica,
		config:    UpgradeConfig{ReplicationPort: 0},
	}

	// Empty body - should use defaults
	req := httptest.NewRequest(http.MethodPost, "/admin/upgrade/promote", nil)
	w := httptest.NewRecorder()

	um.handlePromote(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("handlePromote() status = %d, want %d", w.Code, http.StatusOK)
	}
}
