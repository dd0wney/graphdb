package storage

import (
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// txCfg is a batched-WAL config (a sane FlushInterval avoids the NewTicker(0)
// panic) used by the transaction durability tests.
func txCfg(dir string) StorageConfig {
	return StorageConfig{DataDir: dir, EnableBatching: true, BatchSize: 100, FlushInterval: 10 * time.Millisecond}
}

// TestTransactionCommit_IndexConsistency pins the gap this feature closes:
// nodes/edges created in a committed transaction must be visible to the
// per-tenant indexed reads, counted in stats, and (vector property) searchable
// — not just present in the raw shard map. Before the rewrite, Commit bypassed
// all of these.
func TestTransactionCommit_IndexConsistency(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	tx, err := gs.BeginTransactionForTenant("acme")
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	a, err := tx.CreateNode([]string{"Doc"}, map[string]Value{"k": StringValue("v")})
	if err != nil {
		t.Fatalf("tx.CreateNode: %v", err)
	}
	b, err := tx.CreateNode([]string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("tx.CreateNode: %v", err)
	}
	if _, err := tx.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0); err != nil {
		t.Fatalf("tx.CreateEdge: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Tenant-scoped indexed reads must see the committed nodes/edges.
	if got := gs.GetNodesByLabelForTenant("acme", "Doc"); len(got) != 2 {
		t.Errorf("GetNodesByLabelForTenant=%d, want 2 (commit bypassed tenant label index)", len(got))
	}
	if got := gs.GetAllNodesForTenant("acme"); len(got) != 2 {
		t.Errorf("GetAllNodesForTenant=%d, want 2 (commit bypassed tenant enumeration index)", len(got))
	}
	if got := gs.CountNodesForTenant("acme"); got != 2 {
		t.Errorf("CountNodesForTenant=%d, want 2 (commit bypassed tenant stats)", got)
	}
	if got := gs.GetEdgesByTypeForTenant("acme", "LINKS"); len(got) != 1 {
		t.Errorf("GetEdgesByTypeForTenant=%d, want 1 (commit bypassed tenant edge index)", len(got))
	}
	if got := gs.GetAllEdgesForTenant("acme"); len(got) != 1 {
		t.Errorf("GetAllEdgesForTenant=%d, want 1", len(got))
	}
}

// TestTransactionCommit_VectorSearchable confirms a vector property on a
// transaction-created node lands in the HNSW index (commit applies vector plans
// off-lock).
func TestTransactionCommit_VectorSearchable(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreateVectorIndexForTenant("acme", "embedding", 4, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}

	tx, _ := gs.BeginTransactionForTenant("acme")
	vec := []float32{1, 0, 0, 0}
	node, err := tx.CreateNode([]string{"Doc"}, map[string]Value{"embedding": VectorValue(vec)})
	if err != nil {
		t.Fatalf("tx.CreateNode: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	results, err := gs.VectorSearchForTenant("acme", "embedding", vec, 1, 10)
	if err != nil {
		t.Fatalf("VectorSearchForTenant: %v", err)
	}
	if len(results) != 1 || results[0].ID != node.ID {
		t.Fatalf("committed vector not searchable: got %+v, want node %d", results, node.ID)
	}
}

// TestTransactionCommit_DurableAcrossCrash pins atomic durability: a committed
// transaction survives a crash (reopen replays the WAL). Uses the crash-sim
// pattern (DON'T Close — that snapshots in-memory + truncates; here we want the
// WAL to drive recovery).
func TestTransactionCommit_DurableAcrossCrash(t *testing.T) {
	dataDir := t.TempDir()
	cfg := txCfg(dataDir)

	var aID, bID uint64
	{
		gs := testCrashableStorage(t, dataDir, cfg)
		tx, _ := gs.BeginTransactionForTenant("acme")
		a, _ := tx.CreateNode([]string{"Doc"}, map[string]Value{"k": StringValue("v")})
		b, _ := tx.CreateNode([]string{"Doc"}, nil)
		if _, err := tx.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0); err != nil {
			t.Fatalf("tx.CreateEdge: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		aID, bID = a.ID, b.ID
		// DON'T Close — crash sim. The committed batch is in the WAL.
	}

	gs2, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	if _, err := gs2.GetNode(aID); err != nil {
		t.Errorf("node %d not durable after crash: %v", aID, err)
	}
	if _, err := gs2.GetNode(bID); err != nil {
		t.Errorf("node %d not durable after crash: %v", bID, err)
	}
	// And the tenant index is rebuilt on replay (the committed nodes are
	// label-queryable after restart).
	if got := gs2.GetNodesByLabelForTenant("acme", "Doc"); len(got) != 2 {
		t.Errorf("after crash GetNodesByLabelForTenant=%d, want 2", len(got))
	}
}

// TestTransactionCommit_UpdateExistingNode confirms an update buffered in a
// transaction applies to a pre-existing node on commit.
func TestTransactionCommit_UpdateExistingNode(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	existing, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]Value{"k": StringValue("old")})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	tx, _ := gs.BeginTransactionForTenant("acme")
	if err := tx.UpdateNode(existing.ID, map[string]Value{"k": StringValue("new")}); err != nil {
		t.Fatalf("tx.UpdateNode: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := gs.GetNodeForTenant(existing.ID, "acme")
	if err != nil {
		t.Fatalf("GetNodeForTenant: %v", err)
	}
	if v, _ := got.Properties["k"].AsString(); v != "new" {
		t.Errorf("property k=%q, want \"new\" (transaction update not applied)", v)
	}
}

// TestTransactionCommit_CrossTenantEdgeRejected pins the all-or-none reference
// validation: an edge to a node owned by a different tenant aborts the commit
// with nothing applied.
func TestTransactionCommit_CrossTenantEdgeRejected(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// A node owned by a different tenant.
	other, err := gs.CreateNodeWithTenant("other", nil, nil)
	if err != nil {
		t.Fatalf("seed other: %v", err)
	}

	tx, _ := gs.BeginTransactionForTenant("acme")
	mine, _ := tx.CreateNode([]string{"Doc"}, nil)
	// Edge from my node to a foreign-tenant node — must be rejected.
	if _, err := tx.CreateEdge(mine.ID, other.ID, "LINKS", nil, 1.0); err != nil {
		t.Fatalf("tx.CreateEdge (buffering): %v", err)
	}
	if err := tx.Commit(); err == nil {
		t.Fatalf("expected commit to reject cross-tenant edge, got nil")
	}
	// All-or-none: my node must NOT have been committed.
	if got := gs.CountNodesForTenant("acme"); got != 0 {
		t.Errorf("after rejected commit, acme node count=%d, want 0 (not all-or-none)", got)
	}
}
