package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// BulkImportMode opens no WAL, so it cannot replay one. A WAL left by a
// session that never closed holds acknowledged writes the snapshot does not
// have; a bulk session that ignores them hands their node IDs out again, and
// its Close publishes a snapshot without them. The writes are then gone with
// no error and a clean CheckInvariants.

// priorSessionConfigs are the WAL backends an earlier, non-bulk session may
// have written with. The bulk open must notice entries from any of them.
func priorSessionConfigs(dir string) map[string]StorageConfig {
	plain := DefaultStorageConfig(dir)

	batched := DefaultStorageConfig(dir)
	batched.EnableBatching = true

	compressed := DefaultStorageConfig(dir)
	compressed.EnableCompression = true

	return map[string]StorageConfig{"plain": plain, "batched": batched, "compressed": compressed}
}

func bulkConfig(dir string) StorageConfig {
	cfg := DefaultStorageConfig(dir)
	cfg.BulkImportMode = true
	return cfg
}

func TestBulkImportMode_RefusesWALEntriesPastSnapshot(t *testing.T) {
	for name, prior := range priorSessionConfigs("") {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			prior.DataDir = dir

			crashed := testCrashableStorage(t, dir, prior)
			a, err := crashed.CreateNode([]string{"A"}, map[string]Value{"name": StringValue("acknowledged")})
			if err != nil {
				t.Fatalf("CreateNode: %v", err)
			}
			// No Close: the write is in the WAL and in no snapshot.

			bulk, err := NewGraphStorageWithConfig(bulkConfig(dir))
			if err == nil {
				_ = bulk.Close()
				t.Fatal("bulk open succeeded over a WAL holding an unreplayed write")
			}
			if !errors.Is(err, ErrBulkImportUnreplayedWAL) {
				t.Fatalf("bulk open error = %v, want ErrBulkImportUnreplayedWAL", err)
			}

			// The refusal must leave the write recoverable by a normal open.
			recovered, err := NewGraphStorageWithConfig(prior)
			if err != nil {
				t.Fatalf("normal reopen after the refusal: %v", err)
			}
			defer recovered.Close()
			if _, err := recovered.GetNode(a.ID); err != nil {
				t.Fatalf("acknowledged node %d lost after the refusal: %v", a.ID, err)
			}
		})
	}
}

// A WAL whose entries the snapshot already covers holds nothing to replay, so
// the bulk open must accept it. Its Close must then still record that those
// entries are covered: a boundary of 0 would make the next normal open apply
// them a second time on top of the bulk session's snapshot. Re-applying a
// create over the same node changes nothing visible, so the bulk session
// deletes that node: a second apply brings it back.
func TestBulkImportMode_AcceptsCoveredWALAndKeepsItCovered(t *testing.T) {
	for name, prior := range priorSessionConfigs("") {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			prior.DataDir = dir

			crashed := testCrashableStorage(t, dir, prior)
			a, err := crashed.CreateNode([]string{"A"}, map[string]Value{"name": StringValue("covered")})
			if err != nil {
				t.Fatalf("CreateNode: %v", err)
			}
			if err := crashed.Snapshot(); err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			// No Close: the WAL keeps the entry, and the snapshot covers it.

			bulk, err := NewGraphStorageWithConfig(bulkConfig(dir))
			if err != nil {
				t.Fatalf("bulk open over a fully covered WAL: %v", err)
			}
			if err := bulk.DeleteNode(a.ID); err != nil {
				t.Fatalf("bulk DeleteNode: %v", err)
			}
			if _, err := bulk.CreateNode([]string{"B"}, map[string]Value{"name": StringValue("bulk")}); err != nil {
				t.Fatalf("bulk CreateNode: %v", err)
			}
			if err := bulk.Close(); err != nil {
				t.Fatalf("bulk Close: %v", err)
			}

			again, err := NewGraphStorageWithConfig(prior)
			if err != nil {
				t.Fatalf("normal reopen: %v", err)
			}
			defer again.Close()

			counts := map[string]int{}
			for _, n := range again.GetAllNodesAcrossTenants() {
				counts[n.Labels[0]]++
			}
			if counts["B"] != 1 || len(counts) != 1 {
				t.Fatalf("nodes by label after reopen = %v, want B:1 only (A was deleted in the bulk session)", counts)
			}
			if violations, err := CheckInvariants(again); err != nil || len(violations) > 0 {
				t.Fatalf("invariants after reopen: err=%v violations=%v", err, violations)
			}
		})
	}
}

func TestBulkImportMode_OpensEmptyDirectory(t *testing.T) {
	bulk, err := NewGraphStorageWithConfig(bulkConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("bulk open of an empty directory: %v", err)
	}
	if err := bulk.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// A WAL whose highest LSN is below the snapshot boundary did not come from
// the session that wrote the snapshot, for example after a binary that reset
// the LSN on truncate. A normal open refuses it (guardWALNotBehindSnapshot);
// a bulk open must not be the way around that refusal.
func TestBulkImportMode_RefusesWALBehindSnapshot(t *testing.T) {
	dir := t.TempDir()
	prior := DefaultStorageConfig(dir)
	walPath := filepath.Join(dir, "wal", "wal.log")

	crashed := testCrashableStorage(t, dir, prior)
	if _, err := crashed.CreateNode([]string{"A"}, nil); err != nil {
		t.Fatalf("CreateNode A: %v", err)
	}
	early, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("read WAL after the first write: %v", err)
	}
	if _, err := crashed.CreateNode([]string{"B"}, nil); err != nil {
		t.Fatalf("CreateNode B: %v", err)
	}
	if err := crashed.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// The snapshot covers LSN 2; put back a WAL that ends at LSN 1.
	if err := os.WriteFile(walPath, early, 0o600); err != nil {
		t.Fatalf("rewrite WAL: %v", err)
	}

	if _, err := NewGraphStorageWithConfig(prior); !errors.Is(err, ErrWALBehindSnapshot) {
		t.Fatalf("control: normal open error = %v, want ErrWALBehindSnapshot", err)
	}
	bulk, err := NewGraphStorageWithConfig(bulkConfig(dir))
	if err == nil {
		_ = bulk.Close()
		t.Fatal("bulk open succeeded over a WAL behind the snapshot")
	}
	if !errors.Is(err, ErrWALBehindSnapshot) {
		t.Fatalf("bulk open error = %v, want ErrWALBehindSnapshot", err)
	}
}
