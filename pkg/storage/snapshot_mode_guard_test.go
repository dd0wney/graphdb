package storage

import (
	"path/filepath"
	"strings"
	"testing"
)

// A store closed in mmap mode holds snapshot.mmap, a truncated WAL, and no
// snapshot.json. Reopening it in JSON mode used to report success and serve an
// empty database: loadFromDisk returned os.ErrNotExist for the absent
// snapshot.json, and the caller reads that as "fresh database". The mmap
// snapshot sitting beside it was never consulted.
//
// DEPLOYMENT_GUIDE.md sent operators into exactly this path as a rollback step.
func TestOpenInJSONModeRefusesWhenOnlyMmapSnapshotExists(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open in mmap mode: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, nil); err != nil {
			t.Fatalf("CreateNodeWithTenant: %v", err)
		}
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close in mmap mode: %v", err)
	}

	// Preconditions. Without these the assertion below could pass for the
	// wrong reason.
	if fileExists(filepath.Join(dir, "snapshot.json")) {
		t.Fatal("precondition: snapshot.json exists, so JSON mode has its own snapshot to read")
	}
	if !fileExists(mmapSnapshotPath(dir)) {
		t.Fatal("precondition: snapshot.mmap is absent after an mmap-mode Close")
	}

	reopened, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err == nil {
		got := reopened.GetStatistics().NodeCount
		_ = reopened.Close()
		t.Fatalf("JSON-mode open succeeded and served %d of 3 nodes; want a refusal", got)
	}
	if !strings.Contains(err.Error(), "snapshot.mmap") {
		t.Fatalf("refusal does not name the cause: %v", err)
	}
}

// The reverse direction is a supported migration, not a hazard: a legacy
// JSON-only store opens in mmap mode and writes snapshot.mmap on its next
// Close. This test holds the guard to the one direction that loses data.
func TestOpenInMmapModeAcceptsAJSONOnlyStore(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("open in JSON mode: %v", err)
	}
	if _, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, nil); err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close in JSON mode: %v", err)
	}
	if fileExists(mmapSnapshotPath(dir)) {
		t.Fatal("precondition: snapshot.mmap exists after a JSON-mode Close")
	}

	reopened, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("mmap-mode open of a JSON-only store: %v", err)
	}
	defer reopened.Close()
	if got := reopened.GetStatistics().NodeCount; got != 1 {
		t.Fatalf("node count after migration open = %d, want 1", got)
	}
}
