package replication

import (
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/cluster"
)

// EnableCluster initializes cluster support for high availability
func (rm *ReplicationManager) EnableCluster(clusterConfig cluster.ClusterConfig) error {
	// Initialize cluster membership
	rm.membership = cluster.NewClusterMembership(clusterConfig.NodeID, clusterConfig.NodeAddr)

	// Initialize election manager
	rm.electionMgr = cluster.NewElectionManager(clusterConfig, rm.membership)

	// Initialize node discovery
	rm.discovery = cluster.NewNodeDiscovery(clusterConfig, rm.membership)

	// Set callbacks for role changes
	rm.electionMgr.SetCallbacks(
		rm.onBecomeLeader,
		rm.onBecomeFollower,
		nil, // No special candidate callback needed
	)

	rm.clusterEnabled = true
	log.Printf("Cluster support enabled (node_id=%s)", clusterConfig.NodeID)

	return nil
}

// onBecomeLeader is called when this node becomes the primary
func (rm *ReplicationManager) onBecomeLeader() {
	log.Printf("Became cluster leader - accepting writes")
	// Primary status will be reflected in heartbeats via membership
}

// onBecomeFollower is called when this node becomes a replica
func (rm *ReplicationManager) onBecomeFollower() {
	log.Printf("Became follower - stepping down from primary")
	// Stop accepting replication connections
	if err := rm.Stop(); err != nil {
		log.Printf("Error stopping replication manager during step down: %v", err)
	}
}

// startClusterComponents starts cluster-related components
func (rm *ReplicationManager) startClusterComponents() {
	if err := rm.discovery.Start(); err != nil {
		log.Printf("Warning: Failed to start node discovery: %v", err)
	}
	if err := rm.electionMgr.Start(); err != nil {
		log.Printf("Warning: Failed to start election manager: %v", err)
	}
}

// stopClusterComponents stops cluster-related components
func (rm *ReplicationManager) stopClusterComponents() []error {
	var errors []error
	if rm.electionMgr != nil {
		if err := rm.electionMgr.Stop(); err != nil {
			errors = append(errors, err)
		}
	}
	if rm.discovery != nil {
		if err := rm.discovery.Stop(); err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}
