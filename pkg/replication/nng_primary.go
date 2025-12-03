//go:build nng
// +build nng

package replication

import (
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
	"go.nanomsg.org/mangos/v3/protocol/pub"
	"go.nanomsg.org/mangos/v3/protocol/pull"
	"go.nanomsg.org/mangos/v3/protocol/surveyor"

	// Register transports
	_ "go.nanomsg.org/mangos/v3/transport/all"
)

// NewNNGReplicationManager creates a new NNG-based replication manager
func NewNNGReplicationManager(config ReplicationConfig, storage *storage.GraphStorage) (*NNGReplicationManager, error) {
	return &NNGReplicationManager{
		config:      config,
		primaryID:   generateID(),
		storage:     storage,
		replicas:    make(map[string]*NNGReplicaInfo),
		datacenters: make(map[string]*NNGDatacenterLink),
		walStream:   make(chan *wal.Entry, config.WALBufferSize),
		stopCh:      make(chan struct{}),
	}, nil
}

// Start starts the NNG replication manager
func (nm *NNGReplicationManager) Start() error {
	nm.runningMu.Lock()
	defer nm.runningMu.Unlock()

	if nm.running {
		return fmt.Errorf("replication manager already running")
	}

	// Use cleanup helper to ensure all resources are closed on error
	cleanup := NewResourceCleanup()
	defer cleanup.Cleanup()

	// Create PUB socket for WAL streaming
	pubSock, err := pub.NewSocket()
	if err != nil {
		return fmt.Errorf("failed to create PUB socket: %w", err)
	}
	cleanup.Add(pubSock, "WAL publisher")

	// Bind to all interfaces for WAL streaming
	walAddr := "tcp://*:9090"
	if err := pubSock.Listen(walAddr); err != nil {
		return fmt.Errorf("failed to bind PUB socket: %w", err)
	}
	nm.walPublisher = pubSock
	log.Printf("WAL publisher bound to %s", walAddr)

	// Create SURVEYOR socket for health checks (broadcasts to all replicas)
	survSock, err := surveyor.NewSocket()
	if err != nil {
		return fmt.Errorf("failed to create SURVEYOR socket: %w", err)
	}
	cleanup.Add(survSock, "health surveyor")

	healthAddr := "tcp://*:9091"
	if err := survSock.Listen(healthAddr); err != nil {
		return fmt.Errorf("failed to bind SURVEYOR socket: %w", err)
	}
	nm.healthSurveyor = survSock
	log.Printf("Health surveyor bound to %s", healthAddr)

	// Create PULL socket for write buffering
	pullSock, err := pull.NewSocket()
	if err != nil {
		return fmt.Errorf("failed to create PULL socket: %w", err)
	}
	cleanup.Add(pullSock, "write receiver")

	writeAddr := "tcp://*:9092"
	if err := pullSock.Listen(writeAddr); err != nil {
		return fmt.Errorf("failed to bind PULL socket: %w", err)
	}
	nm.writeReceiver = pullSock
	log.Printf("Write receiver bound to %s", writeAddr)

	nm.running = true

	// Start goroutines
	nm.wg.Add(3)
	go nm.publishWALEntries()
	go nm.handleHealthChecks()
	go nm.handleBufferedWrites()

	log.Printf("NNG replication manager started (primary_id=%s)", nm.primaryID)

	// Success - prevent cleanup from closing resources
	cleanup.Clear()

	return nil
}

// Stop stops the NNG replication manager
func (nm *NNGReplicationManager) Stop() error {
	nm.runningMu.Lock()
	defer nm.runningMu.Unlock()

	if !nm.running {
		return nil
	}

	close(nm.stopCh)
	nm.running = false

	// Close all sockets
	if nm.walPublisher != nil {
		if err := nm.walPublisher.Close(); err != nil {
			log.Printf("Warning: Failed to close WAL publisher: %v", err)
		}
	}
	if nm.healthSurveyor != nil {
		if err := nm.healthSurveyor.Close(); err != nil {
			log.Printf("Warning: Failed to close health surveyor: %v", err)
		}
	}
	if nm.writeReceiver != nil {
		if err := nm.writeReceiver.Close(); err != nil {
			log.Printf("Warning: Failed to close write receiver: %v", err)
		}
	}

	// Close datacenter links
	nm.datacentersMu.Lock()
	for _, dc := range nm.datacenters {
		if dc.Publisher != nil {
			if err := dc.Publisher.Close(); err != nil {
				log.Printf("Warning: Failed to close datacenter publisher: %v", err)
			}
		}
	}
	nm.datacentersMu.Unlock()

	// Wait for all goroutines to complete
	nm.wg.Wait()

	log.Printf("NNG replication manager stopped")

	return nil
}

// StreamWALEntry sends a WAL entry to all replicas via PUB/SUB.
func (nm *NNGReplicationManager) StreamWALEntry(entry *wal.Entry) error {
	if !nm.running {
		return fmt.Errorf("replication manager not running")
	}

	select {
	case nm.walStream <- entry:
		return nil
	case <-nm.stopCh:
		return fmt.Errorf("replication manager stopped")
	}
}

// AddDatacenterLink adds a link to another datacenter
func (nm *NNGReplicationManager) AddDatacenterLink(datacenterID, pubEndpoint string) error {
	nm.datacentersMu.Lock()
	defer nm.datacentersMu.Unlock()

	// Create PUB socket for inter-datacenter replication
	dcPubSock, err := pub.NewSocket()
	if err != nil {
		return fmt.Errorf("failed to create datacenter PUB socket: %w", err)
	}

	if err := dcPubSock.Dial(pubEndpoint); err != nil {
		dcPubSock.Close()
		return fmt.Errorf("failed to connect to datacenter: %w", err)
	}

	nm.datacenters[datacenterID] = &NNGDatacenterLink{
		DatacenterID: datacenterID,
		PubEndpoint:  pubEndpoint,
		Publisher:    dcPubSock,
		Connected:    true,
	}

	log.Printf("Added datacenter link: %s -> %s", datacenterID, pubEndpoint)

	return nil
}

// GetReplicationState returns current replication state
func (nm *NNGReplicationManager) GetReplicationState() ReplicationState {
	nm.replicasMu.RLock()
	defer nm.replicasMu.RUnlock()

	state := ReplicationState{
		IsPrimary:    true,
		PrimaryID:    nm.primaryID,
		ReplicaCount: len(nm.replicas),
		Replicas:     make([]ReplicaStatus, 0, len(nm.replicas)),
	}

	for _, replica := range nm.replicas {
		state.Replicas = append(state.Replicas, ReplicaStatus{
			ReplicaID:      replica.ReplicaID,
			Connected:      replica.Healthy,
			LastSeen:       replica.LastSeen,
			LastAppliedLSN: replica.LastAppliedLSN,
		})
	}

	return state
}
