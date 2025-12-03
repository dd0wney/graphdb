package admin

import (
	"log"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/cluster"
	"github.com/dd0wney/cluso-graphdb/pkg/replication"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// UpgradeManager handles cluster upgrade operations
type UpgradeManager struct {
	storage        *storage.GraphStorage
	replication    *replication.ReplicationManager
	replica        ReplicaInterface
	isPrimary      bool
	config         UpgradeConfig
	mu             sync.RWMutex
	electionMgr    *cluster.ElectionManager // Optional - only set if cluster mode enabled
	clusterEnabled bool
}

// NewUpgradeManager creates a new upgrade manager
func NewUpgradeManager(storage *storage.GraphStorage, replication *replication.ReplicationManager, replica ReplicaInterface, isPrimary bool, config UpgradeConfig) *UpgradeManager {
	// Set default port if not specified
	if config.ReplicationPort == 0 {
		config.ReplicationPort = 9090
	}

	return &UpgradeManager{
		storage:     storage,
		replication: replication,
		replica:     replica,
		isPrimary:   isPrimary,
		config:      config,
	}
}

// SetElectionManager enables cluster mode with an election manager
// This allows manual promotions to coordinate with automatic failover
func (um *UpgradeManager) SetElectionManager(electionMgr *cluster.ElectionManager) {
	um.mu.Lock()
	defer um.mu.Unlock()

	um.electionMgr = electionMgr
	um.clusterEnabled = electionMgr != nil

	if um.clusterEnabled {
		log.Printf("Cluster mode enabled for UpgradeManager - manual promotions will trigger elections")
	}
}

func (um *UpgradeManager) getCurrentRole() string {
	if um.isPrimary {
		return "primary"
	} else if um.replica != nil {
		return "replica"
	}
	return "standalone"
}
