package backup_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/backup"
	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestWriteArchive_RoundTrip(t *testing.T) {
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
	if err := backup.WriteArchive(&buf, srcDir, "test-version"); err != nil {
		t.Fatalf("WriteArchive: %v", err)
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
		if hdr.Name == backup.ManifestName {
			sawManifest = true
			var m backup.Manifest
			b, rerr := io.ReadAll(tr)
			if rerr != nil {
				t.Fatalf("read manifest: %v", rerr)
			}
			if jerr := json.Unmarshal(b, &m); jerr != nil {
				t.Fatalf("manifest: %v", jerr)
			}
			if m.ManifestVersion != 1 || m.GraphdbVersion != "test-version" || m.SnapshotMode != "json" || len(m.Files) == 0 {
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

// TestWriteArchive_PerFileIntegrity asserts the manifest records a versioned
// envelope plus a size + SHA-256 for every archived file that matches the
// actual bytes streamed into the tar. This is the integrity contract the
// offline verify path depends on.
func TestWriteArchive_PerFileIntegrity(t *testing.T) {
	srcDir := t.TempDir()
	gs, err := storage.NewGraphStorage(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
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
		t.Fatalf("WriteArchive: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	// Read the archive once: hash every non-manifest member, capture the manifest.
	var man backup.Manifest
	gotHash := map[string]string{}
	gotSize := map[string]int64{}
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
		b, rerr := io.ReadAll(tr)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if hdr.Name == backup.ManifestName {
			if jerr := json.Unmarshal(b, &man); jerr != nil {
				t.Fatalf("manifest: %v", jerr)
			}
			continue
		}
		sum := sha256.Sum256(b)
		gotHash[hdr.Name] = hex.EncodeToString(sum[:])
		gotSize[hdr.Name] = int64(len(b))
	}

	if man.ManifestVersion != 1 {
		t.Errorf("manifest_version = %d, want 1", man.ManifestVersion)
	}
	if len(man.Files) == 0 {
		t.Fatal("manifest lists no files")
	}
	for _, f := range man.Files {
		if f.Path == "" || f.Sha256 == "" {
			t.Errorf("incomplete manifest entry: %+v", f)
			continue
		}
		if gotHash[f.Path] != f.Sha256 {
			t.Errorf("%s: manifest sha256 %s != archived %s", f.Path, f.Sha256, gotHash[f.Path])
		}
		if gotSize[f.Path] != f.SizeBytes {
			t.Errorf("%s: manifest size %d != archived %d", f.Path, f.SizeBytes, gotSize[f.Path])
		}
	}
}
