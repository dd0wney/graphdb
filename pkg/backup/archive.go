// Package backup builds and verifies graphdb store backup archives.
//
// An archive is a gzip+tar of a store's dataDir: the snapshot file plus the
// wal/, auth/, lsa/, and edgestore/ trees, with a manifest trailer recording
// per-file size + SHA-256 for integrity. The package has no dependency on the
// HTTP server (pkg/api) or storage, so offline tooling (the graphdb-admin CLI)
// can build, verify, and restore archives without opening a graph.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ManifestVersion is the schema version of the manifest envelope. Bump when
// the manifest shape changes incompatibly; verify/restore tooling refuses
// versions it does not understand.
const ManifestVersion = 1

// ManifestName is the archive member holding the manifest.
const ManifestName = "manifest.json"

// File records one archived member with the integrity data needed to detect a
// corrupt or truncated archive before it is restored over live data.
type File struct {
	Path      string `json:"path"`       // path relative to dataDir, slash-separated
	SizeBytes int64  `json:"size_bytes"` // bytes written into the archive
	Sha256    string `json:"sha256"`     // hex SHA-256 of exactly those bytes
}

// Manifest is the provenance + integrity record. It is written as the
// archive's LAST entry (a trailer) so each file's recorded hash describes
// exactly the bytes streamed into the tar — immune to a WAL segment being
// appended between enumeration and streaming on a live backup.
type Manifest struct {
	ManifestVersion int    `json:"manifest_version"`
	GraphdbVersion  string `json:"graphdb_version"`
	CreatedAtUTC    string `json:"created_at_utc"`
	SnapshotMode    string `json:"snapshot_mode"` // "json" | "mmap" | "none"
	Files           []File `json:"files"`
}

// entries lists the files (relative to dataDir) that constitute a restorable
// backup: the snapshot file plus the wal/, auth/, lsa/, and edgestore/ trees.
// Transient *.tmp files are skipped.
func entries(dataDir string) (files []string, snapshotMode string, err error) {
	switch {
	case fileExists(filepath.Join(dataDir, "snapshot.mmap")):
		files, snapshotMode = append(files, "snapshot.mmap"), "mmap"
	case fileExists(filepath.Join(dataDir, "snapshot.json")):
		files, snapshotMode = append(files, "snapshot.json"), "json"
	default:
		snapshotMode = "none"
	}
	for _, dir := range []string{"wal", "auth", "lsa", "edgestore"} {
		full := filepath.Join(dataDir, dir)
		info, statErr := os.Stat(full)
		if statErr != nil || !info.IsDir() {
			continue
		}
		walkErr := filepath.Walk(full, func(p string, fi os.FileInfo, e error) error {
			if e != nil {
				return e
			}
			if fi.IsDir() || strings.HasSuffix(p, ".tmp") {
				return nil
			}
			rel, relErr := filepath.Rel(dataDir, p)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if walkErr != nil {
			return nil, "", walkErr
		}
	}
	return files, snapshotMode, nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// WriteArchive streams a gzip+tar archive of the store's dataDir to w: each
// backup file with its dataDir-relative path, then a manifest.json trailer.
func WriteArchive(w io.Writer, dataDir, version string) error {
	files, mode, err := entries(dataDir)
	if err != nil {
		return fmt.Errorf("enumerate backup files: %w", err)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	// Stream each file first, hashing the exact bytes archived, then emit the
	// manifest trailer describing them.
	manifestFiles := make([]File, 0, len(files))
	for _, rel := range files {
		bf, err := writeTarFromFile(tw, dataDir, rel)
		if err != nil {
			return err
		}
		manifestFiles = append(manifestFiles, bf)
	}

	man := Manifest{
		ManifestVersion: ManifestVersion,
		GraphdbVersion:  version,
		CreatedAtUTC:    time.Now().UTC().Format(time.RFC3339),
		SnapshotMode:    mode,
		Files:           manifestFiles,
	}
	manBytes, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := writeTarBytes(tw, ManifestName, manBytes); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeTarBytes(tw *tar.Writer, name string, b []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(b))}); err != nil {
		return err
	}
	_, err := tw.Write(b)
	return err
}

// writeTarFromFile copies one file into the tar, computing its SHA-256 over
// exactly the bytes archived. It copies precisely info.Size() bytes so a file
// that grows after Stat (an actively-appended WAL segment) can neither corrupt
// the tar (header size mismatch) nor desync the recorded hash from the
// archived content.
func writeTarFromFile(tw *tar.Writer, dataDir, rel string) (File, error) {
	full := filepath.Join(dataDir, filepath.FromSlash(rel))
	f, err := os.Open(full)
	if err != nil {
		return File{}, fmt.Errorf("open %s: %w", rel, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return File{}, err
	}
	size := info.Size()
	if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o600, Size: size, ModTime: info.ModTime()}); err != nil {
		return File{}, err
	}
	h := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(tw, h), f, size); err != nil {
		return File{}, fmt.Errorf("archive %s: %w", rel, err)
	}
	return File{Path: rel, SizeBytes: size, Sha256: hex.EncodeToString(h.Sum(nil))}, nil
}
