package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/backup"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// writeTestArchive builds a real backup archive on disk and returns its path.
func writeTestArchive(t *testing.T) string {
	t.Helper()
	srcDir := t.TempDir()
	gs, err := storage.NewGraphStorage(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := gs.CreateNodeWithTenant("t1", []string{"Person"},
			map[string]storage.Value{"name": storage.StringValue("n")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := gs.Snapshot(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := backup.WriteArchive(&buf, srcDir, "test-version"); err != nil {
		t.Fatal(err)
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunBackupVerify(t *testing.T) {
	good := writeTestArchive(t)

	if err := runBackupVerify([]string{good}); err != nil {
		t.Errorf("verify of good archive: unexpected error %v", err)
	}

	if err := runBackupVerify(nil); err == nil {
		t.Error("expected error when no archive path is given")
	}

	if err := runBackupVerify([]string{filepath.Join(t.TempDir(), "nope.tar.gz")}); err == nil {
		t.Error("expected error for a missing archive file")
	}

	// Corrupt the archive: flip bytes in the middle of the gzip stream.
	corrupt := filepath.Join(t.TempDir(), "corrupt.tar.gz")
	raw, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(raw) / 3; i < len(raw)/3+16 && i < len(raw); i++ {
		raw[i] ^= 0xff
	}
	if err := os.WriteFile(corrupt, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runBackupVerify([]string{corrupt}); err == nil {
		t.Error("expected error for a corrupted archive")
	}
}
