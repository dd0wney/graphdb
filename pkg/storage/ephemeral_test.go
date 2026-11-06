package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// An Ephemeral store is one the caller deletes after Close: a per-scan store
// in a temp directory. It must write nothing durable, refuse to persist on
// request, and refuse a directory that already holds anything, so the flag
// cannot be pointed at a real store.

func ephemeralConfig(dir string) StorageConfig {
	cfg := DefaultStorageConfig(dir)
	cfg.Ephemeral = true
	return cfg
}

// populate writes a graph with properties on both nodes and edges, so a
// snapshot of it has real work to do.
func populate(t *testing.T, gs *GraphStorage, nodes int) {
	t.Helper()
	ids := make([]uint64, 0, nodes)
	for i := range nodes {
		n, err := gs.CreateNode([]string{"Function"}, map[string]Value{
			"name": StringValue(fmt.Sprintf("fn_%d", i)),
			"text": StringValue("func body text long enough to cost something in a snapshot"),
			"loc":  IntValue(int64(i)),
		})
		if err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
		ids = append(ids, n.ID)
	}
	for i := 1; i < len(ids); i++ {
		if _, err := gs.CreateEdge(ids[i-1], ids[i], "CALLS", map[string]Value{"op": StringValue("call")}, 1); err != nil {
			t.Fatalf("CreateEdge %d: %v", i, err)
		}
	}
}

// dirEntries lists every path under dir, relative to it, so a failure names
// exactly what was written.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(out)
	return out
}

func assertOnlyLock(t *testing.T, dir string) {
	t.Helper()
	for _, e := range dirEntries(t, dir) {
		if e != dataDirLockFile {
			t.Errorf("ephemeral store left %q in its data directory; want only %s. All entries: %v",
				e, dataDirLockFile, dirEntries(t, dir))
			return
		}
	}
}

func TestEphemeral_WritesNothingDurable(t *testing.T) {
	// EnableBatching and EnableCompression only pick a WAL backend; an
	// Ephemeral store has no WAL, so neither may bring one back.
	variants := map[string]func(*StorageConfig){
		"default":     func(*StorageConfig) {},
		"batching":    func(c *StorageConfig) { c.EnableBatching = true },
		"compression": func(c *StorageConfig) { c.EnableCompression = true },
		"bulk-import": func(c *StorageConfig) { c.BulkImportMode = true },
		"json-mode":   func(c *StorageConfig) { c.UseMmapSnapshot = false },
	}
	for name, tweak := range variants {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := ephemeralConfig(dir)
			tweak(&cfg)
			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			populate(t, gs, 50)
			if err := gs.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			assertOnlyLock(t, dir)
		})
	}
}

func TestEphemeral_SnapshotRefuses(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(ephemeralConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	populate(t, gs, 10)

	err = gs.Snapshot()
	if !errors.Is(err, ErrEphemeralStore) {
		t.Fatalf("Snapshot on an ephemeral store = %v, want ErrEphemeralStore", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertOnlyLock(t, dir)
}

func TestEphemeral_DeleteAllNodesWritesNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(ephemeralConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	n, err := gs.CreateNode([]string{"Function"}, nil)
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	if err := gs.DeleteAllNodes(); err != nil {
		t.Fatalf("DeleteAllNodes: %v", err)
	}
	// Not GetStatistics().NodeCount: clearAllUnderLock does not reset the
	// stats counters, a defect older than Ephemeral and outside this test.
	if _, err := gs.GetNode(n.ID); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("GetNode after DeleteAllNodes = %v, want ErrNodeNotFound", err)
	}
	assertOnlyLock(t, dir)
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// Close on an ephemeral store must not cost memory in proportion to the
// graph. The ordinary Close is the control: it must allocate a lot, or the
// comparison proves nothing.
func TestEphemeral_CloseAllocatesLittle(t *testing.T) {
	const nodes = 20000

	closeAlloc := func(t *testing.T, cfg StorageConfig) uint64 {
		t.Helper()
		gs, err := NewGraphStorageWithConfig(cfg)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		populate(t, gs, nodes)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	// The control is BulkImportMode: it has no WAL either, so the only
	// difference left is the snapshot Close writes. It also keeps 40k
	// per-write fsyncs off the macOS CI runner, where F_FULLFSYNC is slow.
	control := DefaultStorageConfig(t.TempDir())
	control.BulkImportMode = true
	ordinary := closeAlloc(t, control)
	ephemeral := closeAlloc(t, ephemeralConfig(t.TempDir()))
	t.Logf("Close allocated: ordinary %d bytes, ephemeral %d bytes (%d nodes)", ordinary, ephemeral, nodes)

	const controlFloor = 1 << 20
	if ordinary < controlFloor {
		t.Fatalf("control: ordinary Close allocated %d bytes, want at least %d; the graph is too small to tell the modes apart",
			ordinary, controlFloor)
	}
	if ephemeral*50 > ordinary {
		t.Errorf("ephemeral Close allocated %d bytes, more than 1/50 of the ordinary Close (%d bytes)", ephemeral, ordinary)
	}
}

func TestEphemeral_RefusesNonEmptyDirectory(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"durable store": func(t *testing.T, dir string) {
			gs, err := NewGraphStorage(dir)
			if err != nil {
				t.Fatalf("open ordinary store: %v", err)
			}
			populate(t, gs, 5)
			if err := gs.Close(); err != nil {
				t.Fatalf("close ordinary store: %v", err)
			}
		},
		"stray file": func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"empty subdirectory": func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "wal"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			prepare(t, dir)
			before := dirEntries(t, dir)

			gs, err := NewGraphStorageWithConfig(ephemeralConfig(dir))
			if err == nil {
				_ = gs.Close()
				t.Fatalf("ephemeral open of a non-empty directory succeeded; entries were %v", before)
			}
			if !errors.Is(err, ErrEphemeralStore) {
				t.Fatalf("open error = %v, want ErrEphemeralStore", err)
			}
		})
	}
}

// A listing that fails must refuse the open: "could not look" is not
// "nothing there".
func TestEphemeral_RefusesWhenDirectoryCannotBeListed(t *testing.T) {
	faults := vfstest.NewFaults(vfs.OS(), "ephemeral-readdir")
	faults.FailNthOp(1)
	cfg := ephemeralConfig(t.TempDir())
	cfg.FS = faults

	gs, err := NewGraphStorageWithConfig(cfg)
	if !faults.Fired() {
		t.Fatal("control: the injected fault never fired, so this test exercised nothing")
	}
	if err == nil {
		_ = gs.Close()
		t.Fatal("ephemeral open succeeded although the directory listing failed")
	}
	if !errors.Is(err, ErrEphemeralStore) || !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("open error = %v, want one that wraps ErrEphemeralStore and the injected fault", err)
	}
}

func TestEphemeral_AcceptsAbsentEmptyAndLockOnlyDirectory(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "scan")
		gs, err := NewGraphStorageWithConfig(ephemeralConfig(dir))
		if err != nil {
			t.Fatalf("open of an absent directory: %v", err)
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	t.Run("lock left by an earlier ephemeral store", func(t *testing.T) {
		dir := t.TempDir()
		for i := range 2 {
			gs, err := NewGraphStorageWithConfig(ephemeralConfig(dir))
			if err != nil {
				t.Fatalf("open %d: %v", i, err)
			}
			populate(t, gs, 5)
			if err := gs.Close(); err != nil {
				t.Fatalf("Close %d: %v", i, err)
			}
		}
	})
}
