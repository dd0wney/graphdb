package cluster

import (
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
)

// NodeRole represents the role of a node in the cluster
type NodeRole int

const (
	// RoleReplica is a follower node that replicates data
	RoleReplica NodeRole = iota
	// RoleCandidate is a node in the process of election
	RoleCandidate
	// RolePrimary is the elected leader that accepts writes
	RolePrimary
)

// String returns the string representation of a NodeRole
func (r NodeRole) String() string {
	switch r {
	case RoleReplica:
		return "replica"
	case RoleCandidate:
		return "candidate"
	case RolePrimary:
		return "primary"
	default:
		return "unknown"
	}
}

// NodeInfo contains information about a cluster node
type NodeInfo struct {
	ID               string    // Unique node identifier
	Addr             string    // Network address (host:port)
	Role             NodeRole  // Current role in cluster
	LastSeen         time.Time // Last heartbeat received
	LastHeartbeatSeq uint64    // Last heartbeat sequence number
	Epoch            uint64    // Cluster generation number
	Term             uint64    // Election term
	LastLSN          uint64    // Last known LSN for this node
	Priority         int       // Election priority
}

// IsHealthy returns true if the node has been seen recently
func (n *NodeInfo) IsHealthy(timeout time.Duration) bool {
	return time.Since(n.LastSeen) < timeout
}

// ClusterMembership tracks all nodes in the cluster
//
// Concurrent Safety:
// 1. All public methods use RWMutex for thread-safe access
// 2. Read operations (GetXxx) use RLock for concurrent reads
// 3. Write operations (AddNode, UpdateXxx, RemoveNode) use Lock
// 4. Map iteration creates defensive copies to avoid holding lock
type ClusterMembership struct {
	nodes           map[string]*NodeInfo // nodeID -> NodeInfo
	localNode       *NodeInfo            // This node's info
	epoch           uint64               // Current cluster generation
	mu              sync.RWMutex         // Protects all fields
	metricsRegistry *metrics.Registry    // Metrics tracking
}

// NewClusterMembership creates a new membership tracker
func NewClusterMembership(localNodeID string, localAddr string) *ClusterMembership {
	localNode := &NodeInfo{
		ID:       localNodeID,
		Addr:     localAddr,
		Role:     RoleReplica,
		LastSeen: time.Now(),
		Epoch:    0,
		Term:     0,
		Priority: 1,
	}

	cm := &ClusterMembership{
		nodes:           make(map[string]*NodeInfo),
		localNode:       localNode,
		epoch:           0,
		metricsRegistry: metrics.DefaultRegistry(),
	}

	// Add self to membership
	cm.nodes[localNodeID] = localNode

	// Initialize metrics
	if cm.metricsRegistry != nil {
		cm.metricsRegistry.ClusterNodesTotal.Set(float64(len(cm.nodes)))
	}

	return cm
}
