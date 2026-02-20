package replication

import (
	"strings"
	"testing"
	"time"
)

// --- Validate Tests ---
// (DefaultReplicationConfig, HeartbeatTimeout, ReplicaDeadThreshold, SyncModes
// are already tested in protocol_test.go)

func TestValidate_ValidPrimaryConfig(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        ":9090",
		MaxReplicas:       5,
		HeartbeatInterval: 1 * time.Second,
		WALStreamTimeout:  5 * time.Second,
		WALBufferSize:     100,
		ConnectTimeout:    5 * time.Second,
		HandshakeTimeout:  10 * time.Second,
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid primary config should pass validation: %v", err)
	}
}

func TestValidate_ValidReplicaConfig(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:        false,
		PrimaryAddr:      "localhost:9090",
		ReplicaID:        "replica-1",
		ReconnectDelay:   1 * time.Second,
		WALBufferSize:    100,
		ConnectTimeout:   5 * time.Second,
		HandshakeTimeout: 10 * time.Second,
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid replica config should pass validation: %v", err)
	}
}

func TestValidate_PrimaryMissingListenAddr(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        "", // missing
		MaxReplicas:       5,
		HeartbeatInterval: 1 * time.Second,
		WALStreamTimeout:  5 * time.Second,
		WALBufferSize:     100,
		ConnectTimeout:    5 * time.Second,
		HandshakeTimeout:  10 * time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for missing ListenAddr")
	}
	if !strings.Contains(err.Error(), "ListenAddr") {
		t.Errorf("Error should mention ListenAddr: %v", err)
	}
}

func TestValidate_PrimaryInvalidMaxReplicas(t *testing.T) {
	tests := []struct {
		name        string
		maxReplicas int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too_high", 101},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ReplicationConfig{
				IsPrimary:         true,
				ListenAddr:        ":9090",
				MaxReplicas:       tt.maxReplicas,
				HeartbeatInterval: 1 * time.Second,
				WALStreamTimeout:  5 * time.Second,
				WALBufferSize:     100,
				ConnectTimeout:    5 * time.Second,
				HandshakeTimeout:  10 * time.Second,
			}

			err := cfg.Validate()
			if err == nil {
				t.Errorf("Expected error for MaxReplicas=%d", tt.maxReplicas)
			}
		})
	}
}

func TestValidate_PrimaryInvalidHeartbeatInterval(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        ":9090",
		MaxReplicas:       5,
		HeartbeatInterval: 50 * time.Millisecond, // below 100ms minimum
		WALStreamTimeout:  5 * time.Second,
		WALBufferSize:     100,
		ConnectTimeout:    5 * time.Second,
		HandshakeTimeout:  10 * time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for HeartbeatInterval below minimum")
	}
}

func TestValidate_PrimaryInvalidWALStreamTimeout(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        ":9090",
		MaxReplicas:       5,
		HeartbeatInterval: 1 * time.Second,
		WALStreamTimeout:  500 * time.Millisecond, // below 1s minimum
		WALBufferSize:     100,
		ConnectTimeout:    5 * time.Second,
		HandshakeTimeout:  10 * time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for WALStreamTimeout below minimum")
	}
}

func TestValidate_ReplicaMissingPrimaryAddr(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:        false,
		PrimaryAddr:      "", // missing
		ReplicaID:        "replica-1",
		ReconnectDelay:   1 * time.Second,
		WALBufferSize:    100,
		ConnectTimeout:   5 * time.Second,
		HandshakeTimeout: 10 * time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for missing PrimaryAddr")
	}
	if !strings.Contains(err.Error(), "PrimaryAddr") {
		t.Errorf("Error should mention PrimaryAddr: %v", err)
	}
}

func TestValidate_ReplicaMissingReplicaID(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:        false,
		PrimaryAddr:      "localhost:9090",
		ReplicaID:        "", // missing
		ReconnectDelay:   1 * time.Second,
		WALBufferSize:    100,
		ConnectTimeout:   5 * time.Second,
		HandshakeTimeout: 10 * time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for missing ReplicaID")
	}
	if !strings.Contains(err.Error(), "ReplicaID") {
		t.Errorf("Error should mention ReplicaID: %v", err)
	}
}

func TestValidate_ReplicaInvalidReconnectDelay(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:        false,
		PrimaryAddr:      "localhost:9090",
		ReplicaID:        "replica-1",
		ReconnectDelay:   50 * time.Millisecond, // below 100ms minimum
		WALBufferSize:    100,
		ConnectTimeout:   5 * time.Second,
		HandshakeTimeout: 10 * time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for ReconnectDelay below minimum")
	}
}

func TestValidate_InvalidWALBufferSize(t *testing.T) {
	tests := []struct {
		name      string
		bufferSz  int
		isPrimary bool
	}{
		{"too_small_primary", 5, true},
		{"too_small_replica", 5, false},
		{"too_large_primary", 100001, true},
		{"too_large_replica", 100001, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ReplicationConfig{
				IsPrimary:         tt.isPrimary,
				ListenAddr:        ":9090",
				PrimaryAddr:       "localhost:9090",
				ReplicaID:         "replica-1",
				MaxReplicas:       5,
				HeartbeatInterval: 1 * time.Second,
				WALStreamTimeout:  5 * time.Second,
				ReconnectDelay:    1 * time.Second,
				WALBufferSize:     tt.bufferSz,
				ConnectTimeout:    5 * time.Second,
				HandshakeTimeout:  10 * time.Second,
			}

			err := cfg.Validate()
			if err == nil {
				t.Errorf("Expected error for WALBufferSize=%d", tt.bufferSz)
			}
		})
	}
}

func TestValidate_InvalidConnectTimeout(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        ":9090",
		MaxReplicas:       5,
		HeartbeatInterval: 1 * time.Second,
		WALStreamTimeout:  5 * time.Second,
		WALBufferSize:     100,
		ConnectTimeout:    500 * time.Millisecond, // below 1s minimum
		HandshakeTimeout:  10 * time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for ConnectTimeout below minimum")
	}
}

func TestValidate_InvalidHandshakeTimeout(t *testing.T) {
	cfg := ReplicationConfig{
		IsPrimary:         true,
		ListenAddr:        ":9090",
		MaxReplicas:       5,
		HeartbeatInterval: 1 * time.Second,
		WALStreamTimeout:  5 * time.Second,
		WALBufferSize:     100,
		ConnectTimeout:    5 * time.Second,
		HandshakeTimeout:  500 * time.Millisecond, // below 1s minimum
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for HandshakeTimeout below minimum")
	}
}

// --- ApplyDefaults Tests ---

func TestApplyDefaults_ZeroValueConfig(t *testing.T) {
	cfg := ReplicationConfig{}
	cfg.ApplyDefaults()

	defaults := DefaultReplicationConfig()

	tests := []struct {
		name     string
		got      any
		expected any
	}{
		{"HeartbeatInterval", cfg.HeartbeatInterval, defaults.HeartbeatInterval},
		{"WALStreamTimeout", cfg.WALStreamTimeout, defaults.WALStreamTimeout},
		{"ReconnectDelay", cfg.ReconnectDelay, defaults.ReconnectDelay},
		{"WALBufferSize", cfg.WALBufferSize, defaults.WALBufferSize},
		{"ConnectTimeout", cfg.ConnectTimeout, defaults.ConnectTimeout},
		{"ReadTimeout", cfg.ReadTimeout, defaults.ReadTimeout},
		{"WriteTimeout", cfg.WriteTimeout, defaults.WriteTimeout},
		{"MaxPendingConns", cfg.MaxPendingConns, defaults.MaxPendingConns},
		{"SendBufferSize", cfg.SendBufferSize, defaults.SendBufferSize},
		{"IdleTimeout", cfg.IdleTimeout, defaults.IdleTimeout},
		{"MaxConnectionAge", cfg.MaxConnectionAge, defaults.MaxConnectionAge},
		{"HandshakeTimeout", cfg.HandshakeTimeout, defaults.HandshakeTimeout},
		{"MaxReplicas", cfg.MaxReplicas, defaults.MaxReplicas},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("ApplyDefaults().%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestApplyDefaults_PreservesExistingValues(t *testing.T) {
	cfg := ReplicationConfig{
		HeartbeatInterval: 2 * time.Second,
		WALStreamTimeout:  10 * time.Second,
		ReconnectDelay:    3 * time.Second,
		WALBufferSize:     500,
		ConnectTimeout:    15 * time.Second,
	}
	cfg.ApplyDefaults()

	// These values should be preserved
	if cfg.HeartbeatInterval != 2*time.Second {
		t.Errorf("HeartbeatInterval should be preserved: got %v", cfg.HeartbeatInterval)
	}
	if cfg.WALStreamTimeout != 10*time.Second {
		t.Errorf("WALStreamTimeout should be preserved: got %v", cfg.WALStreamTimeout)
	}
	if cfg.ReconnectDelay != 3*time.Second {
		t.Errorf("ReconnectDelay should be preserved: got %v", cfg.ReconnectDelay)
	}
	if cfg.WALBufferSize != 500 {
		t.Errorf("WALBufferSize should be preserved: got %v", cfg.WALBufferSize)
	}
	if cfg.ConnectTimeout != 15*time.Second {
		t.Errorf("ConnectTimeout should be preserved: got %v", cfg.ConnectTimeout)
	}
}

// --- GetConnectTimeout Tests ---

func TestGetConnectTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		expected time.Duration
	}{
		{"zero_returns_default", 0, 10 * time.Second},
		{"negative_returns_default", -1 * time.Second, 10 * time.Second},
		{"below_minimum_returns_minimum", 500 * time.Millisecond, time.Second},
		{"at_minimum_returns_value", time.Second, time.Second},
		{"above_minimum_returns_value", 5 * time.Second, 5 * time.Second},
		{"large_value_returns_value", 60 * time.Second, 60 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ReplicationConfig{ConnectTimeout: tt.timeout}
			got := cfg.GetConnectTimeout()
			if got != tt.expected {
				t.Errorf("GetConnectTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// --- GetHandshakeTimeout Tests ---

func TestGetHandshakeTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		expected time.Duration
	}{
		{"zero_returns_default", 0, 30 * time.Second},
		{"negative_returns_default", -1 * time.Second, 30 * time.Second},
		{"below_minimum_returns_minimum", 2 * time.Second, 5 * time.Second},
		{"at_minimum_returns_value", 5 * time.Second, 5 * time.Second},
		{"above_minimum_returns_value", 15 * time.Second, 15 * time.Second},
		{"large_value_returns_value", 120 * time.Second, 120 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ReplicationConfig{HandshakeTimeout: tt.timeout}
			got := cfg.GetHandshakeTimeout()
			if got != tt.expected {
				t.Errorf("GetHandshakeTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// --- GetSendBufferSize Tests ---

func TestGetSendBufferSize(t *testing.T) {
	tests := []struct {
		name       string
		bufferSize int
		expected   int
	}{
		{"zero_returns_default_clamped", 0, 100},  // default is 100
		{"below_minimum_returns_minimum", 5, 10},  // min is 10
		{"at_minimum_returns_value", 10, 10},      // min is 10
		{"normal_value_returns_value", 50, 50},
		{"default_value_returns_value", 100, 100},
		{"above_max_returns_max", 20000, 10000},   // max is 10000
		{"at_max_returns_value", 10000, 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ReplicationConfig{SendBufferSize: tt.bufferSize}
			got := cfg.GetSendBufferSize()
			if got != tt.expected {
				t.Errorf("GetSendBufferSize() = %d, want %d", got, tt.expected)
			}
		})
	}
}
