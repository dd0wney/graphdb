package cluster

import (
	"testing"
	"time"
)

func TestDefaultClusterConfig(t *testing.T) {
	cfg := DefaultClusterConfig()

	if cfg.ElectionTimeout != 5*time.Second {
		t.Errorf("ElectionTimeout = %v, want 5s", cfg.ElectionTimeout)
	}
	if cfg.HeartbeatInterval != 1*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 1s", cfg.HeartbeatInterval)
	}
	if cfg.MinQuorumSize != 2 {
		t.Errorf("MinQuorumSize = %d, want 2", cfg.MinQuorumSize)
	}
	if cfg.Priority != 1 {
		t.Errorf("Priority = %d, want 1", cfg.Priority)
	}
	if cfg.EnableAutoFailover {
		t.Error("EnableAutoFailover should be false by default")
	}
	if cfg.EnableQuorumWrites {
		t.Error("EnableQuorumWrites should be false by default")
	}
	if cfg.VoteRequestTimeout != 2*time.Second {
		t.Errorf("VoteRequestTimeout = %v, want 2s", cfg.VoteRequestTimeout)
	}
	if cfg.MaxElectionRetries != 3 {
		t.Errorf("MaxElectionRetries = %d, want 3", cfg.MaxElectionRetries)
	}
}

func TestClusterConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      ClusterConfig
		expectedErr error
	}{
		{
			name: "valid config",
			config: ClusterConfig{
				NodeID:            "node-1",
				NodeAddr:          "localhost:9090",
				ElectionTimeout:   5 * time.Second,
				HeartbeatInterval: 1 * time.Second,
				MinQuorumSize:     2,
				SeedNodes:         []string{"localhost:9091"},
			},
			expectedErr: nil,
		},
		{
			name: "valid config without seed nodes and auto-failover disabled",
			config: ClusterConfig{
				NodeID:             "node-1",
				NodeAddr:           "localhost:9090",
				ElectionTimeout:    5 * time.Second,
				HeartbeatInterval:  1 * time.Second,
				MinQuorumSize:      2,
				EnableAutoFailover: false,
			},
			expectedErr: nil,
		},
		{
			name: "empty node ID",
			config: ClusterConfig{
				NodeID:            "",
				NodeAddr:          "localhost:9090",
				ElectionTimeout:   5 * time.Second,
				HeartbeatInterval: 1 * time.Second,
				MinQuorumSize:     2,
			},
			expectedErr: ErrInvalidNodeID,
		},
		{
			name: "empty node address",
			config: ClusterConfig{
				NodeID:            "node-1",
				NodeAddr:          "",
				ElectionTimeout:   5 * time.Second,
				HeartbeatInterval: 1 * time.Second,
				MinQuorumSize:     2,
			},
			expectedErr: ErrInvalidNodeAddr,
		},
		{
			name: "election timeout smaller than heartbeat",
			config: ClusterConfig{
				NodeID:            "node-1",
				NodeAddr:          "localhost:9090",
				ElectionTimeout:   500 * time.Millisecond,
				HeartbeatInterval: 1 * time.Second,
				MinQuorumSize:     2,
			},
			expectedErr: ErrElectionTimeoutTooSmall,
		},
		{
			name: "invalid quorum size zero",
			config: ClusterConfig{
				NodeID:            "node-1",
				NodeAddr:          "localhost:9090",
				ElectionTimeout:   5 * time.Second,
				HeartbeatInterval: 1 * time.Second,
				MinQuorumSize:     0,
			},
			expectedErr: ErrInvalidQuorumSize,
		},
		{
			name: "invalid quorum size negative",
			config: ClusterConfig{
				NodeID:            "node-1",
				NodeAddr:          "localhost:9090",
				ElectionTimeout:   5 * time.Second,
				HeartbeatInterval: 1 * time.Second,
				MinQuorumSize:     -1,
			},
			expectedErr: ErrInvalidQuorumSize,
		},
		{
			name: "auto-failover enabled without seed nodes",
			config: ClusterConfig{
				NodeID:             "node-1",
				NodeAddr:           "localhost:9090",
				ElectionTimeout:    5 * time.Second,
				HeartbeatInterval:  1 * time.Second,
				MinQuorumSize:      2,
				EnableAutoFailover: true,
				SeedNodes:          []string{}, // empty
			},
			expectedErr: ErrNoSeedNodes,
		},
		{
			name: "auto-failover enabled with seed nodes",
			config: ClusterConfig{
				NodeID:             "node-1",
				NodeAddr:           "localhost:9090",
				ElectionTimeout:    5 * time.Second,
				HeartbeatInterval:  1 * time.Second,
				MinQuorumSize:      2,
				EnableAutoFailover: true,
				SeedNodes:          []string{"localhost:9091", "localhost:9092"},
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err != tt.expectedErr {
				t.Errorf("Validate() = %v, want %v", err, tt.expectedErr)
			}
		})
	}
}
