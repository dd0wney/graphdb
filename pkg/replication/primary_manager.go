package replication

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// PrimaryManager coordinates replication components for a primary node.
// It composes WALPublisher, HealthSurveyor, and WriteReceiver.
type PrimaryManager struct {
	id            string
	walPublisher  *WALPublisher
	healthSurvey  *HealthSurveyor
	writeReceiver *WriteReceiver
	replicas      *replicaRegistry
	running       bool
	runningMu     sync.Mutex
}

// PrimaryManagerConfig configures the primary manager.
type PrimaryManagerConfig struct {
	Transport      TransportConfig
	WALBufferSize  int
	SurveyInterval time.Duration
	SurveyTimeout  time.Duration
}

// DefaultPrimaryManagerConfig returns sensible defaults.
func DefaultPrimaryManagerConfig() PrimaryManagerConfig {
	return PrimaryManagerConfig{
		Transport:      DefaultTransportConfig(),
		WALBufferSize:  1000,
		SurveyInterval: 5 * time.Second,
		SurveyTimeout:  2 * time.Second,
	}
}

// NewPrimaryManager creates a new primary manager with the given dependencies.
func NewPrimaryManager(
	factory SocketFactory,
	config PrimaryManagerConfig,
	stateProvider StateProvider,
	writeExecutor WriteExecutor,
) (*PrimaryManager, error) {
	id := generateID()
	replicas := newReplicaRegistry()

	// Create WAL publisher
	walPub, err := NewWALPublisher(factory, WALPublisherConfig{
		Address:    config.Transport.WALPublishAddr,
		BufferSize: config.WALBufferSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL publisher: %w", err)
	}

	// Create state provider wrapper that includes our ID
	wrappedState := &primaryStateProvider{
		id:       id,
		provider: stateProvider,
	}

	// Create health surveyor
	healthSurvey, err := NewHealthSurveyor(factory, HealthSurveyorConfig{
		Address:       config.Transport.HealthSurveyAddr,
		Interval:      config.SurveyInterval,
		SurveyTimeout: config.SurveyTimeout,
	}, wrappedState, replicas)
	if err != nil {
		walPub.socket.Close()
		return nil, fmt.Errorf("failed to create health surveyor: %w", err)
	}

	// Create write receiver
	writeRcv, err := NewWriteReceiver(factory, WriteReceiverConfig{
		Address: config.Transport.WriteBufferAddr,
	}, writeExecutor)
	if err != nil {
		walPub.socket.Close()
		healthSurvey.socket.Close()
		return nil, fmt.Errorf("failed to create write receiver: %w", err)
	}

	return &PrimaryManager{
		id:            id,
		walPublisher:  walPub,
		healthSurvey:  healthSurvey,
		writeReceiver: writeRcv,
		replicas:      replicas,
	}, nil
}

// Start starts all replication components.
func (m *PrimaryManager) Start() error {
	m.runningMu.Lock()
	defer m.runningMu.Unlock()

	if m.running {
		return fmt.Errorf("primary manager already running")
	}

	// Start components in order
	if err := m.walPublisher.Start(); err != nil {
		return fmt.Errorf("failed to start WAL publisher: %w", err)
	}

	if err := m.healthSurvey.Start(); err != nil {
		m.walPublisher.Stop()
		return fmt.Errorf("failed to start health surveyor: %w", err)
	}

	if err := m.writeReceiver.Start(); err != nil {
		m.walPublisher.Stop()
		m.healthSurvey.Stop()
		return fmt.Errorf("failed to start write receiver: %w", err)
	}

	m.running = true
	log.Printf("Primary manager started (id=%s)", m.id)
	return nil
}

// Stop stops all replication components.
func (m *PrimaryManager) Stop() error {
	m.runningMu.Lock()
	defer m.runningMu.Unlock()

	if !m.running {
		return nil
	}

	// Stop in reverse order
	m.writeReceiver.Stop()
	m.healthSurvey.Stop()
	m.walPublisher.Stop()

	m.running = false
	log.Printf("Primary manager stopped")
	return nil
}

// StreamWALEntry publishes a WAL entry to replicas.
func (m *PrimaryManager) StreamWALEntry(entry *wal.Entry) error {
	return m.walPublisher.Publish(entry)
}

// GetReplicationState returns the current replication state.
func (m *PrimaryManager) GetReplicationState() ReplicationState {
	replicas := m.replicas.GetReplicas()
	statuses := make([]ReplicaStatus, 0, len(replicas))
	for _, r := range replicas {
		statuses = append(statuses, ReplicaStatus{
			ReplicaID:      r.ReplicaID,
			Connected:      r.Healthy,
			LastSeen:       r.LastSeen,
			LastAppliedLSN: r.LastAppliedLSN,
		})
	}

	return ReplicationState{
		IsPrimary:    true,
		PrimaryID:    m.id,
		ReplicaCount: len(replicas),
		Replicas:     statuses,
	}
}

// GetID returns the primary's ID.
func (m *PrimaryManager) GetID() string {
	return m.id
}

// primaryStateProvider wraps a StateProvider with the primary's ID.
type primaryStateProvider struct {
	id       string
	provider StateProvider
}

func (p *primaryStateProvider) GetID() string          { return p.id }
func (p *primaryStateProvider) GetCurrentLSN() uint64  { return p.provider.GetCurrentLSN() }
func (p *primaryStateProvider) GetNodeCount() uint64   { return p.provider.GetNodeCount() }
func (p *primaryStateProvider) GetEdgeCount() uint64   { return p.provider.GetEdgeCount() }

// replicaRegistry tracks replica state.
type replicaRegistry struct {
	replicas map[string]*ReplicaInfo
	mu       sync.RWMutex
}

func newReplicaRegistry() *replicaRegistry {
	return &replicaRegistry{
		replicas: make(map[string]*ReplicaInfo),
	}
}

func (r *replicaRegistry) UpdateReplica(id string, lsn uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replicas[id] = &ReplicaInfo{
		ReplicaID:      id,
		LastSeen:       time.Now(),
		LastAppliedLSN: lsn,
		Healthy:        true,
	}
}

func (r *replicaRegistry) MarkUnhealthy(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if replica, ok := r.replicas[id]; ok {
		replica.Healthy = false
	}
}

func (r *replicaRegistry) GetReplicas() []ReplicaInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ReplicaInfo, 0, len(r.replicas))
	for _, replica := range r.replicas {
		result = append(result, *replica)
	}
	return result
}

// Ensure replicaRegistry implements ReplicaTracker
var _ ReplicaTracker = (*replicaRegistry)(nil)
