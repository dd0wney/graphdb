package storage

import (
	"reflect"
	"testing"
)

// TestMmapSnapshotCodec_RoundTrip verifies the in-memory record codec (no file/mmap),
// so it runs everywhere in CI and pins the format's correctness independent of the
// platform-specific mmap reader.
func TestMmapSnapshotCodec_RoundTrip(t *testing.T) {
	node := &Node{
		ID:       7,
		TenantID: "acme",
		Labels:   []string{"Entity", "Person"},
		Properties: map[string]Value{
			"name": StringValue("alice"),
			"age":  IntValue(42),
			"lat":  IntValue(1 << 19),
		},
		CreatedAt: 1000,
		UpdatedAt: 2000,
	}
	gotNode := decodeNodeRecordAt(encodeNodeRecord(node), 0)
	if gotNode.ID != node.ID || gotNode.TenantID != node.TenantID ||
		!reflect.DeepEqual(gotNode.Labels, node.Labels) ||
		!reflect.DeepEqual(gotNode.Properties, node.Properties) ||
		gotNode.CreatedAt != node.CreatedAt || gotNode.UpdatedAt != node.UpdatedAt {
		t.Fatalf("node round-trip mismatch:\n got %+v\nwant %+v", gotNode, node)
	}

	edge := &Edge{
		ID:         9,
		TenantID:   "acme",
		FromNodeID: 7,
		ToNodeID:   8,
		Type:       "KNOWS",
		Properties: map[string]Value{"since": IntValue(2021)},
		Weight:     2.5, // v2 records carry Weight (the prototype dropped it)
		CreatedAt:  3000,
	}
	gotEdge := decodeEdgeRecordAt(encodeEdgeRecord(edge), 0)
	if gotEdge.ID != edge.ID || gotEdge.TenantID != edge.TenantID ||
		gotEdge.FromNodeID != edge.FromNodeID || gotEdge.ToNodeID != edge.ToNodeID ||
		gotEdge.Type != edge.Type || gotEdge.Weight != edge.Weight ||
		!reflect.DeepEqual(gotEdge.Properties, edge.Properties) ||
		gotEdge.CreatedAt != edge.CreatedAt {
		t.Fatalf("edge round-trip mismatch:\n got %+v\nwant %+v", gotEdge, edge)
	}

	// Empty property bag and no labels must round-trip too.
	bare := &Node{ID: 1, TenantID: "", Labels: nil, Properties: map[string]Value{}}
	gb := decodeNodeRecordAt(encodeNodeRecord(bare), 0)
	if gb.ID != 1 || len(gb.Labels) != 0 || len(gb.Properties) != 0 {
		t.Fatalf("bare node round-trip mismatch: %+v", gb)
	}
}
