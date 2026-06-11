package storage

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// M-1 Option A (DESIGN_m1_wal_remanence_2026-06-10.md): CompactWAL()
// checkpoints — snapshot the current state with a boundary LSN captured
// under the same lock, then TruncateUpTo(boundary) so entries covered by
// the snapshot (including a deleted tenant's creates, i.e. its PII)
// leave the WAL immediately, while concurrent writers' entries survive.

// piiMarker is long enough that snappy (compressed backend) emits it as a
// verbatim literal, so raw-byte scanning works on all three backends.
const piiMarker = "tenant-secret-PII-marker-3c1f9a7e5b2d4068"

// walMarker is what actually appears in WAL bytes: Value.Data is []byte,
// which encoding/json base64-encodes. The property value is exactly the
// marker, so its base64 form appears contiguously in the entry JSON.
var walMarker = base64.StdEncoding.EncodeToString([]byte(piiMarker))

func compactBackends() []struct {
	name string
	cfg  func(dir string) StorageConfig
} {
	return []struct {
		name string
		cfg  func(dir string) StorageConfig
	}{
		{"plain", func(dir string) StorageConfig {
			return DefaultStorageConfig(dir)
		}},
		{"batched", func(dir string) StorageConfig {
			c := DefaultStorageConfig(dir)
			c.EnableBatching = true
			c.BatchSize = 8
			c.FlushInterval = time.Millisecond
			return c
		}},
		{"compressed", func(dir string) StorageConfig {
			c := DefaultStorageConfig(dir)
			c.EnableCompression = true
			return c
		}},
	}
}

func walFilePath(dir string, cfg StorageConfig) string {
	if cfg.EnableCompression {
		return filepath.Join(dir, "wal", "wal_compressed.log")
	}
	return filepath.Join(dir, "wal", "wal.log")
}

// walContainsMarker scans DECODED WAL entries for the marker. Raw-byte
// scanning is wrong for the compressed backend: snappy may split the
// marker across literal/copy ops depending on surrounding JSON (which
// varies with map iteration order), making a byte scan flaky.
func walContainsMarker(t *testing.T, dir string, cfg StorageConfig) bool {
	t.Helper()
	walDir := filepath.Join(dir, "wal")
	var entries []*wal.Entry
	var err error
	if cfg.EnableCompression {
		var cw *wal.CompressedWAL
		cw, err = wal.NewCompressedWAL(walDir)
		if err == nil {
			entries, err = cw.ReadAll()
			_ = cw.Close()
		}
	} else {
		var w *wal.WAL
		w, err = wal.NewWAL(walDir)
		if err == nil {
			entries, err = w.ReadAll()
			_ = w.Close()
		}
	}
	if err != nil {
		t.Fatalf("read WAL entries: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(string(e.Data), walMarker) {
			return true
		}
	}
	return false
}

func TestCompactWAL_PurgesDeletedTenantData(t *testing.T) {
	for _, backend := range compactBackends() {
		t.Run(backend.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := backend.cfg(dir)
			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("NewGraphStorageWithConfig: %v", err)
			}

			if _, err := gs.CreateNodeWithTenant("victim", []string{"Doc"}, map[string]Value{
				"secret": StringValue(piiMarker),
			}); err != nil {
				t.Fatalf("create victim node: %v", err)
			}
			survivor, err := gs.CreateNodeWithTenant("survivor", []string{"Doc"}, map[string]Value{
				"keep": StringValue("survivor-data"),
			})
			if err != nil {
				t.Fatalf("create survivor node: %v", err)
			}

			if !walContainsMarker(t, dir, cfg) {
				t.Fatalf("test premise broken: PII marker not in WAL before compaction")
			}

			if _, _, err := gs.DeleteTenant("victim"); err != nil {
				t.Fatalf("DeleteTenant: %v", err)
			}
			if err := gs.CompactWAL(); err != nil {
				t.Fatalf("CompactWAL: %v", err)
			}

			if walContainsMarker(t, dir, cfg) {
				t.Fatalf("deleted tenant's PII still in WAL after CompactWAL (remanence)")
			}
			// The WAL file itself must have been rewritten (not just
			// logically superseded) — remanence is about bytes on disk.
			if raw, err := os.ReadFile(walFilePath(dir, cfg)); err != nil {
				t.Fatalf("stat WAL post-compact: %v", err)
			} else if len(raw) > 0 && strings.Contains(string(raw), walMarker) {
				t.Fatalf("raw WAL bytes still carry the PII marker after CompactWAL")
			}

			// Crash-sim recovery (no Close): snapshot + truncated WAL must
			// restore the survivor and must NOT resurrect the victim.
			gs2, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("crash-recovery reopen: %v", err)
			}
			defer func() { _ = gs2.Close() }()
			if _, err := gs2.GetNodeForTenant(survivor.ID, "survivor"); err != nil {
				t.Fatalf("survivor node lost after compaction + crash recovery: %v", err)
			}
			if nodes := gs2.GetNodesByLabelForTenant("victim", "Doc"); len(nodes) != 0 {
				t.Fatalf("victim tenant resurrected after crash recovery: %d nodes", len(nodes))
			}
		})
	}
}

// The design doc's data-loss scenario: writes landing concurrently with
// the snapshot+truncate must survive a crash. Every create acknowledged
// to a writer goroutine must be present after recovery.
func TestCompactWAL_ConcurrentWritesNotLost(t *testing.T) {
	for _, backend := range compactBackends() {
		t.Run(backend.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := backend.cfg(dir)
			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("NewGraphStorageWithConfig: %v", err)
			}

			const writers, perWriter = 4, 40
			var mu sync.Mutex
			acked := make([]uint64, 0, writers*perWriter)
			var wg sync.WaitGroup
			for w := 0; w < writers; w++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					for i := 0; i < perWriter; i++ {
						node, err := gs.CreateNodeWithTenant("writer", []string{"W"}, map[string]Value{
							"k": StringValue(fmt.Sprintf("w%d-i%d", w, i)),
						})
						if err != nil {
							t.Errorf("create: %v", err)
							return
						}
						mu.Lock()
						acked = append(acked, node.ID)
						mu.Unlock()
					}
				}(w)
			}
			compactDone := make(chan struct{})
			go func() {
				defer close(compactDone)
				for i := 0; i < 8; i++ {
					if err := gs.CompactWAL(); err != nil {
						t.Errorf("CompactWAL: %v", err)
						return
					}
				}
			}()
			wg.Wait()
			<-compactDone
			if t.Failed() {
				return
			}

			// Crash-sim (no Close): every acknowledged write must recover.
			gs2, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("crash-recovery reopen: %v", err)
			}
			defer func() { _ = gs2.Close() }()
			for _, id := range acked {
				if _, err := gs2.GetNodeForTenant(id, "writer"); err != nil {
					t.Fatalf("acknowledged node %d lost after compaction under concurrency", id)
				}
			}
		})
	}
}

// Transaction.Commit applies in-memory under gs.mu but appends its WAL
// batch AFTER releasing the lock. A checkpoint boundary captured in that
// window would miss the commit's entries: the snapshot contains the data
// AND the surviving WAL re-applies it (or, worse ordering, loses it).
// Hammer commits against compactions and require exact recovery plus
// clean invariants.
func TestCompactWAL_TransactionCommitsSurviveCompaction(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultStorageConfig(dir)
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}

	const committers, perCommitter = 4, 30
	var mu sync.Mutex
	acked := make([]uint64, 0, committers*perCommitter)
	var wg sync.WaitGroup
	for c := 0; c < committers; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			for i := 0; i < perCommitter; i++ {
				tx, err := gs.BeginTransactionForTenant("txn-tenant")
				if err != nil {
					t.Errorf("begin tx: %v", err)
					return
				}
				node, err := tx.CreateNode([]string{"T"}, map[string]Value{
					"k": StringValue(fmt.Sprintf("c%d-i%d", c, i)),
				})
				if err != nil {
					t.Errorf("tx create: %v", err)
					return
				}
				if err := tx.Commit(); err != nil {
					t.Errorf("tx commit: %v", err)
					return
				}
				mu.Lock()
				acked = append(acked, node.ID)
				mu.Unlock()
			}
		}(c)
	}
	compactDone := make(chan struct{})
	go func() {
		defer close(compactDone)
		for i := 0; i < 8; i++ {
			if err := gs.CompactWAL(); err != nil {
				t.Errorf("CompactWAL: %v", err)
				return
			}
		}
	}()
	wg.Wait()
	<-compactDone
	if t.Failed() {
		return
	}

	gs2, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("crash-recovery reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()
	for _, id := range acked {
		if _, err := gs2.GetNodeForTenant(id, "txn-tenant"); err != nil {
			t.Fatalf("committed transaction node %d lost across compaction", id)
		}
	}
	// Over-replay (entry both in snapshot and surviving WAL) would drift
	// derived indexes; the parallel-invariant checker catches that class.
	if violations := checkGraphInvariants(gs2); len(violations) != 0 {
		t.Fatalf("invariant violations after recovery: %v", violations)
	}
}

func TestCompactWAL_NoWALConfiguredIsNoOp(t *testing.T) {
	cfg := DefaultStorageConfig(t.TempDir())
	cfg.BulkImportMode = true // no WAL
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()
	if err := gs.CompactWAL(); err != nil {
		t.Fatalf("CompactWAL without WAL should no-op, got: %v", err)
	}
}

// Pre-existing durability hole surfaced by the compact tests' premise
// check: with EnableCompression=true, enqueueWAL/writeToWALWithError had
// no compressed branch (single-op writes never logged) and replayWAL
// never replayed the compressed WAL. Crash-sim must recover a plain
// create on the compressed backend.
func TestCompressedWALBackend_SingleOpWritesAreDurable(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultStorageConfig(dir)
	cfg.EnableCompression = true
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	node, err := gs.CreateNodeWithTenant("t1", []string{"D"}, map[string]Value{
		"k": StringValue("compressed-durability"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Crash-sim: no Close, no snapshot — recovery must come from the WAL.
	gs2, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("crash-recovery reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()
	if _, err := gs2.GetNodeForTenant(node.ID, "t1"); err != nil {
		t.Fatalf("create on compressed backend lost across crash: %v", err)
	}
}
