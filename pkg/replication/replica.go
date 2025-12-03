package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/cluster"
	"github.com/dd0wney/cluso-graphdb/pkg/logging"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ReplicaNode represents a replica instance
type ReplicaNode struct {
	config                   ReplicationConfig
	replicaID                string
	storage                  *storage.GraphStorage
	conn                     net.Conn
	encoder                  *json.Encoder
	decoder                  *json.Decoder
	lastAppliedLSN           uint64
	primaryID                string
	connected                bool
	connectedMu              sync.RWMutex
	stopCh                   chan struct{}
	wg                       sync.WaitGroup // Tracks all goroutines for clean shutdown
	running                  bool
	runningMu                sync.Mutex
	lastReceivedHeartbeatSeq uint64 // Last heartbeat sequence received from primary
	heartbeatSeqMu           sync.Mutex
	logger                   logging.Logger // Structured logger

	// Cluster management (optional - only set if HA enabled)
	clusterEnabled    bool
	membership        *cluster.ClusterMembership
	electionMgr       *cluster.ElectionManager
	discovery         *cluster.NodeDiscovery
	lastHeartbeatTime time.Time // For election timeout detection
	heartbeatTimeMu   sync.Mutex
}

// ReplicaStatusInfo contains detailed status information about this replica
type ReplicaStatusInfo struct {
	ReplicaID        string    `json:"replica_id"`
	PrimaryID        string    `json:"primary_id"`
	Connected        bool      `json:"connected"`
	LastAppliedLSN   uint64    `json:"last_applied_lsn"`
	LastHeartbeatSeq uint64    `json:"last_heartbeat_seq"`
	Timestamp        time.Time `json:"timestamp"`
}

// NewReplicaNode creates a new replica node
func NewReplicaNode(config ReplicationConfig, storage *storage.GraphStorage) *ReplicaNode {
	if config.ReplicaID == "" {
		config.ReplicaID = generateID()
	}

	// Create logger with component context
	logger := logging.DefaultLogger().With(
		logging.Component("replication"),
		logging.String("role", "replica"),
		logging.String("replica_id", config.ReplicaID),
	)

	return &ReplicaNode{
		config:    config,
		replicaID: config.ReplicaID,
		storage:   storage,
		stopCh:    make(chan struct{}),
		logger:    logger,
	}
}

// EnableCluster enables cluster mode with automatic failover
// Must be called before Start()
func (rn *ReplicaNode) EnableCluster(clusterConfig cluster.ClusterConfig) error {
	if rn.running {
		return fmt.Errorf("cannot enable cluster on running replica")
	}

	// Initialize cluster components
	rn.membership = cluster.NewClusterMembership(clusterConfig.NodeID, clusterConfig.NodeAddr)
	rn.membership.SetLocalRole(cluster.RoleReplica)

	rn.electionMgr = cluster.NewElectionManager(clusterConfig, rn.membership)
	rn.discovery = cluster.NewNodeDiscovery(clusterConfig, rn.membership)

	// Set up election callbacks
	rn.electionMgr.SetCallbacks(
		rn.onBecomeLeader,   // When replica wins election
		rn.onBecomeFollower, // When replica loses leadership
		nil,                 // onBecomeCandidate not needed
	)

	rn.clusterEnabled = true
	log.Printf("Cluster mode enabled for replica %s", clusterConfig.NodeID)

	return nil
}

// onBecomeLeader is called when this replica wins an election
func (rn *ReplicaNode) onBecomeLeader() {
	log.Printf("🎯 Replica won election - promoting to primary")
	// TODO: Transition to primary mode (requires ReplicationManager integration)
	// For now, this will be coordinated with the admin API
}

// onBecomeFollower is called when this replica becomes/remains a follower
func (rn *ReplicaNode) onBecomeFollower() {
	log.Printf("📥 Replica is follower - continuing normal replication")
}

// Start starts the replica node
func (rn *ReplicaNode) Start() error {
	rn.runningMu.Lock()
	defer rn.runningMu.Unlock()

	if rn.running {
		return fmt.Errorf("replica already running")
	}

	rn.running = true

	// Start cluster components if enabled
	if rn.clusterEnabled {
		if err := rn.startClusterComponents(); err != nil {
			return err
		}
	}

	// Start connection manager
	rn.wg.Add(1)
	go rn.connectionManager()

	// Start primary health monitor if cluster enabled
	if rn.clusterEnabled {
		rn.wg.Add(1)
		go rn.monitorPrimaryHealth()
	}

	rn.logger.Info("replica node started",
		logging.Operation("start"),
		logging.Bool("cluster_enabled", rn.clusterEnabled))

	return nil
}

// startClusterComponents starts all cluster-related components
func (rn *ReplicaNode) startClusterComponents() error {
	// Start discovery
	if err := rn.discovery.Start(); err != nil {
		log.Printf("Warning: Failed to start discovery: %v", err)
	}

	// Start election manager
	if err := rn.electionMgr.Start(); err != nil {
		log.Printf("Warning: Failed to start election manager: %v", err)
	}

	log.Printf("Cluster components started for replica %s", rn.replicaID)
	return nil
}

// Stop stops the replica node
func (rn *ReplicaNode) Stop() error {
	rn.runningMu.Lock()
	defer rn.runningMu.Unlock()

	if !rn.running {
		return nil
	}

	close(rn.stopCh)
	rn.running = false

	rn.disconnect()

	// Wait for all goroutines to complete
	rn.wg.Wait()

	// Stop cluster components if enabled
	if rn.clusterEnabled {
		rn.stopClusterComponents()
	}

	rn.logger.Info("replica node stopped", logging.Operation("stop"))

	return nil
}

// stopClusterComponents stops all cluster-related components
func (rn *ReplicaNode) stopClusterComponents() {
	if err := rn.electionMgr.Stop(); err != nil {
		rn.logger.Warn("failed to stop election manager", logging.Error(err))
	}
	if err := rn.discovery.Stop(); err != nil {
		rn.logger.Warn("failed to stop discovery", logging.Error(err))
	}
}

// GetReplicationState returns current replication state
func (rn *ReplicaNode) GetReplicationState() ReplicationState {
	rn.connectedMu.RLock()
	defer rn.connectedMu.RUnlock()

	return ReplicationState{
		IsPrimary:  false,
		PrimaryID:  rn.primaryID,
		CurrentLSN: rn.lastAppliedLSN,
	}
}

// GetReplicaStatus returns detailed status information about this replica
func (rn *ReplicaNode) GetReplicaStatus() ReplicaStatusInfo {
	rn.connectedMu.RLock()
	connected := rn.connected
	primaryID := rn.primaryID
	lastAppliedLSN := rn.lastAppliedLSN
	rn.connectedMu.RUnlock()

	rn.heartbeatSeqMu.Lock()
	lastHeartbeatSeq := rn.lastReceivedHeartbeatSeq
	rn.heartbeatSeqMu.Unlock()

	return ReplicaStatusInfo{
		ReplicaID:        rn.replicaID,
		PrimaryID:        primaryID,
		Connected:        connected,
		LastAppliedLSN:   lastAppliedLSN,
		LastHeartbeatSeq: lastHeartbeatSeq,
		Timestamp:        time.Now(),
	}
}

// CalculateLagLSN calculates the LSN lag between this replica and the primary
func (rn *ReplicaNode) CalculateLagLSN(primaryCurrentLSN uint64) uint64 {
	if primaryCurrentLSN <= rn.lastAppliedLSN {
		return 0
	}
	return primaryCurrentLSN - rn.lastAppliedLSN
}
