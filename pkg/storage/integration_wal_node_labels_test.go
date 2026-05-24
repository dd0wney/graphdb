package storage

import (
	"os"
	"sort"
	"testing"
)

// TestWAL_AddNodeLabels_SurvivesRestart pins the durability contract
// for OpAddNodeLabels: a label added at runtime must come back after
// a crash-style restart (no clean snapshot, only WAL replay). This is
// the highest-leverage test for the new op — without it the HTTP
// endpoint could appear to work in-memory and silently lose mutations
// on restart, breaking the Ulysses backfill use case the surface
// exists for.
func TestWAL_AddNodeLabels_SurvivesRestart(t *testing.T) {
	dataDir := t.TempDir()
	defer func() { _ = os.RemoveAll(dataDir) }()

	var nodeID uint64

	// Phase 1: create node, add label, do NOT close cleanly so the
	// snapshot isn't written — recovery must come from WAL alone.
	// testCrashableStorage registers t.Cleanup so the goroutines tied
	// to this gs instance shut down on test exit even though we never
	// call Close() inline (which would also snapshot and defeat the
	// crash simulation).
	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		node, err := gs.CreateNodeWithTenant("tenant-A",
			[]string{"TextEmbedding"},
			map[string]Value{"text": StringValue("hello")})
		if err != nil {
			t.Fatalf("Phase 1 CreateNodeWithTenant: %v", err)
		}
		nodeID = node.ID

		added, err := gs.AddNodeLabelsForTenant(nodeID, "tenant-A",
			[]string{"CharacterEmbedding"})
		if err != nil {
			t.Fatalf("Phase 1 AddNodeLabelsForTenant: %v", err)
		}
		if len(added) != 1 || added[0] != "CharacterEmbedding" {
			t.Fatalf("Phase 1 added=%v, want [CharacterEmbedding]", added)
		}
		// Do NOT call Close — simulating a crash before clean shutdown.
	}

	// Phase 2: recover from WAL replay.
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Phase 2 NewGraphStorageWithConfig: %v", err)
		}
		defer func() { _ = gs.Close() }()

		node, err := gs.GetNodeForTenant(nodeID, "tenant-A")
		if err != nil {
			t.Fatalf("Phase 2 GetNodeForTenant: %v", err)
		}

		// Both labels must be present after replay. Sort for stable
		// comparison since the order of additions isn't part of the
		// contract.
		got := append([]string(nil), node.Labels...)
		sort.Strings(got)
		want := []string{"CharacterEmbedding", "TextEmbedding"}
		if len(got) != len(want) {
			t.Fatalf("after replay labels=%v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("label[%d]=%q, want %q (full set: got=%v want=%v)",
					i, got[i], want[i], got, want)
			}
		}

		// The tenant-scoped label index must also be rebuilt — this
		// is the post-condition the consumer's label-filtered vector
		// search relies on. A label that's on the Node struct but
		// missing from the index would silently break HNSW filtering.
		got2 := gs.GetNodesByLabelForTenant("tenant-A", "CharacterEmbedding")
		if len(got2) != 1 || got2[0].ID != nodeID {
			t.Errorf("after replay GetNodesByLabelForTenant: want [%d], got %d nodes",
				nodeID, len(got2))
		}
	}
}

// TestWAL_RemoveNodeLabel_SurvivesRestart is the symmetric test for
// OpRemoveNodeLabel. A removal applied at runtime must NOT come back
// after restart — the post-removal label set is what replay must
// reach.
func TestWAL_RemoveNodeLabel_SurvivesRestart(t *testing.T) {
	dataDir := t.TempDir()
	defer func() { _ = os.RemoveAll(dataDir) }()

	var nodeID uint64

	{
		gs := testCrashableStorage(t, dataDir, StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})

		node, err := gs.CreateNodeWithTenant("tenant-A",
			[]string{"Primary", "Secondary"}, nil)
		if err != nil {
			t.Fatalf("Phase 1 CreateNodeWithTenant: %v", err)
		}
		nodeID = node.ID

		if err := gs.RemoveNodeLabelForTenant(nodeID, "tenant-A", "Secondary"); err != nil {
			t.Fatalf("Phase 1 RemoveNodeLabelForTenant: %v", err)
		}
		// Do NOT call Close — simulating a crash before clean shutdown.
	}

	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Phase 2 NewGraphStorageWithConfig: %v", err)
		}
		defer func() { _ = gs.Close() }()

		node, err := gs.GetNodeForTenant(nodeID, "tenant-A")
		if err != nil {
			t.Fatalf("Phase 2 GetNodeForTenant: %v", err)
		}
		if len(node.Labels) != 1 || node.Labels[0] != "Primary" {
			t.Errorf("after replay labels=%v, want [Primary]", node.Labels)
		}

		// Removed label must NOT come back in the tenant-scoped index.
		if got := gs.GetNodesByLabelForTenant("tenant-A", "Secondary"); len(got) != 0 {
			t.Errorf("after replay GetNodesByLabelForTenant(Secondary): want 0, got %d", len(got))
		}
		// Surviving label must still be indexed.
		got := gs.GetNodesByLabelForTenant("tenant-A", "Primary")
		if len(got) != 1 || got[0].ID != nodeID {
			t.Errorf("after replay GetNodesByLabelForTenant(Primary): want [%d], got %d nodes",
				nodeID, len(got))
		}
	}
}

// TestAddNodeLabelsForTenant_TenantIsolation pins the storage-layer
// tenant gate independently of the HTTP handler. The HTTP test covers
// the same property but a future caller that goes direct to the
// storage method (e.g. a Cypher SET extension landing later) deserves
// its own pin.
func TestAddNodeLabelsForTenant_TenantIsolation(t *testing.T) {
	dataDir := t.TempDir()
	defer func() { _ = os.RemoveAll(dataDir) }()

	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	node, err := gs.CreateNodeWithTenant("tenant-A", []string{"Owner"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	added, err := gs.AddNodeLabelsForTenant(node.ID, "tenant-B", []string{"Stolen"})
	if err != ErrNodeNotFound {
		t.Errorf("cross-tenant add: want ErrNodeNotFound, got err=%v added=%v", err, added)
	}

	// Owner-side readback: labels must be untouched.
	got, err := gs.GetNodeForTenant(node.ID, "tenant-A")
	if err != nil {
		t.Fatalf("owner readback: %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "Owner" {
		t.Errorf("cross-tenant add leaked through: labels=%v, want [Owner]", got.Labels)
	}
}

// TestRemoveNodeLabelForTenant_ErrLabelNotPresent covers the
// distinct error path for "node exists, label doesn't" — the
// consumer-distinguishable case the HTTP handler maps to a 404
// with a label-specific message.
func TestRemoveNodeLabelForTenant_ErrLabelNotPresent(t *testing.T) {
	dataDir := t.TempDir()
	defer func() { _ = os.RemoveAll(dataDir) }()

	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	node, err := gs.CreateNodeWithTenant("tenant-A",
		[]string{"OnlyOne", "Backup"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	err = gs.RemoveNodeLabelForTenant(node.ID, "tenant-A", "NotThere")
	if err != ErrLabelNotPresent {
		t.Errorf("want ErrLabelNotPresent, got %v", err)
	}
}

// TestRemoveNodeLabelForTenant_ErrLabelLastLabel pins the
// last-label-protection invariant.
func TestRemoveNodeLabelForTenant_ErrLabelLastLabel(t *testing.T) {
	dataDir := t.TempDir()
	defer func() { _ = os.RemoveAll(dataDir) }()

	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	node, err := gs.CreateNodeWithTenant("tenant-A", []string{"Only"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	err = gs.RemoveNodeLabelForTenant(node.ID, "tenant-A", "Only")
	if err != ErrLabelLastLabel {
		t.Errorf("want ErrLabelLastLabel, got %v", err)
	}
}

// TestAddNodeLabelsForTenant_Idempotent_NoIndexDuplication pins the
// belt-and-braces invariant that re-adding a label cannot cause
// duplicate entries in the global or tenant-scoped indexes. A
// double-insert would silently corrupt iteration counts (a label
// filter would report the node twice).
func TestAddNodeLabelsForTenant_Idempotent_NoIndexDuplication(t *testing.T) {
	dataDir := t.TempDir()
	defer func() { _ = os.RemoveAll(dataDir) }()

	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	node, err := gs.CreateNodeWithTenant("tenant-A", []string{"Base"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Add the same label five times.
	for i := 0; i < 5; i++ {
		if _, err := gs.AddNodeLabelsForTenant(node.ID, "tenant-A", []string{"Repeat"}); err != nil {
			t.Fatalf("AddNodeLabelsForTenant iter %d: %v", i, err)
		}
	}

	// The node's label slice should only carry the label once.
	got, err := gs.GetNodeForTenant(node.ID, "tenant-A")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	repeatCount := 0
	for _, l := range got.Labels {
		if l == "Repeat" {
			repeatCount++
		}
	}
	if repeatCount != 1 {
		t.Errorf("node.Labels has %d copies of 'Repeat', want 1: %v", repeatCount, got.Labels)
	}

	// And the tenant index should return the node exactly once.
	indexed := gs.GetNodesByLabelForTenant("tenant-A", "Repeat")
	if len(indexed) != 1 {
		t.Errorf("GetNodesByLabelForTenant: want 1 entry, got %d", len(indexed))
	}
}
