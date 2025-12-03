package cluster

import "time"

// ClusterConfig defines configuration for cluster management and high availability
type ClusterConfig struct {
	// Node identification
	NodeID   string // Unique identifier for this node
	NodeAddr string // Address other nodes can reach this node at (host:port)

	// Seed nodes for initial discovery
	SeedNodes []string // List of seed node addresses for bootstrapping

	// Election configuration
	ElectionTimeout    time.Duration // Time without heartbeat before starting election (default: 5s)
	HeartbeatInterval  time.Duration // Interval between heartbeats (default: 1s)
	MinQuorumSize      int           // Minimum nodes for quorum (typically N/2 + 1)
	Priority           int           // Election priority (higher wins ties)

	// Feature flags for gradual rollout
	EnableAutoFailover bool // Enable automatic failover (default: false)
	EnableQuorumWrites bool // Enable quorum-based writes (default: false)

	// Timeouts and limits
	VoteRequestTimeout time.Duration // Timeout waiting for vote responses (default: 2s)
	MaxElectionRetries int           // Maximum consecutive election failures before backing off
}

// DefaultClusterConfig returns a safe default configuration
func DefaultClusterConfig() ClusterConfig {
	return ClusterConfig{
		ElectionTimeout:    5 * time.Second,
		HeartbeatInterval:  1 * time.Second,
		MinQuorumSize:      2, // For 3-node cluster
		Priority:           1,
		EnableAutoFailover: false, // Disabled by default for safety
		EnableQuorumWrites: false,
		VoteRequestTimeout: 2 * time.Second,
		MaxElectionRetries: 3,
	}
}

// Validate checks if configuration is valid
func (c *ClusterConfig) Validate() error {
	if c.NodeID == "" {
		return ErrInvalidNodeID
	}
	if c.NodeAddr == "" {
		return ErrInvalidNodeAddr
	}
	if c.ElectionTimeout < c.HeartbeatInterval {
		return ErrElectionTimeoutTooSmall
	}
	if c.MinQuorumSize < 1 {
		return ErrInvalidQuorumSize
	}
	if len(c.SeedNodes) == 0 && c.EnableAutoFailover {
		return ErrNoSeedNodes
	}
	return nil
}
