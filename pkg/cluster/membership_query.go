package cluster

import (
	"time"
)

// GetNode returns info about a specific node
func (cm *ClusterMembership) GetNode(nodeID string) (*NodeInfo, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	node, exists := cm.nodes[nodeID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	// Return a copy to prevent external mutations
	nodeCopy := *node
	return &nodeCopy, nil
}

// GetLocalNode returns this node's info
func (cm *ClusterMembership) GetLocalNode() *NodeInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy
	nodeCopy := *cm.localNode
	return &nodeCopy
}

// GetAllNodes returns all nodes in the cluster
func (cm *ClusterMembership) GetAllNodes() []NodeInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	nodes := make([]NodeInfo, 0, len(cm.nodes))
	for _, node := range cm.nodes {
		nodes = append(nodes, *node)
	}

	return nodes
}

// GetHealthyNodes returns nodes that have sent heartbeats recently
func (cm *ClusterMembership) GetHealthyNodes(healthTimeout time.Duration) []NodeInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	healthy := make([]NodeInfo, 0, len(cm.nodes))
	for _, node := range cm.nodes {
		if node.IsHealthy(healthTimeout) {
			healthy = append(healthy, *node)
		}
	}

	// Update metrics
	if cm.metricsRegistry != nil {
		cm.metricsRegistry.ClusterHealthyNodesTotal.Set(float64(len(healthy)))
	}

	return healthy
}

// GetPrimary returns the current primary node (if any)
func (cm *ClusterMembership) GetPrimary() *NodeInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for _, node := range cm.nodes {
		if node.Role == RolePrimary {
			nodeCopy := *node
			return &nodeCopy
		}
	}

	return nil
}

// GetNodesByRole returns all nodes with a specific role
func (cm *ClusterMembership) GetNodesByRole(role NodeRole) []NodeInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	nodes := make([]NodeInfo, 0)
	for _, node := range cm.nodes {
		if node.Role == role {
			nodes = append(nodes, *node)
		}
	}

	return nodes
}

// GetNodeCount returns the total number of nodes
func (cm *ClusterMembership) GetNodeCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return len(cm.nodes)
}

// GetHealthyNodeCount returns the number of healthy nodes
func (cm *ClusterMembership) GetHealthyNodeCount(healthTimeout time.Duration) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	count := 0
	for _, node := range cm.nodes {
		if node.IsHealthy(healthTimeout) {
			count++
		}
	}

	return count
}

// HasQuorum returns true if enough healthy nodes exist for quorum
func (cm *ClusterMembership) HasQuorum(minQuorum int, healthTimeout time.Duration) bool {
	hasQuorum := cm.GetHealthyNodeCount(healthTimeout) >= minQuorum

	// Update metrics
	if cm.metricsRegistry != nil {
		if hasQuorum {
			cm.metricsRegistry.ClusterHasQuorum.Set(1)
		} else {
			cm.metricsRegistry.ClusterHasQuorum.Set(0)
		}
	}

	return hasQuorum
}

// GetEpoch returns the current cluster epoch
func (cm *ClusterMembership) GetEpoch() uint64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.epoch
}
