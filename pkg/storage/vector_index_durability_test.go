package storage

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// These tests pin vector-index DEFINITION durability across a crash. The HNSW
// graph is rebuilt from node properties on load (rebuildVectorIndexesFromNodes),
// so what must survive a crash is the index *definition* — its construction
// parameters. Definitions are captured in the snapshot, but an index created (or
// dropped) AFTER the last snapshot lives only in memory unless the create/drop
// is WAL-logged. CreatePropertyIndex is WAL-logged; CreateVectorIndex was not,
// so a post-snapshot vector index was silently lost on crash (its vectors
// un-indexed, search returning ErrNodeNotFound after recovery).
//
// Distinct from TestVectorIndex_SurvivesCrashRecovery_PostSnapshotWrite: there
// the definition was created in session 1 and captured by the clean-close
// snapshot — only post-snapshot NODES were WAL-only. Here the DEFINITION itself
// is post-snapshot.

// TestVectorIndexDurability_CreateIndexAfterSnapshot_CrashRecovery is the teeth
// case for WAL-logging CreateVectorIndex: a vector index created after the last
// snapshot must survive a crash so its vectors stay searchable.
func TestVectorIndexDurability_CreateIndexAfterSnapshot_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	const tenant = "acme"

	// Session 1: a node WITHOUT any vector index, clean close. The snapshot
	// captures the node but no vector-index definition.
	{
		gs, err := NewGraphStorageWithConfig(crashRecoveryConfig(dir))
		if err != nil {
			t.Fatalf("session1 open: %v", err)
		}
		if _, err := gs.CreateNodeWithTenant(tenant, []string{"Doc"}, map[string]Value{
			"title": StringValue("pre-index node"),
		}); err != nil {
			t.Fatalf("session1 create node: %v", err)
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("session1 close: %v", err)
		}
	}

	// Session 2: create the vector index (POST-snapshot — WAL-only) and a node
	// carrying a vector, then crash (no Close). The default WAL fsyncs per
	// write, so both are durable in the WAL.
	var vid uint64
	{
		gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
		if err := gs.CreateVectorIndexForTenant(tenant, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("session2 create vector index: %v", err)
		}
		n, err := gs.CreateNodeWithTenant(tenant, []string{"Doc"}, map[string]Value{
			"embedding": VectorValue([]float32{1, 0, 0}),
		})
		if err != nil {
			t.Fatalf("session2 create vector node: %v", err)
		}
		vid = n.ID
		// no Close — simulate crash.
	}

	// Session 3: recovery. The index definition must be restored from the WAL
	// (it was never snapshotted), then rebuildVectorIndexesFromNodes populates
	// the HNSW graph from the recovered node set.
	gs, err := NewGraphStorageWithConfig(crashRecoveryConfig(dir))
	if err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if !gs.HasVectorIndexForTenant(tenant, "embedding") {
		t.Fatalf("vector index 'embedding' LOST after crash — CreateVectorIndex not WAL-logged")
	}
	res, err := gs.VectorSearchForTenant(tenant, "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("search after recovery: %v — index definition not recovered", err)
	}
	if len(res) != 1 || res[0].ID != vid {
		t.Errorf("search after recovery = %v, want exactly node %d", res, vid)
	}
}

// TestVectorIndexDurability_DropIndexAfterSnapshot_CrashRecovery is the teeth
// case for WAL-logging DropVectorIndex: a drop applied after the last snapshot
// must survive a crash, or the snapshotted definition resurrects on recovery.
func TestVectorIndexDurability_DropIndexAfterSnapshot_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	const tenant = "acme"

	// Session 1: create the index + a vector node, clean close. The snapshot
	// now contains the index definition.
	{
		gs, err := NewGraphStorageWithConfig(crashRecoveryConfig(dir))
		if err != nil {
			t.Fatalf("session1 open: %v", err)
		}
		if err := gs.CreateVectorIndexForTenant(tenant, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("session1 create vector index: %v", err)
		}
		if _, err := gs.CreateNodeWithTenant(tenant, []string{"Doc"}, map[string]Value{
			"embedding": VectorValue([]float32{1, 0, 0}),
		}); err != nil {
			t.Fatalf("session1 create vector node: %v", err)
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("session1 close: %v", err)
		}
	}

	// Session 2: drop the index (POST-snapshot — WAL-only), then crash.
	{
		gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
		if err := gs.DropVectorIndexForTenant(tenant, "embedding"); err != nil {
			t.Fatalf("session2 drop vector index: %v", err)
		}
		// no Close — simulate crash.
	}

	// Session 3: recovery. The drop must be replayed, or the snapshotted
	// definition is restored and the index resurrects.
	gs, err := NewGraphStorageWithConfig(crashRecoveryConfig(dir))
	if err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if gs.HasVectorIndexForTenant(tenant, "embedding") {
		t.Errorf("vector index 'embedding' RESURRECTED after crash — DropVectorIndex not WAL-logged")
	}
	if _, err := gs.VectorSearchForTenant(tenant, "embedding", []float32{1, 0, 0}, 1, 50); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("search on dropped index after recovery: err=%v, want ErrNodeNotFound", err)
	}
}
