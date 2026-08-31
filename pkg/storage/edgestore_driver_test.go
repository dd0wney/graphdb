package storage

// StorageConfig.FS is documented as the driver every file operation in this
// package goes through. The disk-backed edge store did not go through it.
//
// NewEdgeStore took a directory and a cache size and no filesystem, and built
// lsm.LSMOptions without setting FS. lsm.LSMOptions.FS defaults to
// vfs.Default(), so a store configured with a fault, crash or in-memory driver
// ran the WAL, the snapshot and the btree on that driver and every EDGE on the
// real disk. Two filesystems at once, with nothing said about it.
//
// That is a correctness bug before it is a testability one. StorageConfig.FS
// offers "serve a store from memory", and a caller doing that silently wrote
// edges to the real disk.
//
// Reported by the github.com/dd0wney/fault session, whose leak sweep could not
// see the edge store at all and said so rather than reporting a pass:
// NewEdgeStore is outside the seam, so no injected fault can reach it, and
// "the sweep found nothing" and "the sweep never saw it" render identically.
//
// The control below is the part that matters. A zero count under the edge
// store's path proves nothing on its own — it reads the same whether the
// driver was bypassed or never installed.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

const (
	roleEdgeStore = vfstest.Role("edgestore")
	roleRestOfDB  = vfstest.Role("restofdb")
)

// The edge store lives under <dataDir>/edgestore/edges-lsm.
func edgeStoreClassifier() vfstest.Classifier {
	return func(_ vfstest.Op, name string, _ int) vfstest.Role {
		if strings.Contains(name, "edges-lsm") {
			return roleEdgeStore
		}
		return roleRestOfDB
	}
}

func TestEdgeStoreGoesThroughStorageConfigFS(t *testing.T) {
	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "edgestore-driver", edgeStoreClassifier())

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
		FS:                 fs,
	})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer func() { _ = gs.Close() }()

	a, err := gs.CreateNode([]string{"A"}, nil)
	if err != nil {
		t.Fatalf("create node a: %v", err)
	}
	b, err := gs.CreateNode([]string{"B"}, nil)
	if err != nil {
		t.Fatalf("create node b: %v", err)
	}
	if _, err := gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1); err != nil {
		t.Fatalf("create edge: %v", err)
	}

	// THE CONTROL. If the driver saw nothing anywhere then it was never
	// installed, and the assertion below would be measuring the wrong thing
	// while reading exactly like a real failure.
	if got := fs.OpsForRole(roleRestOfDB); got == 0 {
		t.Fatalf("the driver observed no operation anywhere in the store, so it is not " +
			"installed and this test cannot say anything about the edge store")
	}

	if got := fs.OpsForRole(roleEdgeStore); got == 0 {
		t.Errorf("the driver observed %d operations elsewhere in the store and 0 under "+
			"edges-lsm, so the edge store bypassed StorageConfig.FS and wrote to the real "+
			"disk; a caller serving this store from memory would not know",
			fs.OpsForRole(roleRestOfDB))
	}
}

// NewEdgeStore keeps working on the default driver.
func TestNewEdgeStoreStillUsesTheDefaultDriver(t *testing.T) {
	es, err := NewEdgeStore(filepath.Join(t.TempDir(), "edgestore"), 10)
	if err != nil {
		t.Fatalf("NewEdgeStore: %v", err)
	}
	if err := es.StoreOutgoingEdges(1, []uint64{2, 3}); err != nil {
		t.Fatalf("StoreOutgoingEdges: %v", err)
	}
	got, err := es.GetOutgoingEdges(1)
	if err != nil {
		t.Fatalf("GetOutgoingEdges: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d edges, want 2", len(got))
	}
}
