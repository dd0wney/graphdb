package backup_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/backup"
	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestExtract_RestoresFilesNotManifest(t *testing.T) {
	srcDir := t.TempDir()
	gs, err := storage.NewGraphStorage(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
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

	dest := t.TempDir()
	if err := backup.Extract(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// The snapshot file must be present; the manifest is metadata and must not
	// be written into the data directory.
	if _, err := os.Stat(filepath.Join(dest, "snapshot.mmap")); err != nil {
		t.Errorf("snapshot.mmap not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, backup.ManifestName)); !os.IsNotExist(err) {
		t.Errorf("manifest should not be extracted into dataDir (err=%v)", err)
	}

	// The extracted store must reopen with the original data.
	restored, err := storage.NewGraphStorage(dest)
	if err != nil {
		t.Fatalf("reopen extracted: %v", err)
	}
	defer restored.Close()
	if c := restored.CountNodesForTenant("t1"); c != 6 {
		t.Errorf("restored nodes = %d, want 6", c)
	}
}

func TestExtract_RejectsPathTraversal(t *testing.T) {
	// Craft a malicious archive whose member escapes the destination dir.
	evil := buildArchive(t,
		[]struct{ name, body string }{{"../escaped.txt", "pwned"}},
		manifestFor([]struct{ name, body string }{{"../escaped.txt", "pwned"}}),
	)
	dest := t.TempDir()
	err := backup.Extract(bytes.NewReader(evil), dest)
	if err == nil {
		t.Fatal("expected Extract to reject a path-traversal entry")
	}
	parent := filepath.Dir(dest)
	if _, statErr := os.Stat(filepath.Join(parent, "escaped.txt")); !os.IsNotExist(statErr) {
		t.Errorf("path-traversal wrote outside dest (err=%v)", statErr)
	}
}
