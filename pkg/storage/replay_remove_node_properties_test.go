package storage

import (
	"encoding/json"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
	"github.com/dd0wney/graphdb/pkg/wal"
)

// TestReplayRemoveNodeProperties_SurvivesCrashRecovery pins that a property
// removed AFTER the last snapshot stays removed across a crash.
// RemoveNodeProperties logged wal.OpUpdateNode carrying the properties that
// remained, and replayUpdateNode merges that map into the snapshot copy of the
// node, so the removed key came back from the snapshot on recovery.
//
// Must reopen (the defect is only observable through WAL replay): session 1
// snapshots the node with both keys, session 2 removes one WAL-only then
// crashes, session 3 recovers and must not see the removed key.
func TestReplayRemoveNodeProperties_SurvivesCrashRecovery(t *testing.T) {
	for _, mode := range []struct {
		name string
		mmap bool
	}{{"json", false}, {"mmap", true}} {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := crashRecoveryConfig(dir)
			cfg.UseMmapSnapshot = mode.mmap
			assertRemovalSurvivesCrash(t, cfg)
		})
	}
}

func assertRemovalSurvivesCrash(t *testing.T, cfg StorageConfig) {
	t.Helper()
	dir := cfg.DataDir
	const tenant = "acme"
	var nodeID uint64

	{
		gs, err := NewGraphStorageWithConfig(cfg)
		if err != nil {
			t.Fatalf("session1 open: %v", err)
		}
		// An index on the removed key, so recovery must also drop the index
		// entry; CheckInvariants below compares the index to the nodes.
		if err := gs.CreatePropertyIndex("remove", TypeString); err != nil {
			t.Fatalf("create property index: %v", err)
		}
		n, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, map[string]Value{
			"keep":   StringValue("k"),
			"remove": StringValue("r"),
		})
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		nodeID = n.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("session1 close: %v", err)
		}
	}

	{
		gs := testCrashableStorage(t, dir, cfg)
		if err := gs.RemoveNodePropertiesForTenant(nodeID, []string{"remove"}, tenant); err != nil {
			t.Fatalf("session2 remove: %v", err)
		}
		// no Close — simulate crash.
	}

	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	n, err := gs.GetNodeForTenant(nodeID, tenant)
	if err != nil {
		t.Fatalf("get node after recovery: %v", err)
	}
	if v, ok := n.Properties["remove"]; ok {
		t.Errorf("removed property came back after crash recovery: remove=%v, properties=%v", v, n.Properties)
	}
	if got, _ := n.Properties["keep"].AsString(); got != "k" {
		t.Errorf("keep = %q after crash recovery, want \"k\"", got)
	}
	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("invariant violations after recovery: %v", violations)
	}
}

// TestNodeUpdateRecord_EncodesLikeTheOldPayload pins that an update with no
// removal encodes byte for byte as the anonymous struct it replaced, so WAL
// entries from a binary on either side of the change read the same.
func TestNodeUpdateRecord_EncodesLikeTheOldPayload(t *testing.T) {
	props := map[string]Value{"a": StringValue("1")}
	old, err := json.Marshal(struct {
		NodeID     uint64
		Properties map[string]Value
	}{NodeID: 7, Properties: props})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(nodeUpdateRecord{NodeID: 7, Properties: props})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(old) {
		t.Errorf("nodeUpdateRecord = %s, old payload = %s", got, old)
	}
}

// TestReplayRemoveNodeProperties_InterleavedWithUpdates pins that replay applies
// updates and removals in WAL order: set-remove-set ends set, set-remove ends
// absent. A replay that applied every removal first, or skipped Removed,
// would get one of these wrong.
func TestReplayRemoveNodeProperties_InterleavedWithUpdates(t *testing.T) {
	dir := t.TempDir()
	cfg := crashRecoveryConfig(dir)
	const tenant = "acme"
	var resetID, clearedID uint64

	{
		gs, err := NewGraphStorageWithConfig(cfg)
		if err != nil {
			t.Fatalf("session1 open: %v", err)
		}
		for _, id := range []*uint64{&resetID, &clearedID} {
			n, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, map[string]Value{"a": StringValue("0")})
			if err != nil {
				t.Fatalf("create node: %v", err)
			}
			*id = n.ID
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("session1 close: %v", err)
		}
	}

	{
		gs := testCrashableStorage(t, dir, cfg)
		steps := []func() error{
			func() error { return gs.UpdateNodeForTenant(resetID, map[string]Value{"a": StringValue("1")}, tenant) },
			func() error { return gs.RemoveNodePropertiesForTenant(resetID, []string{"a"}, tenant) },
			func() error { return gs.UpdateNodeForTenant(resetID, map[string]Value{"a": StringValue("2")}, tenant) },
			func() error {
				return gs.UpdateNodeForTenant(clearedID, map[string]Value{"a": StringValue("1")}, tenant)
			},
			func() error { return gs.RemoveNodePropertiesForTenant(clearedID, []string{"a"}, tenant) },
		}
		for i, step := range steps {
			if err := step(); err != nil {
				t.Fatalf("session2 step %d: %v", i, err)
			}
		}
		// no Close — simulate crash.
	}

	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	reset, err := gs.GetNodeForTenant(resetID, tenant)
	if err != nil {
		t.Fatalf("get reset node: %v", err)
	}
	if got, _ := reset.Properties["a"].AsString(); got != "2" {
		t.Errorf("set-remove-set: a = %q after recovery, want \"2\"", got)
	}
	cleared, err := gs.GetNodeForTenant(clearedID, tenant)
	if err != nil {
		t.Fatalf("get cleared node: %v", err)
	}
	if v, ok := cleared.Properties["a"]; ok {
		t.Errorf("set-remove: a = %v after recovery, want absent", v)
	}
}

// TestReplayRemoveNodeProperties_DropsVectorAfterRecovery pins that a removed
// vector-indexed property is not searchable after recovery. The index is
// rebuilt from the recovered nodes, so this holds only if replay removed the
// key from the node.
func TestReplayRemoveNodeProperties_DropsVectorAfterRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := crashRecoveryConfig(dir)
	const tenant = "acme"
	var nodeID uint64

	{
		gs, err := NewGraphStorageWithConfig(cfg)
		if err != nil {
			t.Fatalf("session1 open: %v", err)
		}
		if err := gs.CreateVectorIndexForTenant(tenant, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("CreateVectorIndexForTenant: %v", err)
		}
		n, err := gs.CreateNodeWithTenant(tenant, []string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		nodeID = n.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("session1 close: %v", err)
		}
	}

	{
		gs := testCrashableStorage(t, dir, cfg)
		if err := gs.RemoveNodePropertiesForTenant(nodeID, []string{"embedding"}, tenant); err != nil {
			t.Fatalf("session2 remove: %v", err)
		}
		// no Close — simulate crash.
	}

	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	res, err := gs.VectorSearchForTenant(tenant, "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("VectorSearch after recovery: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("VectorSearch = %d results after recovery, want 0: the removed vector came back", len(res))
	}
}

// TestReplayUpdateNode_OldRemovePayloadStillMerges pins how a remove entry
// written before nodeUpdateRecord.Removed existed replays: it carries the
// properties that remained and no Removed, so replay merges it and removes
// nothing. That is the old behaviour, kept so the fix does not reinterpret
// entries already on disk.
func TestReplayUpdateNode_OldRemovePayloadStillMerges(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	n, err := gs.CreateNode([]string{"N"}, map[string]Value{"keep": StringValue("k"), "gone": StringValue("g")})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	old, err := json.Marshal(struct {
		NodeID     uint64
		Properties map[string]Value
	}{NodeID: n.ID, Properties: map[string]Value{"keep": StringValue("k")}})
	if err != nil {
		t.Fatal(err)
	}

	gs.mu.Lock()
	err = gs.replayUpdateNode(&wal.Entry{OpType: wal.OpUpdateNode, Data: old})
	gs.mu.Unlock()
	if err != nil {
		t.Fatalf("replayUpdateNode: %v", err)
	}

	got, err := gs.GetNode(n.ID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if _, ok := got.Properties["gone"]; !ok {
		t.Errorf("old payload removed a key: properties = %v, want keep and gone", got.Properties)
	}
}
