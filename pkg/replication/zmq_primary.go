//go:build zmq
// +build zmq

package replication

import (
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
	zmq "github.com/pebbe/zmq4"
)

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

	// Use cleanup helper to ensure all resources are closed on error
	cleanup := NewResourceCleanup()
	defer cleanup.Cleanup()

	// Create PUB socket for WAL streaming
	pub, err := zmq.NewSocket(zmq.PUB)
	if err != nil {
		return fmt.Errorf("failed to create PUB socket: %w", err)
	}
	cleanup.Add(pub, "WAL publisher")

	// Bind to all interfaces for WAL streaming
	walAddr := fmt.Sprintf("tcp://*:9090")
	if err := pub.Bind(walAddr); err != nil {
		return fmt.Errorf("failed to bind PUB socket: %w", err)
	}
	zm.walPublisher = pub
	log.Printf("WAL publisher bound to %s", walAddr)

	// Create ROUTER socket for health checks
	router, err := zmq.NewSocket(zmq.ROUTER)
	if err != nil {
		return fmt.Errorf("failed to create ROUTER socket: %w", err)
	}
	cleanup.Add(router, "health router")

	healthAddr := fmt.Sprintf("tcp://*:9091")
	if err := router.Bind(healthAddr); err != nil {
		return fmt.Errorf("failed to bind ROUTER socket: %w", err)
	}
	zm.healthRouter = router
	log.Printf("Health router bound to %s", healthAddr)

	// Create PULL socket for write buffering
	pull, err := zmq.NewSocket(zmq.PULL)
	if err != nil {
		return fmt.Errorf("failed to create PULL socket: %w", err)
	}
	cleanup.Add(pull, "write receiver")

	writeAddr := fmt.Sprintf("tcp://*:9092")
	if err := pull.Bind(writeAddr); err != nil {
		return fmt.Errorf("failed to bind PULL socket: %w", err)
	}
	zm.writeReceiver = pull
	log.Printf("Write receiver bound to %s", writeAddr)

	zm.running = true

	// Start goroutines
	zm.wg.Add(1)
	go zm.publishWALEntries()
	zm.wg.Add(1)
	go zm.handleHealthChecks()
	zm.wg.Add(1)
	go zm.handleBufferedWrites()

	log.Printf("ZeroMQ replication manager started (primary_id=%s)", zm.primaryID)

	// Success - prevent cleanup from closing resources
	cleanup.Clear()

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
		if err := zm.walPublisher.Close(); err != nil {
			log.Printf("Warning: Failed to close WAL publisher: %v", err)
		}
	}
	if zm.healthRouter != nil {
		if err := zm.healthRouter.Close(); err != nil {
			log.Printf("Warning: Failed to close health router: %v", err)
		}
	}
	if zm.writeReceiver != nil {
		if err := zm.writeReceiver.Close(); err != nil {
			log.Printf("Warning: Failed to close write receiver: %v", err)
		}
	}

	// Close datacenter links
	zm.datacentersMu.Lock()
	for _, dc := range zm.datacenters {
		if dc.Publisher != nil {
			if err := dc.Publisher.Close(); err != nil {
				log.Printf("Warning: Failed to close datacenter publisher: %v", err)
			}
		}
	}
	zm.datacentersMu.Unlock()

	// Wait for all goroutines to complete
	zm.wg.Wait()

	log.Printf("ZeroMQ replication manager stopped")

	return nil
}

// StreamWALEntry sends a WAL entry to all replicas via PUB/SUB.
// This method blocks if the buffer is full to ensure durability.
// Returns an error if the manager is not running.
func (zm *ZMQReplicationManager) StreamWALEntry(entry *wal.Entry) error {
	if !zm.running {
		return fmt.Errorf("replication manager not running")
	}

	select {
	case zm.walStream <- entry:
		return nil
	case <-zm.stopCh:
		return fmt.Errorf("replication manager stopped")
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
		if closeErr := pub.Close(); closeErr != nil {
			log.Printf("Warning: Failed to close datacenter PUB socket after connect error: %v", closeErr)
		}
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
