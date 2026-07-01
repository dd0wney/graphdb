package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestRunBackupRestore(t *testing.T) {
	t.Run("restores into a fresh dir", func(t *testing.T) {
		archive := writeTestArchive(t)
		dest := filepath.Join(t.TempDir(), "data")
		if err := runBackupRestore([]string{"--into", dest, archive}); err != nil {
			t.Fatalf("restore: %v", err)
		}
		gs, err := storage.NewGraphStorage(dest)
		if err != nil {
			t.Fatalf("reopen restored: %v", err)
		}
		defer gs.Close()
		if c := gs.CountNodesForTenant("t1"); c != 4 {
			t.Errorf("restored nodes = %d, want 4", c)
		}
	})

	t.Run("dry-run does not mutate the target", func(t *testing.T) {
		archive := writeTestArchive(t)
		dest := filepath.Join(t.TempDir(), "data")
		if err := runBackupRestore([]string{"--into", dest, "--dry-run", archive}); err != nil {
			t.Fatalf("dry-run: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dest, "snapshot.mmap")); !os.IsNotExist(err) {
			t.Errorf("dry-run wrote snapshot (err=%v)", err)
		}
	})

	t.Run("missing --into is an error", func(t *testing.T) {
		archive := writeTestArchive(t)
		if err := runBackupRestore([]string{archive}); err == nil {
			t.Error("expected error without --into")
		}
	})

	t.Run("corrupt archive aborts before touching target", func(t *testing.T) {
		archive := writeTestArchive(t)
		raw, err := os.ReadFile(archive)
		if err != nil {
			t.Fatal(err)
		}
		for i := len(raw) / 3; i < len(raw)/3+16 && i < len(raw); i++ {
			raw[i] ^= 0xff
		}
		corrupt := filepath.Join(t.TempDir(), "corrupt.tar.gz")
		if err := os.WriteFile(corrupt, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), "data")
		if err := runBackupRestore([]string{"--into", dest, corrupt}); err == nil {
			t.Error("expected error for corrupt archive")
		}
		if _, err := os.Stat(filepath.Join(dest, "snapshot.mmap")); !os.IsNotExist(err) {
			t.Errorf("corrupt restore wrote into target (err=%v)", err)
		}
	})

	t.Run("snapshot-mode mismatch is refused", func(t *testing.T) {
		archive := writeTestArchiveJSON(t) // json-mode snapshot vs default mmap restore -> mismatch
		t.Setenv("GRAPHDB_STORAGE_MODE", "mmap")
		dest := filepath.Join(t.TempDir(), "data")
		err := runBackupRestore([]string{"--into", dest, archive})
		if err == nil {
			t.Fatal("expected mode-mismatch error")
		}
	})

	t.Run("non-empty target needs --force", func(t *testing.T) {
		archive := writeTestArchive(t)
		dest := t.TempDir() // exists and we drop a file in it
		if err := os.WriteFile(filepath.Join(dest, "existing"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := runBackupRestore([]string{"--into", dest, archive}); err == nil {
			t.Error("expected error restoring into a non-empty dir without --force")
		}
		if err := runBackupRestore([]string{"--into", dest, "--force", archive}); err != nil {
			t.Errorf("--force restore: %v", err)
		}
	})
}
