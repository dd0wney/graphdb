//go:build nng
// +build nng

package replication

import (
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
	"go.nanomsg.org/mangos/v3"
)

// NNGReplicationManager manages replication using NNG/mangos (pure Go)
type NNGReplicationManager struct {
	config    ReplicationConfig
	primaryID string
	storage   *storage.GraphStorage

	// NNG sockets
	walPublisher   mangos.Socket // PUB socket for WAL streaming
	healthSurveyor mangos.Socket // SURVEYOR socket for health checks (broadcasts to all replicas)
	writeReceiver  mangos.Socket // PULL socket for write buffering

	// Replica tracking
	replicas   map[string]*NNGReplicaInfo
	replicasMu sync.RWMutex

	// Channels
	walStream chan *wal.Entry
	stopCh    chan struct{}
	wg        sync.WaitGroup
	running   bool
	runningMu sync.Mutex

	// Datacenter support
	datacenters   map[string]*NNGDatacenterLink
	datacentersMu sync.RWMutex
}

// NNGReplicaInfo tracks NNG replica information
type NNGReplicaInfo struct {
	ReplicaID      string
	Datacenter     string
	LastSeen       time.Time
	LastAppliedLSN uint64
	Healthy        bool
}

// NNGDatacenterLink represents a link to another datacenter
type NNGDatacenterLink struct {
	DatacenterID string
	PubEndpoint  string
	Publisher    mangos.Socket
	Connected    bool
}
