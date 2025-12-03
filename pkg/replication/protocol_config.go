package replication

import (
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/validation"
)

// SyncMode defines replication synchronization mode
type SyncMode uint8

const (
	// Async mode - primary doesn't wait for replica acknowledgment
	SyncModeAsync SyncMode = iota

	// Sync mode - primary waits for at least one replica acknowledgment
	SyncModeSync

	// Quorum mode - primary waits for majority of replicas
	SyncModeQuorum
)

// ReplicationConfig holds replication configuration
type ReplicationConfig struct {
	// Primary configuration
	IsPrimary         bool
	ListenAddr        string
	MaxReplicas       int
	HeartbeatInterval time.Duration
	WALStreamTimeout  time.Duration // Timeout for WAL stream buffer (prevents silent drops)

	// Replica configuration
	PrimaryAddr    string
	ReplicaID      string
	ReconnectDelay time.Duration

	// Connection pool limits
	ConnectTimeout   time.Duration // Timeout for establishing new connections
	ReadTimeout      time.Duration // Timeout for read operations (0 = no timeout)
	WriteTimeout     time.Duration // Timeout for write operations (0 = no timeout)
	MaxPendingConns  int           // Max connections waiting in accept queue
	SendBufferSize   int           // Size of per-replica send buffer
	IdleTimeout      time.Duration // Close connections idle longer than this (0 = no timeout)
	MaxConnectionAge time.Duration // Force reconnect after this duration (0 = never)
	HandshakeTimeout time.Duration // Timeout for initial handshake

	// Common
	SyncMode      SyncMode
	WALBufferSize int
}

// DefaultReplicationConfig returns default configuration
func DefaultReplicationConfig() ReplicationConfig {
	return ReplicationConfig{
		IsPrimary:         false,
		ListenAddr:        ":9090",
		MaxReplicas:       10,
		HeartbeatInterval: 1 * time.Second,
		WALStreamTimeout:  5 * time.Second, // Default 5s timeout for WAL buffer
		ReconnectDelay:    5 * time.Second,
		SyncMode:          SyncModeAsync,
		WALBufferSize:     1000,

		// Connection pool defaults
		ConnectTimeout:   10 * time.Second,  // 10s to establish connection
		ReadTimeout:      30 * time.Second,  // 30s read timeout
		WriteTimeout:     10 * time.Second,  // 10s write timeout
		MaxPendingConns:  100,               // Max 100 pending connections
		SendBufferSize:   100,               // 100 messages per replica
		IdleTimeout:      5 * time.Minute,   // Close after 5 min idle
		MaxConnectionAge: 24 * time.Hour,    // Force reconnect after 24 hours
		HandshakeTimeout: 30 * time.Second,  // 30s for handshake
	}
}

// Validate validates the replication configuration
func (c *ReplicationConfig) Validate() error {
	v := validation.NewConfigValidator("ReplicationConfig")

	// Primary-specific validation
	v.When(c.IsPrimary, func(cv *validation.ConfigValidator) {
		cv.Required("ListenAddr", c.ListenAddr).
			RangeInt("MaxReplicas", c.MaxReplicas, 1, 100).
			MinDuration("HeartbeatInterval", c.HeartbeatInterval, 100*time.Millisecond).
			MinDuration("WALStreamTimeout", c.WALStreamTimeout, 1*time.Second)
	})

	// Replica-specific validation
	v.When(!c.IsPrimary, func(cv *validation.ConfigValidator) {
		cv.Required("PrimaryAddr", c.PrimaryAddr).
			Required("ReplicaID", c.ReplicaID).
			MinDuration("ReconnectDelay", c.ReconnectDelay, 100*time.Millisecond)
	})

	// Common validation
	v.RangeInt("WALBufferSize", c.WALBufferSize, 10, 100000).
		MinDuration("ConnectTimeout", c.ConnectTimeout, 1*time.Second).
		MinDuration("HandshakeTimeout", c.HandshakeTimeout, 1*time.Second)

	return v.Validate()
}

// ApplyDefaults applies default values to zero-valued fields
func (c *ReplicationConfig) ApplyDefaults() {
	defaults := DefaultReplicationConfig()

	c.HeartbeatInterval = validation.DefaultOrDuration(c.HeartbeatInterval, defaults.HeartbeatInterval)
	c.WALStreamTimeout = validation.DefaultOrDuration(c.WALStreamTimeout, defaults.WALStreamTimeout)
	c.ReconnectDelay = validation.DefaultOrDuration(c.ReconnectDelay, defaults.ReconnectDelay)
	c.WALBufferSize = validation.DefaultOrInt(c.WALBufferSize, defaults.WALBufferSize)
	c.ConnectTimeout = validation.DefaultOrDuration(c.ConnectTimeout, defaults.ConnectTimeout)
	c.ReadTimeout = validation.DefaultOrDuration(c.ReadTimeout, defaults.ReadTimeout)
	c.WriteTimeout = validation.DefaultOrDuration(c.WriteTimeout, defaults.WriteTimeout)
	c.MaxPendingConns = validation.DefaultOrInt(c.MaxPendingConns, defaults.MaxPendingConns)
	c.SendBufferSize = validation.DefaultOrInt(c.SendBufferSize, defaults.SendBufferSize)
	c.IdleTimeout = validation.DefaultOrDuration(c.IdleTimeout, defaults.IdleTimeout)
	c.MaxConnectionAge = validation.DefaultOrDuration(c.MaxConnectionAge, defaults.MaxConnectionAge)
	c.HandshakeTimeout = validation.DefaultOrDuration(c.HandshakeTimeout, defaults.HandshakeTimeout)
	c.MaxReplicas = validation.DefaultOrInt(c.MaxReplicas, defaults.MaxReplicas)
}

// GetConnectTimeout returns the configured connect timeout, with a minimum of 1s
func (c *ReplicationConfig) GetConnectTimeout() time.Duration {
	if c.ConnectTimeout <= 0 {
		return 10 * time.Second // default
	}
	if c.ConnectTimeout < time.Second {
		return time.Second // minimum
	}
	return c.ConnectTimeout
}

// GetHandshakeTimeout returns the configured handshake timeout, with a minimum of 5s
func (c *ReplicationConfig) GetHandshakeTimeout() time.Duration {
	if c.HandshakeTimeout <= 0 {
		return 30 * time.Second // default
	}
	if c.HandshakeTimeout < 5*time.Second {
		return 5 * time.Second // minimum
	}
	return c.HandshakeTimeout
}

// HeartbeatTimeout returns the duration after which a replica is considered unresponsive
// Following ZeroMQ Paranoid Pirate pattern: 3x heartbeat interval
func (c *ReplicationConfig) HeartbeatTimeout() time.Duration {
	return c.HeartbeatInterval * 3
}

// ReplicaDeadThreshold returns the number of missed heartbeats before declaring replica dead
// Default: 5 missed heartbeats (5 seconds with 1s interval)
func (c *ReplicationConfig) ReplicaDeadThreshold() int {
	return 5
}

// GetSendBufferSize returns the configured send buffer size, with a minimum of 10
func (c *ReplicationConfig) GetSendBufferSize() int {
	return validation.ClampInt(
		validation.DefaultOrInt(c.SendBufferSize, 100),
		10, 10000,
	)
}
