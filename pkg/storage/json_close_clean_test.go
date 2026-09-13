package storage

// The JSON-mode counterpart of mmap_close_clean_test.go. Close on a JSON store
// rewrote snapshot.json on every exit; on the 2.0M-node ICIJ store that was
// 25 s and 17.5 GB for a read-only consumer (COI_SCREEN_REAL_CORPUS_2026-09-13.md,
// finding 1). The mmap fix (#598) could read the overlay and the tombstones;
// JSON mode has neither, so its proof that the file on disk already IS the
// live state is a different one, and every test below names one way that
// proof could be wrong.

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

func jsonSnapshotFileIdentity(t *testing.T, dir string) snapshotFingerprint {
	t.Helper()
	path := filepath.Join(dir, "snapshot.json")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return snapshotFingerprint{mtime: fi.ModTime(), sum: sha256.Sum256(b)}
}

// openJSON opens with cfg and fails the test unless the store took the JSON
// path — the behaviour under test only exists on that path.
func openJSON(t *testing.T, cfg StorageConfig) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if gs.mmapSnap != nil {
		t.Fatal("open took the mmap path")
	}
	return gs
}

// writeJSONFixture builds the reopen fixture under cfg, closes it so
// snapshot.json exists, and returns the fingerprints of the file and the data.
func writeJSONFixture(t *testing.T, cfg StorageConfig) (snapshotFingerprint, fingerprint) {
	t.Helper()
	gs := openJSON(t, cfg)
	buildReopenFixture(t, gs)
	want := fingerprintTenant(t, gs, rtTenantA)
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return jsonSnapshotFileIdentity(t, cfg.DataDir), want
}

func assertSnapshotUntouched(t *testing.T, before, after snapshotFingerprint, ctx string) {
	t.Helper()
	if after.sum != before.sum {
		t.Errorf("%s: Close rewrote snapshot.json: bytes changed", ctx)
	}
	if !after.mtime.Equal(before.mtime) {
		t.Errorf("%s: Close rewrote snapshot.json: mtime %v -> %v", ctx, before.mtime, after.mtime)
	}
}

// jsonWALBackends are the three WAL implementations a JSON store can log to.
// The boundary LSN is read differently from each (walBoundaryLSNLocked), so
// the read-only proof is checked against all three.
func jsonWALBackends(dir string) map[string]StorageConfig {
	plain := jsonConfig(dir)
	batched := jsonConfig(dir)
	batched.EnableBatching = true
	compressed := jsonConfig(dir)
	compressed.EnableCompression = true
	return map[string]StorageConfig{"plain": plain, "batched": batched, "compressed": compressed}
}

// A session that only reads must leave snapshot.json byte-for-byte and
// mtime-for-mtime as it found it.
func TestJSONClose_ReadOnlySessionLeavesSnapshotUntouched(t *testing.T) {
	for name, cfg := range jsonWALBackends(t.TempDir()) {
		t.Run(name, func(t *testing.T) {
			cfg.DataDir = filepath.Join(cfg.DataDir, name)
			before, wantData := writeJSONFixture(t, cfg)

			gs := openJSON(t, cfg)
			if _, err := gs.GetNodeForTenant(wantData.personIDs[0], rtTenantA); err != nil {
				t.Fatalf("read: %v", err)
			}
			if _, err := gs.GetNodesByLabelForTenant(rtTenantA, "Person"); err != nil {
				t.Fatalf("enumerate: %v", err)
			}
			assertFingerprintEqual(t, wantData, fingerprintTenant(t, gs, rtTenantA), "read-only session")
			if err := gs.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			assertSnapshotUntouched(t, before, jsonSnapshotFileIdentity(t, cfg.DataDir), name)

			gs2 := openJSON(t, cfg)
			defer gs2.Close()
			assertFingerprintEqual(t, wantData, fingerprintTenant(t, gs2, rtTenantA), "after read-only close")
		})
	}
}

// One write is enough to require the rewrite: the next open must see it.
func TestJSONClose_SessionWithOneWriteRewritesSnapshot(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	before, _ := writeJSONFixture(t, cfg)

	gs := openJSON(t, cfg)
	n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("late")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if jsonSnapshotFileIdentity(t, cfg.DataDir).sum == before.sum {
		t.Fatal("Close after a write left snapshot.json unchanged")
	}
	gs2 := openJSON(t, cfg)
	defer gs2.Close()
	if _, err := gs2.GetNodeForTenant(n.ID, rtTenantA); err != nil {
		t.Fatalf("the written node did not survive reopen: %v", err)
	}
}

// An index definition is logged like any write and must force the rewrite.
func TestJSONClose_SessionWithOnlyAnIndexDefinitionRewritesSnapshot(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	before, _ := writeJSONFixture(t, cfg)

	gs := openJSON(t, cfg)
	if err := gs.CreatePropertyIndex("name", TypeString); err != nil {
		t.Fatalf("create index: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if jsonSnapshotFileIdentity(t, cfg.DataDir).sum == before.sum {
		t.Fatal("Close after an index definition left snapshot.json unchanged")
	}
	gs2 := openJSON(t, cfg)
	defer gs2.Close()
	if _, ok := gs2.propertyIndexes["name"]; !ok {
		t.Fatal("the index definition did not survive reopen")
	}
}

// copyDir snapshots a data directory the way a crash would leave it: the
// files as they are on disk right now, with the store still open.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s -> %s: %v", src, dst, err)
	}
}

// A store that crashed after a write has that write in the WAL and not in
// snapshot.json. The next session replays it at open and writes nothing of
// its own — and "nothing logged since open" is true for it. It must still
// rewrite, or the WAL truncate at Close drops the only copy of the write.
func TestJSONClose_ReplayedWALEntriesRewriteSnapshot(t *testing.T) {
	live := jsonConfig(t.TempDir())
	_, _ = writeJSONFixture(t, live)
	gs := openJSON(t, live)
	n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("unsnapshotted")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	crashed := jsonConfig(t.TempDir())
	copyDir(t, live.DataDir, crashed.DataDir) // the write is in the WAL, not in snapshot.json
	if err := gs.Close(); err != nil {
		t.Fatalf("close live: %v", err)
	}

	before := jsonSnapshotFileIdentity(t, crashed.DataDir)
	gs2 := openJSON(t, crashed)
	if _, err := gs2.GetNodeForTenant(n.ID, rtTenantA); err != nil {
		t.Fatalf("replay did not restore the node, so the test cannot say anything: %v", err)
	}
	if err := gs2.Close(); err != nil {
		t.Fatalf("close crashed: %v", err)
	}
	if jsonSnapshotFileIdentity(t, crashed.DataDir).sum == before.sum {
		t.Fatal("Close after a replayed WAL left snapshot.json unchanged")
	}
	gs3 := openJSON(t, crashed)
	defer gs3.Close()
	if _, err := gs3.GetNodeForTenant(n.ID, rtTenantA); err != nil {
		t.Fatalf("the replayed node did not survive the next open: %v", err)
	}
}

// BulkImportMode has no WAL, so nothing can prove the session wrote nothing.
// The only safe answer is to write. The second case is the one that bites:
// an explicit Snapshot records a sync point at LSN 0, and the writes after
// it never move the LSN because there is no WAL to log them to.
func TestJSONClose_BulkImportModeAlwaysWrites(t *testing.T) {
	t.Run("read-only session", func(t *testing.T) {
		cfg := jsonConfig(t.TempDir())
		cfg.BulkImportMode = true
		before, _ := writeJSONFixture(t, cfg)

		gs := openJSON(t, cfg)
		if gs.hasWAL() {
			t.Fatal("BulkImportMode opened a WAL, so this test proves nothing")
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if jsonSnapshotFileIdentity(t, cfg.DataDir).mtime.Equal(before.mtime) {
			t.Fatal("Close skipped the rewrite on a store with no WAL")
		}
	})
	t.Run("write after an explicit snapshot", func(t *testing.T) {
		cfg := jsonConfig(t.TempDir())
		cfg.BulkImportMode = true
		_, _ = writeJSONFixture(t, cfg)

		gs := openJSON(t, cfg)
		if err := gs.Snapshot(); err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("unlogged")})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		gs2 := openJSON(t, cfg)
		defer gs2.Close()
		if _, err := gs2.GetNodeForTenant(n.ID, rtTenantA); err != nil {
			t.Fatalf("the unlogged write did not survive reopen: %v", err)
		}
	})
}

// SetEncryption logs nothing, and the snapshot on disk is plaintext. A Close
// that skips here leaves customer data in the clear after the operator
// turned encryption on.
func TestJSONClose_SetEncryptionRewritesSnapshot(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	before, wantData := writeJSONFixture(t, cfg)

	gs := openJSON(t, cfg)
	engine := testEncryptionEngine(t)
	gs.SetEncryption(engine, &mockKeyProvider{})
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if jsonSnapshotFileIdentity(t, cfg.DataDir).sum == before.sum {
		t.Fatal("Close after SetEncryption left the plaintext snapshot.json in place")
	}
	raw, err := os.ReadFile(filepath.Join(cfg.DataDir, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, encrypted, _, err := decodeSnapshotEnvelope(raw); err != nil || !encrypted {
		t.Fatalf("snapshot after SetEncryption: encrypted=%v err=%v", encrypted, err)
	}

	cfg.EncryptionEngine = engine
	gs2 := openJSON(t, cfg)
	defer gs2.Close()
	assertFingerprintEqual(t, wantData, fingerprintTenant(t, gs2, rtTenantA), "after encrypted close")
}

// The file can vanish under a running store. A clean session must then
// write, not skip, or the next open finds nothing.
func TestJSONClose_ReadOnlySessionWritesWhenFileIsGone(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	_, wantData := writeJSONFixture(t, cfg)

	gs := openJSON(t, cfg)
	path := filepath.Join(cfg.DataDir, "snapshot.json")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Close skipped the write and left no snapshot on disk: %v", err)
	}
	gs2 := openJSON(t, cfg)
	defer gs2.Close()
	assertFingerprintEqual(t, wantData, fingerprintTenant(t, gs2, rtTenantA), "after file removal")
}

// An explicit Snapshot mid-session is a sync point too: a Close with no
// write after it has nothing to add.
func TestJSONClose_ExplicitSnapshotThenCloseSkips(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	_, _ = writeJSONFixture(t, cfg)

	gs := openJSON(t, cfg)
	if _, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("then-snapshot")}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := gs.Snapshot(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	mid := jsonSnapshotFileIdentity(t, cfg.DataDir)
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	assertSnapshotUntouched(t, mid, jsonSnapshotFileIdentity(t, cfg.DataDir), "close after explicit snapshot")
}

// DeleteAllNodes truncates the WAL, which resets the LSN to 0 — the same
// value a read-only session's boundary holds. If its own empty snapshot then
// fails to land, the old data is on disk and nothing is in memory, and a
// Close that reads "LSN unchanged since open" skips the rewrite: the next
// open resurrects everything the caller was told is gone.
func TestJSONClose_DeleteAllNodesWithFailedSnapshotStillRewrites(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	_, wantData := writeJSONFixture(t, cfg)

	faults := vfstest.NewFaults(vfs.OS(), "delete-all")
	cfg.FS = faults
	gs := openJSON(t, cfg)
	faults.FailWrite(vfstest.Once, 0) // the next write is the empty snapshot's temp file
	err := gs.DeleteAllNodes()
	if !faults.Fired() {
		t.Fatal("the write fault never fired, so DeleteAllNodes did not reach its snapshot")
	}
	if err == nil {
		t.Fatal("DeleteAllNodes reported success although its snapshot write failed")
	}
	faults.Clear()
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	gs2 := openJSON(t, cfg)
	defer gs2.Close()
	if _, err := gs2.GetNodeForTenant(wantData.personIDs[0], rtTenantA); err != ErrNodeNotFound {
		t.Fatalf("a node deleted by DeleteAllNodes came back after reopen: err=%v", err)
	}
}

// Edge compression runs at the top of every snapshot, before the clean
// check, and on a clean Close it is the whole remaining cost: 1.1 s on the
// 2.0M-node ICIJ store for a file that is not going to be written. A clean
// Close must leave the adjacency lists as it found them.
func TestJSONClose_ReadOnlySessionSkipsEdgeCompression(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	cfg.EnableEdgeCompression = true
	_, _ = writeJSONFixture(t, cfg)

	gs := openJSON(t, cfg)
	if len(gs.outgoingEdges) == 0 {
		t.Fatal("open did not populate the plain adjacency map, so this test proves nothing")
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(gs.outgoingEdges) == 0 {
		t.Fatal("a clean Close compressed the adjacency lists before deciding it had nothing to write")
	}
}

// A WAL append that fails rolls the LSN back (wal.go, "Rollback LSN on
// error") and the single-op write paths log the error instead of returning
// it. The node is in memory and not in the WAL, and "nothing logged since
// the sync point" is true. Before this change, the rewrite at Close was
// what saved that node. The property is 1 MB so the entry is larger than the
// WAL's 4 KB buffer and its write reaches the file inside writeEntry.
func TestJSONClose_FailedWALAppendRewritesSnapshot(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	_, _ = writeJSONFixture(t, cfg)

	faults := vfstest.NewFaults(vfs.OS(), "wal-append")
	cfg.FS = faults
	gs := openJSON(t, cfg)
	big := StringValue(strings.Repeat("x", 1<<20))
	faults.FailWrite(vfstest.Once, 0) // the next write is the WAL append for the create
	n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"blob": big})
	if !errors.Is(err, ErrWALWriteFailed) {
		t.Fatalf("create after a WAL write fault: err=%v, want ErrWALWriteFailed", err)
	}
	if n == nil {
		t.Fatal("the create returned no node with its error; the change is applied in memory and the caller needs its ID")
	}
	if !faults.Fired() {
		t.Fatal("the write fault never fired, so the WAL append succeeded and this test proves nothing")
	}
	faults.Clear()
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	gs2 := openJSON(t, cfg)
	defer gs2.Close()
	if _, err := gs2.GetNodeForTenant(n.ID, rtTenantA); err != nil {
		t.Fatalf("a node the API reported as created is gone after reopen: %v", err)
	}
}

// A Snapshot that captured its boundary before DeleteAllNodes can publish
// after DeleteAllNodes' own empty snapshot. The old data then renames last,
// and a sync point recorded for it would say the file is clean at the same
// LSN a read-only Close sees (0, because the truncate reset it). The
// ordering cannot be produced with a filesystem pause, because the parked
// publish holds jsonPublishMu, so the test drives the two halves by hand.
func TestJSONClose_StalePublishAfterDeleteAllNodesIsNotClean(t *testing.T) {
	cfg := jsonConfig(t.TempDir())
	_, wantData := writeJSONFixture(t, cfg)
	stale, err := os.ReadFile(filepath.Join(cfg.DataDir, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}

	gs := openJSON(t, cfg)
	gs.mu.RLock()
	boundary := gs.walBoundaryLSNLocked()
	engine := gs.encryptionEngine
	epoch := gs.jsonSyncEpoch
	gs.mu.RUnlock()

	if err := gs.DeleteAllNodes(); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	// The publish that started before the clear lands last, with the old bytes.
	if err := gs.publishJSONSnapshot(stale, boundary, engine, epoch); err != nil {
		t.Fatalf("stale publish: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	gs2 := openJSON(t, cfg)
	defer gs2.Close()
	if _, err := gs2.GetNodeForTenant(wantData.personIDs[0], rtTenantA); err != ErrNodeNotFound {
		t.Fatalf("a node deleted by DeleteAllNodes came back after a stale publish: err=%v", err)
	}
}
