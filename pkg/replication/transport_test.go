package replication

import (
	"encoding/json"
	"strings"
	"testing"
)

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
	// Asserting Properties is what the field write was for; the previous
	// version set it but never read — govet's unusedwrite flagged that.
	if nodeOp.Properties["name"] != "Alice" {
		t.Errorf("Properties[name] = %v, want %q", nodeOp.Properties["name"], "Alice")
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

// TestWriteOperation_TenantID_RoundTrip pins the wire-format contract
// added in audit A8 (2026-05-09): every WriteOperation MUST carry a
// TenantID across JSON round-trip. Two halves:
//
//  1. A populated TenantID survives marshal → unmarshal byte-for-byte.
//     Catches a future bug where someone renames the field, drops the
//     JSON tag, or shadows it with a different field that happens to
//     compile.
//  2. The wire encoding uses the snake_case key `tenant_id` (matching
//     the convention of every other field in this struct), and emits
//     the field even when empty (no `omitempty`). Empty-on-the-wire
//     is the diagnostic signal the apply path's fail-closed check
//     needs — see the type's doc comment in transport.go.
func TestWriteOperation_TenantID_RoundTrip(t *testing.T) {
	t.Run("populated TenantID survives JSON round-trip", func(t *testing.T) {
		original := WriteOperation{
			TenantID:   "tenant-A",
			Type:       "create_node",
			Labels:     []string{"Person"},
			Properties: map[string]interface{}{"name": "Alice"},
		}
		bytes, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded WriteOperation
		if err := json.Unmarshal(bytes, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.TenantID != "tenant-A" {
			t.Errorf("TenantID = %q, want %q (wire payload: %s)", decoded.TenantID, "tenant-A", bytes)
		}
	})

	t.Run("wire key is tenant_id and emitted even when empty", func(t *testing.T) {
		// An empty TenantID still appears on the wire so the apply
		// path can distinguish "explicit empty" from "missing field
		// from a buggy/old sender" — both are refused, but the diagnostic
		// log differs. omitempty would make these indistinguishable.
		bytes, err := json.Marshal(WriteOperation{Type: "create_node"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		wire := string(bytes)
		if !strings.Contains(wire, `"tenant_id":""`) {
			t.Errorf("expected wire to contain `\"tenant_id\":\"\"`, got: %s", wire)
		}
	})
}
