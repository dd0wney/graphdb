package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestWriteBackupArchive_RoundTrip(t *testing.T) {
	srcDir := t.TempDir()
	gs, err := storage.NewGraphStorage(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint64
	for i := 0; i < 12; i++ {
		n, err := gs.CreateNodeWithTenant("t1", []string{"Person"},
			map[string]storage.Value{"name": storage.StringValue("n")})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	for i := 0; i+1 < len(ids); i++ {
		if _, err := gs.CreateEdgeWithTenant("t1", ids[i], ids[i+1], "NEXT", nil, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := gs.Snapshot(); err != nil { // mirror what the handler does before archiving
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := writeBackupArchive(&buf, srcDir, "test-version"); err != nil {
		t.Fatalf("writeBackupArchive: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	// Extract the archive into a fresh dir and reopen.
	restoreDir := t.TempDir()
	var sawManifest, sawSnapshot bool
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "manifest.json" {
			sawManifest = true
			var m backupManifest
			b, rerr := io.ReadAll(tr)
			if rerr != nil {
				t.Fatalf("read manifest: %v", rerr)
			}
			if jerr := json.Unmarshal(b, &m); jerr != nil {
				t.Fatalf("manifest: %v", jerr)
			}
			if m.GraphdbVersion != "test-version" || m.SnapshotMode != "json" || len(m.Files) == 0 {
				t.Errorf("bad manifest: %+v", m)
			}
			continue
		}
		if hdr.Name == "snapshot.json" {
			sawSnapshot = true
		}
		dst := filepath.Join(restoreDir, hdr.Name)
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
		f, ferr := os.Create(dst)
		if ferr != nil {
			t.Fatal(ferr)
		}
		if _, cerr := io.Copy(f, tr); cerr != nil {
			t.Fatal(cerr)
		}
		if cerr := f.Close(); cerr != nil {
			t.Fatal(cerr)
		}
	}
	if !sawManifest || !sawSnapshot {
		t.Fatalf("archive missing entries (manifest=%v snapshot=%v)", sawManifest, sawSnapshot)
	}

	restored, err := storage.NewGraphStorage(restoreDir)
	if err != nil {
		t.Fatalf("reopen restored: %v", err)
	}
	defer restored.Close()
	if c := restored.CountNodesForTenant("t1"); c != 12 {
		t.Errorf("restored nodes = %d, want 12", c)
	}
	if c := restored.CountEdgesForTenant("t1"); c != 11 {
		t.Errorf("restored edges = %d, want 11", c)
	}
}
