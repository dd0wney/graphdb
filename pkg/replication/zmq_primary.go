//go:build zmq
// +build zmq

package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
	zmq "github.com/pebbe/zmq4"
)

// ZMQReplicationManager manages replication using ZeroMQ
type ZMQReplicationManager struct {
	config    ReplicationConfig
	primaryID string
	storage   *storage.GraphStorage

	// ZeroMQ sockets
	walPublisher  *zmq.Socket // PUB socket for WAL streaming
	healthRouter  *zmq.Socket // ROUTER socket for health checks
	writeReceiver *zmq.Socket // PULL socket for write buffering

	// Replica tracking
	replicas   map[string]*ZMQReplicaInfo
	replicasMu sync.RWMutex

	// Channels
	walStream chan *wal.Entry
	stopCh    chan struct{}
	running   bool
	runningMu sync.Mutex

	// Datacenter support
	datacenters   map[string]*DatacenterLink
	datacentersMu sync.RWMutex
}

// ZMQReplicaInfo tracks ZeroMQ replica information
type ZMQReplicaInfo struct {
	ReplicaID      string
	Datacenter     string
	LastSeen       time.Time
	LastAppliedLSN uint64
	Healthy        bool
}

// DatacenterLink represents a link to another datacenter
type DatacenterLink struct {
	DatacenterID string
	PubEndpoint  string
	Publisher    *zmq.Socket
	Connected    bool
}

// NewZMQReplicationManager creates a new ZeroMQ-based replication manager
func NewZMQReplicationManager(config ReplicationConfig, storage *storage.GraphStorage) (*ZMQReplicationManager, error) {
	return &ZMQReplicationManager{
		config:      config,
		primaryID:   generateID(),
		storage:     storage,
		replicas:    make(map[string]*ZMQReplicaInfo),
		datacenters: make(map[string]*DatacenterLink),
		walStream:   make(chan *wal.Entry, config.WALBufferSize),
		stopCh:      make(chan struct{}),
	}, nil
}

// Start starts the ZeroMQ replication manager
func (zm *ZMQReplicationManager) Start() error {
	zm.runningMu.Lock()
	defer zm.runningMu.Unlock()

	if zm.running {
		return fmt.Errorf("replication manager already running")
	}

	// Create PUB socket for WAL streaming
	pub, err := zmq.NewSocket(zmq.PUB)
	if err != nil {
		return fmt.Errorf("failed to create PUB socket: %w", err)
	}

	// Bind to all interfaces for WAL streaming
	walAddr := fmt.Sprintf("tcp://*:9090")
	if err := pub.Bind(walAddr); err != nil {
		pub.Close()
		return fmt.Errorf("failed to bind PUB socket: %w", err)
	}
	zm.walPublisher = pub
	log.Printf("WAL publisher bound to %s", walAddr)

	// Create ROUTER socket for health checks
	router, err := zmq.NewSocket(zmq.ROUTER)
	if err != nil {
		zm.walPublisher.Close()
		return fmt.Errorf("failed to create ROUTER socket: %w", err)
	}

	healthAddr := fmt.Sprintf("tcp://*:9091")
	if err := router.Bind(healthAddr); err != nil {
		zm.walPublisher.Close()
		router.Close()
		return fmt.Errorf("failed to bind ROUTER socket: %w", err)
	}
	zm.healthRouter = router
	log.Printf("Health router bound to %s", healthAddr)

	// Create PULL socket for write buffering
	pull, err := zmq.NewSocket(zmq.PULL)
	if err != nil {
		zm.walPublisher.Close()
		zm.healthRouter.Close()
		return fmt.Errorf("failed to create PULL socket: %w", err)
	}

	writeAddr := fmt.Sprintf("tcp://*:9092")
	if err := pull.Bind(writeAddr); err != nil {
		zm.walPublisher.Close()
		zm.healthRouter.Close()
		pull.Close()
		return fmt.Errorf("failed to bind PULL socket: %w", err)
	}
	zm.writeReceiver = pull
	log.Printf("Write receiver bound to %s", writeAddr)

	zm.running = true

	// Start goroutines
	go zm.publishWALEntries()
	go zm.handleHealthChecks()
	go zm.handleBufferedWrites()

	log.Printf("ZeroMQ replication manager started (primary_id=%s)", zm.primaryID)

	return nil
}

// Stop stops the ZeroMQ replication manager
func (zm *ZMQReplicationManager) Stop() error {
	zm.runningMu.Lock()
	defer zm.runningMu.Unlock()

	if !zm.running {
		return nil
	}

	close(zm.stopCh)
	zm.running = false

	// Close all sockets
	if zm.walPublisher != nil {
		zm.walPublisher.Close()
	}
	if zm.healthRouter != nil {
		zm.healthRouter.Close()
	}
	if zm.writeReceiver != nil {
		zm.writeReceiver.Close()
	}

	// Close datacenter links
	zm.datacentersMu.Lock()
	for _, dc := range zm.datacenters {
		if dc.Publisher != nil {
			dc.Publisher.Close()
		}
	}
	zm.datacentersMu.Unlock()

	log.Printf("ZeroMQ replication manager stopped")

	return nil
}

// StreamWALEntry sends a WAL entry to all replicas via PUB/SUB
func (zm *ZMQReplicationManager) StreamWALEntry(entry *wal.Entry) {
	if !zm.running {
		return
	}

	select {
	case zm.walStream <- entry:
	default:
		log.Printf("Warning: WAL stream buffer full")
	}
}

// publishWALEntries publishes WAL entries to all subscribers
func (zm *ZMQReplicationManager) publishWALEntries() {
	for {
		select {
		case <-zm.stopCh:
			return
		case entry := <-zm.walStream:
			// Serialize WAL entry
			data, err := json.Marshal(entry)
			if err != nil {
				log.Printf("Failed to marshal WAL entry: %v", err)
				continue
			}

			// Publish to local replicas (PUB/SUB)
			// Topic: "WAL" for filtering
			if _, err := zm.walPublisher.SendMessage("WAL", data); err != nil {
				log.Printf("Failed to publish WAL entry: %v", err)
			}

			// Also publish to remote datacenters
			zm.publishToDatacenters(entry, data)
		}
	}
}

// publishToDatacenters publishes to remote datacenters
func (zm *ZMQReplicationManager) publishToDatacenters(entry *wal.Entry, data []byte) {
	zm.datacentersMu.RLock()
	defer zm.datacentersMu.RUnlock()

	for _, dc := range zm.datacenters {
		if dc.Connected && dc.Publisher != nil {
			if _, err := dc.Publisher.SendMessage("WAL", data); err != nil {
				log.Printf("Failed to publish to datacenter %s: %v", dc.DatacenterID, err)
			}
		}
	}
}

// handleHealthChecks handles health check requests from replicas
func (zm *ZMQReplicationManager) handleHealthChecks() {
	for {
		select {
		case <-zm.stopCh:
			return
		default:
		}

		// Receive health check with timeout
		zm.healthRouter.SetRcvtimeo(1 * time.Second)
		msg, err := zm.healthRouter.RecvMessage(0)
		if err != nil {
			// Timeout or error, continue
			continue
		}

		if len(msg) < 2 {
			continue
		}

		// ROUTER gives us: [identity, empty, payload]
		identity := msg[0]
		payload := msg[len(msg)-1]

		// Parse health check message
		var hc HeartbeatMessage
		if err := json.Unmarshal([]byte(payload), &hc); err != nil {
			log.Printf("Failed to parse health check: %v", err)
			continue
		}

		// Update replica info
		zm.replicasMu.Lock()
		zm.replicas[hc.From] = &ZMQReplicaInfo{
			ReplicaID:      hc.From,
			LastSeen:       time.Now(),
			LastAppliedLSN: hc.CurrentLSN,
			Healthy:        true,
		}
		zm.replicasMu.Unlock()

		// Send health check response
		stats := zm.storage.GetStatistics()
		response := HeartbeatMessage{
			From:       zm.primaryID,
			CurrentLSN: zm.storage.GetCurrentLSN(),
			NodeCount:  stats.NodeCount,
			EdgeCount:  stats.EdgeCount,
		}

		responseData, _ := json.Marshal(response)
		zm.healthRouter.SendMessage(identity, "", responseData)
	}
}

// handleBufferedWrites handles writes from PUSH sockets
func (zm *ZMQReplicationManager) handleBufferedWrites() {
	for {
		select {
		case <-zm.stopCh:
			return
		default:
		}

		// Receive write operation with timeout
		zm.writeReceiver.SetRcvtimeo(1 * time.Second)
		msg, err := zm.writeReceiver.RecvMessage(0)
		if err != nil {
			continue
		}

		if len(msg) == 0 {
			continue
		}

		// Parse write operation
		var writeOp WriteOperation
		if err := json.Unmarshal([]byte(msg[0]), &writeOp); err != nil {
			log.Printf("Failed to parse write operation: %v", err)
			continue
		}

		// Execute write operation
		zm.executeWriteOperation(&writeOp)
	}
}

// AddDatacenterLink adds a link to another datacenter
func (zm *ZMQReplicationManager) AddDatacenterLink(datacenterID, pubEndpoint string) error {
	zm.datacentersMu.Lock()
	defer zm.datacentersMu.Unlock()

	// Create PUB socket for inter-datacenter replication
	pub, err := zmq.NewSocket(zmq.PUB)
	if err != nil {
		return fmt.Errorf("failed to create datacenter PUB socket: %w", err)
	}

	if err := pub.Connect(pubEndpoint); err != nil {
		pub.Close()
		return fmt.Errorf("failed to connect to datacenter: %w", err)
	}

	zm.datacenters[datacenterID] = &DatacenterLink{
		DatacenterID: datacenterID,
		PubEndpoint:  pubEndpoint,
		Publisher:    pub,
		Connected:    true,
	}

	log.Printf("Added datacenter link: %s -> %s", datacenterID, pubEndpoint)

	return nil
}

// WriteOperation represents a buffered write operation
type WriteOperation struct {
	Type       string                   `json:"type"` // "create_node", "create_edge"
	Labels     []string                 `json:"labels,omitempty"`
	Properties map[string]storage.Value `json:"properties,omitempty"`
	FromNodeID uint64                   `json:"from_node_id,omitempty"`
	ToNodeID   uint64                   `json:"to_node_id,omitempty"`
	EdgeType   string                   `json:"edge_type,omitempty"`
	Weight     float64                  `json:"weight,omitempty"`
}

// executeWriteOperation executes a buffered write operation
func (zm *ZMQReplicationManager) executeWriteOperation(op *WriteOperation) {
	switch op.Type {
	case "create_node":
		if _, err := zm.storage.CreateNode(op.Labels, op.Properties); err != nil {
			log.Printf("Failed to create node: %v", err)
		}
	case "create_edge":
		if _, err := zm.storage.CreateEdge(op.FromNodeID, op.ToNodeID, op.EdgeType, op.Properties, op.Weight); err != nil {
			log.Printf("Failed to create edge: %v", err)
		}
	}
}

// GetReplicationState returns current replication state
func (zm *ZMQReplicationManager) GetReplicationState() ReplicationState {
	zm.replicasMu.RLock()
	defer zm.replicasMu.RUnlock()

	state := ReplicationState{
		IsPrimary:    true,
		PrimaryID:    zm.primaryID,
		ReplicaCount: len(zm.replicas),
		Replicas:     make([]ReplicaStatus, 0, len(zm.replicas)),
	}

	for _, replica := range zm.replicas {
		state.Replicas = append(state.Replicas, ReplicaStatus{
			ReplicaID:      replica.ReplicaID,
			Connected:      replica.Healthy,
			LastSeen:       replica.LastSeen,
			LastAppliedLSN: replica.LastAppliedLSN,
		})
	}

	return state
}
