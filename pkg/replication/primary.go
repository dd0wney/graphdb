package replication

import (
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/dd0wney/cluso-graphdb/pkg/cluster"
	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// ReplicationManager manages replication on the primary node
type ReplicationManager struct {
	config              ReplicationConfig
	primaryID           string
	storage             *storage.GraphStorage
	replicas            map[string]*ReplicaConnection
	replicasMu          sync.RWMutex
	listener            net.Listener
	walStream           chan *wal.Entry
	stopCh              chan struct{}
	wg                  sync.WaitGroup  // Tracks all goroutines for clean shutdown
	running             bool
	runningMu           sync.Mutex
	heartbeatSeqCounter atomic.Uint64 // Monotonically increasing heartbeat sequence (lock-free)
	metricsRegistry     *metrics.Registry

	// Cluster management (optional - only set if HA enabled)
	clusterEnabled bool
	membership     *cluster.ClusterMembership
	electionMgr    *cluster.ElectionManager
	discovery      *cluster.NodeDiscovery
}

// ReplicaConnection represents a connection to a replica
type ReplicaConnection struct {
	replicaID                string
	conn                     net.Conn
	sendCh                   chan *Message
	stopCh                   chan struct{}
	stopOnce                 sync.Once // Ensures stopCh is only closed once
	mu                       sync.RWMutex
	lastResponseTime         time.Time // Primary's local time when last response received (protected by mu)
	lastResponseHeartbeatSeq uint64    // Last heartbeat seq we received ACK for (protected by mu)
	lastAppliedLSN           uint64    // (protected by mu)
}

// NewReplicationManager creates a new replication manager for primary
func NewReplicationManager(config ReplicationConfig, storage *storage.GraphStorage) *ReplicationManager {
	return &ReplicationManager{
		config:          config,
		primaryID:       generateID(),
		storage:         storage,
		replicas:        make(map[string]*ReplicaConnection),
		walStream:       make(chan *wal.Entry, config.WALBufferSize),
		stopCh:          make(chan struct{}),
		clusterEnabled:  false,
		metricsRegistry: metrics.DefaultRegistry(),
	}
}

// Start starts the replication manager
func (rm *ReplicationManager) Start() error {
	rm.runningMu.Lock()
	defer rm.runningMu.Unlock()

	if rm.running {
		return fmt.Errorf("replication manager already running")
	}

	// Listen for replica connections
	listener, err := net.Listen("tcp", rm.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	rm.listener = listener
	rm.running = true

	// Start accepting connections
	rm.wg.Add(1)
	go rm.acceptConnections()

	// Start heartbeat sender
	rm.wg.Add(1)
	go rm.sendHeartbeats()

	// Start WAL broadcaster
	rm.wg.Add(1)
	go rm.broadcastWALEntries()

	// Start cluster components if enabled
	if rm.clusterEnabled {
		rm.startClusterComponents()
	}

	log.Printf("Replication manager started on %s (primary_id=%s)", rm.config.ListenAddr, rm.primaryID)

	return nil
}

// Stop stops the replication manager.
// Returns an error if any resources failed to close (indicates potential resource leak).
func (rm *ReplicationManager) Stop() error {
	rm.runningMu.Lock()
	defer rm.runningMu.Unlock()

	if !rm.running {
		return nil
	}

	close(rm.stopCh)
	rm.running = false

	var closeErrors []error

	// Stop cluster components if enabled
	if rm.clusterEnabled {
		closeErrors = append(closeErrors, rm.stopClusterComponents()...)
	}

	// Close listener to stop accepting new connections
	if rm.listener != nil {
		if err := rm.listener.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("listener close: %w", err))
		}
	}

	// Close all replica connections
	closeErrors = append(closeErrors, rm.closeAllReplicas()...)

	// Wait for all goroutines to complete
	rm.wg.Wait()

	if len(closeErrors) > 0 {
		log.Printf("Replication manager stopped with %d close errors", len(closeErrors))
		return fmt.Errorf("replication shutdown completed with %d errors: %w", len(closeErrors), closeErrors[0])
	}

	log.Printf("Replication manager stopped")
	return nil
}

// closeAllReplicas closes all replica connections
func (rm *ReplicationManager) closeAllReplicas() []error {
	var errors []error
	rm.replicasMu.Lock()
	for id, replica := range rm.replicas {
		replica.stopOnce.Do(func() {
			close(replica.stopCh)
		})
		if err := replica.conn.Close(); err != nil {
			errors = append(errors, fmt.Errorf("replica %s close: %w", id, err))
		}
	}
	rm.replicas = make(map[string]*ReplicaConnection)
	rm.replicasMu.Unlock()
	return errors
}

// ErrWALStreamFull is returned when the WAL stream buffer is full and times out
var ErrWALStreamFull = fmt.Errorf("WAL stream buffer full")

// ErrReplicationStopped is returned when replication manager is not running
var ErrReplicationStopped = fmt.Errorf("replication manager stopped")

// StreamWALEntry sends a WAL entry to all replicas.
// Returns an error if the buffer is full after timeout (prevents silent data loss).
func (rm *ReplicationManager) StreamWALEntry(entry *wal.Entry) error {
	// Check running state under lock to avoid race with Stop()
	rm.runningMu.Lock()
	if !rm.running {
		rm.runningMu.Unlock()
		return ErrReplicationStopped
	}
	rm.runningMu.Unlock()

	// Get timeout from config, default to 5 seconds
	timeout := rm.config.WALStreamTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// Block with timeout - never silently drop WAL entries
	select {
	case rm.walStream <- entry:
		return nil
	case <-time.After(timeout):
		// Track dropped entries for monitoring
		if rm.metricsRegistry != nil {
			rm.metricsRegistry.ReplicationWALEntriesTotal.WithLabelValues("dropped").Inc()
		}
		return fmt.Errorf("%w: entry LSN %d not replicated after %v timeout", ErrWALStreamFull, entry.LSN, timeout)
	}
}

// GetReplicationState returns current replication state
func (rm *ReplicationManager) GetReplicationState() ReplicationState {
	// Get atomic heartbeat sequence first (no lock needed)
	currentSeq := rm.heartbeatSeqCounter.Load()

	rm.replicasMu.RLock()
	defer rm.replicasMu.RUnlock()

	state := ReplicationState{
		IsPrimary:    true,
		PrimaryID:    rm.primaryID,
		ReplicaCount: len(rm.replicas),
		Replicas:     make([]ReplicaStatus, 0, len(rm.replicas)),
	}

	now := time.Now()
	deadThreshold := rm.config.ReplicaDeadThreshold()

	for _, replica := range rm.replicas {
		status := rm.getReplicaStatus(replica, currentSeq, now, deadThreshold)
		state.Replicas = append(state.Replicas, status)
	}

	return state
}

// getReplicaStatus builds status for a single replica
func (rm *ReplicationManager) getReplicaStatus(replica *ReplicaConnection, currentSeq uint64, now time.Time, deadThreshold int) ReplicaStatus {
	// Read replica state with lock
	replica.mu.RLock()
	lastResponseTime := replica.lastResponseTime
	lastResponseHeartbeatSeq := replica.lastResponseHeartbeatSeq
	lastAppliedLSN := replica.lastAppliedLSN
	replica.mu.RUnlock()

	// Calculate lag using primary's local monotonic time
	lagDuration := now.Sub(lastResponseTime)

	// Calculate heartbeat lag (how many heartbeats behind)
	heartbeatLag := currentSeq - lastResponseHeartbeatSeq

	// Determine if replica is healthy based on heartbeat lag
	// Following ZeroMQ Paranoid Pirate pattern
	connected := heartbeatLag < uint64(deadThreshold)

	return ReplicaStatus{
		ReplicaID:      replica.replicaID,
		Connected:      connected,        // Based on heartbeat lag, not TCP connection
		LastSeen:       lastResponseTime, // Use primary's local time for display
		LastAppliedLSN: lastAppliedLSN,
		LagMs:          lagDuration.Milliseconds(), // Time since last response
		HeartbeatLag:   heartbeatLag,               // Logical heartbeat lag
		LagDuration:    lagDuration,
	}
}

// generateID generates a unique ID using UUID v4
// This ensures node identity survives network changes and prevents collisions
func generateID() string {
	return uuid.New().String()
}
