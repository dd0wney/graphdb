package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSnapshot_FilePermissionsOwnerOnly pins security audit finding H-2
// for the storage layer: snapshot.json is customer-data-equivalent (a
// full flat dump of every node and edge), and the data directory holds it
// plus the WAL, so both must be owner-only. Before the fix the package
// constants were 0644/0755 (the audit assumed they were already 0600).
//
// RED against pre-fix code: filePermissions=0644, dirPermissions=0755.
func TestSnapshot_FilePermissionsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not applicable on Windows")
	}
	dataDir := filepath.Join(t.TempDir(), "snap-perms")
	gs, err := NewGraphStorageWithConfig(jsonConfig(dataDir))
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	if _, err := gs.CreateNode([]string{"Secret"}, map[string]Value{
		"pii": StringValue("sensitive"),
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := gs.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if info, err := os.Stat(dataDir); err != nil {
		t.Fatalf("stat dataDir: %v", err)
	} else if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("data dir mode %o: group/other bits set (want owner-only)", perm)
	}

	if info, err := os.Stat(filepath.Join(dataDir, "snapshot.json")); err != nil {
		t.Fatalf("stat snapshot.json: %v", err)
	} else if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("snapshot.json mode %o: group/other bits set (want owner-only)", perm)
	}
}
