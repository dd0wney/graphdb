//go:build zmq
// +build zmq

package replication

import (
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
	wg        sync.WaitGroup // Tracks all goroutines for clean shutdown
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
