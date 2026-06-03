package storage

import "testing"

// TestEdgeAdjacencySurvivesReopen pins the cross-cutting persistence bug
// surfaced by driving coi-screen (Track Q / Q3) and confirmed independently by
// Stór: the snapshot serializes only the plain outgoingEdges/incomingEdges
// maps, but with edge compression on (the NewGraphStorage default) live
// adjacency lives in the compressed representation, which is not serialized. A
// naive restore therefore loses ALL adjacency on reopen — edges load (GetEdge
// works) but GetOutgoingEdges/Incoming return nothing, silently breaking every
// traversal after a restart. Uses the DEFAULT config (compression on) and the
// NORMAL create path, so it guards the bug for every consumer, not just bulk
// import.
func TestEdgeAdjacencySurvivesReopen(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorage(dir) // default config: EnableEdgeCompression=true
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	a, _ := gs.CreateNode([]string{"Officer"}, map[string]Value{"name": StringValue("A")})
	b, _ := gs.CreateNode([]string{"Entity"}, map[string]Value{"name": StringValue("B")})
	if _, err := gs.CreateEdge(a.ID, b.ID, "officer_of", nil, 1.0); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	gs2, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	out, err := gs2.GetOutgoingEdges(a.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("GetOutgoingEdges(a) = %d after reopen, want 1 — adjacency lost across restart", len(out))
	}
	in, err := gs2.GetIncomingEdges(b.ID)
	if err != nil {
		t.Fatalf("GetIncomingEdges: %v", err)
	}
	if len(in) != 1 {
		t.Fatalf("GetIncomingEdges(b) = %d after reopen, want 1", len(in))
	}
}
