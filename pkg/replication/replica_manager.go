package replication

import (
	"fmt"
	"log"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// ReplicaManager coordinates replication components for a replica node.
// It composes WALSubscriber, HealthRespondent, and WriteForwarder.
type ReplicaManager struct {
	id              string
	primaryID       string
	walSubscriber   *WALSubscriber
	healthRespond   *HealthRespondent
	writeForwarder  *WriteForwarder
	lastAppliedLSN  uint64
	lsnMu           sync.RWMutex
	running         bool
	runningMu       sync.Mutex
}

// ReplicaManagerConfig configures the replica manager.
type ReplicaManagerConfig struct {
	ReplicaID   string // If empty, auto-generated
	PrimaryAddr string // e.g., "localhost:9090"
}

// NewReplicaManager creates a new replica manager with the given dependencies.
func NewReplicaManager(
	factory SocketFactory,
	config ReplicaManagerConfig,
	walApplier WALApplier,
) (*ReplicaManager, error) {
	id := config.ReplicaID
	if id == "" {
		id = generateID()
	}

	// Extract host for other ports
	host := extractHostFromAddr(config.PrimaryAddr)
	walAddr := fmt.Sprintf("tcp://%s", config.PrimaryAddr)
	healthAddr := fmt.Sprintf("tcp://%s:9091", host)
	writeAddr := fmt.Sprintf("tcp://%s:9092", host)

	// Create replica manager first so we can use it as state provider
	rm := &ReplicaManager{
		id: id,
	}

	// Create WAL subscriber
	walSub, err := NewWALSubscriber(factory, WALSubscriberConfig{
		PrimaryAddr: walAddr,
	}, &replicaWALApplier{rm: rm, applier: walApplier})
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL subscriber: %w", err)
	}

	// Create health respondent
	healthResp, err := NewHealthRespondent(factory, HealthRespondentConfig{
		PrimaryAddr: healthAddr,
	}, rm)
	if err != nil {
		walSub.socket.Close()
		return nil, fmt.Errorf("failed to create health respondent: %w", err)
	}

	// Create write forwarder
	writeFwd, err := NewWriteForwarder(factory, WriteForwarderConfig{
		PrimaryAddr: writeAddr,
	})
	if err != nil {
		walSub.socket.Close()
		healthResp.socket.Close()
		return nil, fmt.Errorf("failed to create write forwarder: %w", err)
	}

	rm.walSubscriber = walSub
	rm.healthRespond = healthResp
	rm.writeForwarder = writeFwd

	return rm, nil
}

// Start starts all replication components.
func (m *ReplicaManager) Start() error {
	m.runningMu.Lock()
	defer m.runningMu.Unlock()

	if m.running {
		return fmt.Errorf("replica manager already running")
	}

	// Start components
	if err := m.walSubscriber.Start(); err != nil {
		return fmt.Errorf("failed to start WAL subscriber: %w", err)
	}

	if err := m.healthRespond.Start(); err != nil {
		m.walSubscriber.Stop()
		return fmt.Errorf("failed to start health respondent: %w", err)
	}

	if err := m.writeForwarder.Start(); err != nil {
		m.walSubscriber.Stop()
		m.healthRespond.Stop()
		return fmt.Errorf("failed to start write forwarder: %w", err)
	}

	m.running = true
	log.Printf("Replica manager started (id=%s)", m.id)
	return nil
}

// Stop stops all replication components.
func (m *ReplicaManager) Stop() error {
	m.runningMu.Lock()
	defer m.runningMu.Unlock()

	if !m.running {
		return nil
	}

	m.writeForwarder.Stop()
	m.healthRespond.Stop()
	m.walSubscriber.Stop()

	m.running = false
	log.Printf("Replica manager stopped")
	return nil
}

// ForwardWrite forwards a write operation to the primary.
func (m *ReplicaManager) ForwardWrite(op *WriteOperation) error {
	return m.writeForwarder.Forward(op)
}

// GetReplicationState returns the current replication state.
func (m *ReplicaManager) GetReplicationState() ReplicationState {
	m.lsnMu.RLock()
	lsn := m.lastAppliedLSN
	m.lsnMu.RUnlock()

	return ReplicationState{
		IsPrimary:  false,
		PrimaryID:  m.primaryID,
		CurrentLSN: lsn,
	}
}

// StateProvider implementation for HealthRespondent
func (m *ReplicaManager) GetID() string {
	return m.id
}

func (m *ReplicaManager) GetCurrentLSN() uint64 {
	m.lsnMu.RLock()
	defer m.lsnMu.RUnlock()
	return m.lastAppliedLSN
}

func (m *ReplicaManager) GetNodeCount() uint64 {
	return 0 // Replica doesn't track this directly
}

func (m *ReplicaManager) GetEdgeCount() uint64 {
	return 0 // Replica doesn't track this directly
}

// setLastAppliedLSN updates the last applied LSN.
func (m *ReplicaManager) setLastAppliedLSN(lsn uint64) {
	m.lsnMu.Lock()
	m.lastAppliedLSN = lsn
	m.lsnMu.Unlock()
}

// replicaWALApplier wraps a WALApplier and updates the replica's LSN.
type replicaWALApplier struct {
	rm      *ReplicaManager
	applier WALApplier
}

func (a *replicaWALApplier) ApplyWALEntry(entry *wal.Entry) error {
	if err := a.applier.ApplyWALEntry(entry); err != nil {
		return err
	}
	a.rm.setLastAppliedLSN(entry.LSN)
	return nil
}

// extractHost extracts host from address (e.g., "localhost:9090" -> "localhost")
func extractHostFromAddr(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

// Ensure ReplicaManager implements StateProvider
var _ StateProvider = (*ReplicaManager)(nil)
