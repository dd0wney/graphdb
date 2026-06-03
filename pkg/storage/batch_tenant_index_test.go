package storage

import "testing"

// TestBatchCommit_VisibleToForTenantReaders pins the Track Q / Q3 divergence
// surfaced by driving coi-screen against main: the batch/bulk-import path
// (BeginBatch -> AddNode/AddEdge -> Commit), which cmd/import-icij uses to load
// the entire ICIJ corpus, updated only the legacy GLOBAL indexes
// (nodesByLabel, outgoingEdges/incomingEdges) and never stamped TenantID nor
// populated the per-tenant indexes (tenantNodesByLabel, tenantNodeIDs) that the
// *ForTenant readers depend on. Result: every node/edge imported via the batch
// path was invisible to GetNodesByLabelForTenant / GetNodeForTenant /
// Get{Outgoing,Incoming}EdgesForTenant — the entire API surface coi-screen
// (and every current tenant-strict consumer) uses. import-icij + coi-screen
// were each tested in isolation but never together, so the whole bulk-import ->
// tenant-read path returned nothing.
//
// Batch import is tenant-blind, so created nodes/edges must land in the default
// tenant — matching CreateNode, which delegates to CreateNodeWithTenant(
// DefaultTenantID, ...). These assertions read with tenantID "" (-> default via
// effectiveTenantID), exactly as coi-screen does (cmd/coi/main.go passes "").
func TestBatchCommit_VisibleToForTenantReaders(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	b := gs.BeginBatch()
	acme, err := b.AddNode([]string{"Entity"}, map[string]Value{"name": StringValue("Acme Holdings Ltd")})
	if err != nil {
		t.Fatalf("AddNode(Entity): %v", err)
	}
	smith, err := b.AddNode([]string{"Officer"}, map[string]Value{"name": StringValue("Robert Smith")})
	if err != nil {
		t.Fatalf("AddNode(Officer): %v", err)
	}
	if _, err := b.AddEdge(smith, acme, "officer_of", nil, 1.0); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// 1. Label resolution — coi-screen's linkage.Resolve entry point.
	entities := gs.GetNodesByLabelForTenant("", "Entity")
	if len(entities) != 1 {
		t.Errorf("GetNodesByLabelForTenant(Entity) = %d nodes, want 1 — batch nodes not in tenant label index", len(entities))
	}
	officers := gs.GetNodesByLabelForTenant("", "Officer")
	if len(officers) != 1 {
		t.Errorf("GetNodesByLabelForTenant(Officer) = %d nodes, want 1", len(officers))
	}

	// 2. Direct node fetch — used while walking paths.
	if _, err := gs.GetNodeForTenant(acme, ""); err != nil {
		t.Errorf("GetNodeForTenant(acme) = %v, want ok — batch node not owned by default tenant", err)
	}

	// 3. Adjacency — coi-screen's FindInterestPaths hot path.
	outE, err := gs.GetOutgoingEdgesForTenant(smith, "")
	if err != nil {
		t.Fatalf("GetOutgoingEdgesForTenant: %v", err)
	}
	if len(outE) != 1 {
		t.Errorf("GetOutgoingEdgesForTenant(smith) = %d edges, want 1 — batch edge filtered out by TenantID mismatch", len(outE))
	}
	inE, err := gs.GetIncomingEdgesForTenant(acme, "")
	if err != nil {
		t.Fatalf("GetIncomingEdgesForTenant: %v", err)
	}
	if len(inE) != 1 {
		t.Errorf("GetIncomingEdgesForTenant(acme) = %d edges, want 1", len(inE))
	}
}

// TestBatchCommit_VisibleAfterReopen is the cross-process half of the Q3 fix.
// coi-screen never shares a process with import-icij: the importer batch-writes
// then Close()s (snapshot), and coi-screen Open()s the data dir and reads. The
// per-tenant indexes are NOT serialized — they are rebuilt on load from each
// node's/edge's TenantID (persistence.go addNodeToTenantIndex/addEdgeToTenant-
// Index). So this verifies the stamped TenantID survives the snapshot and the
// load rebuild re-buckets it correctly — the path the in-memory test above
// can't reach. Without the TenantID stamp, GetNodeForTenant and the edge filter
// fail post-reload even if the label index happens to rebuild.
func TestBatchCommit_VisibleAfterReopen(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	b := gs.BeginBatch()
	acme, _ := b.AddNode([]string{"Entity"}, map[string]Value{"name": StringValue("Acme Holdings Ltd")})
	smith, _ := b.AddNode([]string{"Officer"}, map[string]Value{"name": StringValue("Robert Smith")})
	if _, err := b.AddEdge(smith, acme, "officer_of", nil, 1.0); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := gs.Close(); err != nil { // snapshot to disk
		t.Fatalf("Close: %v", err)
	}

	// Fresh process equivalent: reopen the same data dir.
	gs2, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("reopen NewGraphStorage: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	if got := gs2.GetNodesByLabelForTenant("", "Entity"); len(got) != 1 {
		t.Errorf("after reopen: GetNodesByLabelForTenant(Entity) = %d, want 1", len(got))
	}
	if _, err := gs2.GetNodeForTenant(acme, ""); err != nil {
		t.Errorf("after reopen: GetNodeForTenant(acme) = %v, want ok", err)
	}
	outE, err := gs2.GetOutgoingEdgesForTenant(smith, "")
	if err != nil {
		t.Fatalf("after reopen: GetOutgoingEdgesForTenant: %v", err)
	}
	if len(outE) != 1 {
		t.Errorf("after reopen: GetOutgoingEdgesForTenant(smith) = %d, want 1", len(outE))
	}
}
