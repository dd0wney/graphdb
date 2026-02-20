package replication

import "testing"

func TestDefaultTransportConfig(t *testing.T) {
	cfg := DefaultTransportConfig()

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"WALPublishAddr", cfg.WALPublishAddr, "tcp://*:9090"},
		{"HealthSurveyAddr", cfg.HealthSurveyAddr, "tcp://*:9091"},
		{"WriteBufferAddr", cfg.WriteBufferAddr, "tcp://*:9092"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("DefaultTransportConfig().%s = %q, want %q", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestStorageStats(t *testing.T) {
	stats := StorageStats{
		NodeCount: 100,
		EdgeCount: 250,
	}

	if stats.NodeCount != 100 {
		t.Errorf("NodeCount = %d, want 100", stats.NodeCount)
	}
	if stats.EdgeCount != 250 {
		t.Errorf("EdgeCount = %d, want 250", stats.EdgeCount)
	}
}

func TestWriteOperation(t *testing.T) {
	// Test create node operation
	nodeOp := WriteOperation{
		Type:       "create_node",
		Labels:     []string{"Person", "Employee"},
		Properties: map[string]interface{}{"name": "Alice"},
	}

	if nodeOp.Type != "create_node" {
		t.Errorf("Type = %q, want %q", nodeOp.Type, "create_node")
	}
	if len(nodeOp.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(nodeOp.Labels))
	}

	// Test create edge operation
	edgeOp := WriteOperation{
		Type:       "create_edge",
		FromNodeID: 1,
		ToNodeID:   2,
		EdgeType:   "KNOWS",
		Weight:     0.5,
	}

	if edgeOp.Type != "create_edge" {
		t.Errorf("Type = %q, want %q", edgeOp.Type, "create_edge")
	}
	if edgeOp.FromNodeID != 1 {
		t.Errorf("FromNodeID = %d, want 1", edgeOp.FromNodeID)
	}
	if edgeOp.ToNodeID != 2 {
		t.Errorf("ToNodeID = %d, want 2", edgeOp.ToNodeID)
	}
	if edgeOp.EdgeType != "KNOWS" {
		t.Errorf("EdgeType = %q, want %q", edgeOp.EdgeType, "KNOWS")
	}
	if edgeOp.Weight != 0.5 {
		t.Errorf("Weight = %f, want 0.5", edgeOp.Weight)
	}
}
