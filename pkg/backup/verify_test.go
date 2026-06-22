package backup_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/backup"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// buildArchive crafts a gzip+tar archive from the given members (written in
// order) followed by an optional manifest trailer, so each failure mode can be
// constructed precisely rather than by mutating bytes.
func buildArchive(t *testing.T, members []struct{ name, body string }, man *backup.Manifest) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name string, b []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(b))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range members {
		write(m.name, []byte(m.body))
	}
	if man != nil {
		mb, err := json.Marshal(man)
		if err != nil {
			t.Fatal(err)
		}
		write(backup.ManifestName, mb)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func manifestFor(members []struct{ name, body string }) *backup.Manifest {
	m := &backup.Manifest{ManifestVersion: backup.ManifestVersion, SnapshotMode: "json"}
	for _, mem := range members {
		m.Files = append(m.Files, backup.File{Path: mem.name, SizeBytes: int64(len(mem.body)), Sha256: sum(mem.body)})
	}
	return m
}

// TestVerify_RealArchive confirms an archive produced by WriteArchive (manifest
// emitted as the trailer) passes Verify — the producer/verifier contract.
func TestVerify_RealArchive(t *testing.T) {
	srcDir := t.TempDir()
	gs, err := storage.NewGraphStorage(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
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
	man, err := backup.Verify(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Verify of real archive failed: %v", err)
	}
	if man.GraphdbVersion != "test-version" || len(man.Files) == 0 {
		t.Errorf("unexpected manifest: %+v", man)
	}
}

func TestVerify(t *testing.T) {
	good := []struct{ name, body string }{
		{"snapshot.json", `{"nodes":{}}`},
		{"wal/000001.wal", "walwalwal"},
	}

	tests := []struct {
		name      string
		archive   []byte
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "valid archive verifies",
			archive: buildArchive(t, good, manifestFor(good)),
			wantErr: false,
		},
		{
			name: "tampered file content fails on its path",
			archive: func() []byte {
				man := manifestFor(good)
				tampered := []struct{ name, body string }{good[0], {"wal/000001.wal", "TAMPERED!!"}}
				return buildArchive(t, tampered, man)
			}(),
			wantErr:   true,
			errSubstr: "wal/000001.wal",
		},
		{
			name: "file listed in manifest but absent from archive",
			archive: func() []byte {
				man := manifestFor(good)
				return buildArchive(t, good[:1], man) // drop the wal member
			}(),
			wantErr:   true,
			errSubstr: "wal/000001.wal",
		},
		{
			name: "file present in archive but absent from manifest",
			archive: func() []byte {
				man := manifestFor(good[:1]) // manifest knows only the snapshot
				return buildArchive(t, good, man)
			}(),
			wantErr:   true,
			errSubstr: "wal/000001.wal",
		},
		{
			name: "unknown manifest version is refused",
			archive: func() []byte {
				man := manifestFor(good)
				man.ManifestVersion = backup.ManifestVersion + 99
				return buildArchive(t, good, man)
			}(),
			wantErr:   true,
			errSubstr: "version",
		},
		{
			name:      "missing manifest is an error",
			archive:   buildArchive(t, good, nil),
			wantErr:   true,
			errSubstr: "manifest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			man, err := backup.Verify(bytes.NewReader(tt.archive))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (manifest=%+v)", man)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not mention %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if man == nil || len(man.Files) != len(good) {
				t.Errorf("manifest not returned correctly: %+v", man)
			}
		})
	}
}
