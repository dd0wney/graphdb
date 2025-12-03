package cluster

import (
	"time"
)

// AddNode registers a node in the cluster
func (cm *ClusterMembership) AddNode(info NodeInfo) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.nodes[info.ID]; exists {
		return ErrNodeAlreadyExists
	}

	// Make a copy to avoid external mutations
	nodeCopy := info
	nodeCopy.LastSeen = time.Now()
	cm.nodes[info.ID] = &nodeCopy

	// Update metrics
	if cm.metricsRegistry != nil {
		cm.metricsRegistry.ClusterNodesTotal.Set(float64(len(cm.nodes)))
	}

	return nil
}

// RemoveNode removes a node from the cluster
func (cm *ClusterMembership) RemoveNode(nodeID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if nodeID == cm.localNode.ID {
		return ErrCannotRemoveSelf
	}

	if _, exists := cm.nodes[nodeID]; !exists {
		return ErrNodeNotFound
	}

	delete(cm.nodes, nodeID)

	// Update metrics
	if cm.metricsRegistry != nil {
		cm.metricsRegistry.ClusterNodesTotal.Set(float64(len(cm.nodes)))
	}

	return nil
}

// UpdateNodeHeartbeat updates the last heartbeat info for a node
func (cm *ClusterMembership) UpdateNodeHeartbeat(nodeID string, seq uint64, epoch uint64, term uint64) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node, exists := cm.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	node.LastSeen = time.Now()
	node.LastHeartbeatSeq = seq
	node.Epoch = epoch
	node.Term = term

	return nil
}

// UpdateNodeRole updates a node's role
func (cm *ClusterMembership) UpdateNodeRole(nodeID string, role NodeRole) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node, exists := cm.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	node.Role = role
	return nil
}

// UpdateNodeLSN updates a node's last known LSN
func (cm *ClusterMembership) UpdateNodeLSN(nodeID string, lsn uint64) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	node, exists := cm.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	node.LastLSN = lsn
	return nil
}

// SetLocalRole updates this node's role
func (cm *ClusterMembership) SetLocalRole(role NodeRole) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.localNode.Role = role
}

// SetLocalTerm updates this node's term
func (cm *ClusterMembership) SetLocalTerm(term uint64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.localNode.Term = term
}

// SetLocalLSN updates this node's last LSN
func (cm *ClusterMembership) SetLocalLSN(lsn uint64) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.localNode.LastLSN = lsn
}

// IncrementEpoch increments the cluster epoch (used on leadership change)
func (cm *ClusterMembership) IncrementEpoch() uint64 {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.epoch++
	cm.localNode.Epoch = cm.epoch
	return cm.epoch
}
