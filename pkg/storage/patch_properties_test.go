package storage

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// TestPatchNode_SetAndRemoveSurviveCrash pins that one PatchNodeForTenant call
// sets some keys and removes others, and that both halves survive a crash. A
// PUT with a null and a value in one body must not land half-applied, so the
// patch is one write and one WAL record.
func TestPatchNode_SetAndRemoveSurviveCrash(t *testing.T) {
	for _, mode := range []struct {
		name string
		mmap bool
	}{{"json", false}, {"mmap", true}} {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := crashRecoveryConfig(dir)
			cfg.UseMmapSnapshot = mode.mmap
			const tenant = "acme"
			var nodeID uint64

			{
				gs, err := NewGraphStorageWithConfig(cfg)
				if err != nil {
					t.Fatalf("session1 open: %v", err)
				}
				n, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, map[string]Value{
					"keep":  StringValue("k"),
					"clear": StringValue("c"),
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
				if err := gs.PatchNodeForTenant(nodeID, map[string]Value{"added": StringValue("a")}, []string{"clear"}, tenant); err != nil {
					t.Fatalf("session2 patch: %v", err)
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
				t.Fatalf("get node: %v", err)
			}
			if v, ok := n.Properties["clear"]; ok {
				t.Errorf("clear = %v after recovery, want removed", v)
			}
			keep, _ := n.Properties["keep"].AsString()
			added, _ := n.Properties["added"].AsString()
			if keep != "k" || added != "a" {
				t.Errorf("properties = %v after recovery, want keep=k added=a", n.Properties)
			}
			assertGraphInvariants(t, gs)
		})
	}
}

// TestPatchNode_WrongTenantIsNotFound pins the *ForTenant convention: a patch
// from another tenant reports ErrNodeNotFound and changes nothing.
func TestPatchNode_WrongTenantIsNotFound(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	n, err := gs.CreateNodeWithTenant("acme", []string{"N"}, map[string]Value{"a": StringValue("1")})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := gs.PatchNodeForTenant(n.ID, nil, []string{"a"}, "other"); err != ErrNodeNotFound {
		t.Fatalf("cross-tenant patch: err = %v, want ErrNodeNotFound", err)
	}
	got, err := gs.GetNodeForTenant(n.ID, "acme")
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if _, ok := got.Properties["a"]; !ok {
		t.Errorf("cross-tenant patch removed a key: properties = %v", got.Properties)
	}
}

// TestPatchNode_RemovesVectorEntry pins that a patch removing a vector-indexed
// key also takes the vector out of the index, as RemoveNodeProperties does.
func TestPatchNode_RemovesVectorEntry(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tn = "acme"
	if err := gs.CreateVectorIndexForTenant(tn, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}
	n, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := gs.PatchNodeForTenant(n.ID, map[string]Value{"title": StringValue("t")}, []string{"embedding"}, tn); err != nil {
		t.Fatalf("patch: %v", err)
	}
	res, err := gs.VectorSearchForTenant(tn, "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("VectorSearch = %d results after the patch removed the vector, want 0", len(res))
	}
	assertGraphInvariants(t, gs)
}

// TestPatchEdge_RemoveSurvivesCrash pins edge property removal. Edges had no
// removal path at all. The edge WAL record is the full edge and replay
// replaces, so the removal survives as long as the live path deletes the key
// before it logs.
func TestPatchEdge_RemoveSurvivesCrash(t *testing.T) {
	dir := t.TempDir()
	cfg := crashRecoveryConfig(dir)
	const tenant = "acme"
	var edgeID uint64

	{
		gs, err := NewGraphStorageWithConfig(cfg)
		if err != nil {
			t.Fatalf("session1 open: %v", err)
		}
		a, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
		if err != nil {
			t.Fatalf("create a: %v", err)
		}
		b, err := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
		if err != nil {
			t.Fatalf("create b: %v", err)
		}
		e, err := gs.CreateEdgeWithTenant(tenant, a.ID, b.ID, "LINK", map[string]Value{
			"keep":  StringValue("k"),
			"clear": StringValue("c"),
		}, 1.0)
		if err != nil {
			t.Fatalf("create edge: %v", err)
		}
		edgeID = e.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("session1 close: %v", err)
		}
	}

	{
		gs := testCrashableStorage(t, dir, cfg)
		if err := gs.PatchEdgeForTenant(edgeID, nil, []string{"clear"}, nil, tenant); err != nil {
			t.Fatalf("session2 patch: %v", err)
		}
		// no Close — simulate crash.
	}

	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("recovery open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	e, err := gs.GetEdgeForTenant(edgeID, tenant)
	if err != nil {
		t.Fatalf("get edge: %v", err)
	}
	if v, ok := e.Properties["clear"]; ok {
		t.Errorf("clear = %v after recovery, want removed", v)
	}
	if got, _ := e.Properties["keep"].AsString(); got != "k" {
		t.Errorf("keep = %q after recovery, want \"k\"", got)
	}
	if e.Weight != 1.0 {
		t.Errorf("weight = %v, want 1.0 unchanged by a nil weight", e.Weight)
	}
}

// TestPatchNode_BadVectorChangesNothing pins that a patch refused for a bad
// vector leaves the node, the property indexes and the vector index as they
// were. The vector check used to run after the in-memory mutation, so a
// refused patch still applied its removals and its other sets, wrote no WAL
// record, and left the removed vector searchable.
func TestPatchNode_BadVectorChangesNothing(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tn = "acme"
	for _, prop := range []string{"emb", "old"} {
		if err := gs.CreateVectorIndexForTenant(tn, prop, 3, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("CreateVectorIndexForTenant %s: %v", prop, err)
		}
	}
	n, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, map[string]Value{"old": VectorValue([]float32{1, 0, 0})})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	err = gs.PatchNodeForTenant(n.ID, map[string]Value{
		"emb":   VectorValue([]float32{1, 2}), // wrong dimension
		"title": StringValue("t"),
	}, []string{"old"}, tn)
	if err == nil {
		t.Fatal("patch with a wrong-dimension vector succeeded, want an error")
	}

	got, err := gs.GetNodeForTenant(n.ID, tn)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if _, ok := got.Properties["old"]; !ok {
		t.Errorf("refused patch removed old: properties = %v", got.Properties)
	}
	if _, ok := got.Properties["title"]; ok {
		t.Errorf("refused patch set title: properties = %v", got.Properties)
	}
	if res, err := gs.VectorSearchForTenant(tn, "old", []float32{1, 0, 0}, 1, 50); err != nil || len(res) != 1 {
		t.Errorf("VectorSearch old = %d results, err=%v; want 1", len(res), err)
	}
	assertGraphInvariants(t, gs)
}
