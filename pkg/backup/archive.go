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
	"sort"
	"strings"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
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

// walkFilesWithFS visits every regular file at or under root, through fsys,
// in the same order os/filepath.Walk would: entries within one directory
// sorted lexically by name, subdirectories recursed depth-first before the
// next sibling.
//
// The sort is not optional. vfs.FileSystem.ReadDir's doc comment (pkg/vfs/vfs.go)
// makes no promise about the order it returns entries in — that is an
// os.ReadDir behaviour, not an interface contract — so a driver is free to
// return them in any order, including a different one on every call. Trusting
// that order would still enumerate the same files; it would just be free to
// reorder them between two runs, and a reordering changes which file lands
// where in the archive's tar stream for the same input. A consumer that
// compares two archives, or checks a manifest, would see that as corruption.
//
// visit is called once per regular file with its slash-independent
// filepath.Join'd path (native separators; callers normalise with
// filepath.ToSlash as needed). Directories and entries whose name ends in
// ".tmp" are never passed to visit — directories are still recursed into,
// matching filepath.Walk's behaviour of visiting a *.tmp-suffixed directory's
// contents even though it wouldn't be recorded as an archived member itself.
func walkFilesWithFS(fsys vfs.FileSystem, root string, visit func(path string) error) error {
	entries, err := fsys.ReadDir(root)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() {
			if err := walkFilesWithFS(fsys, p, visit); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		if err := visit(p); err != nil {
			return err
		}
	}
	return nil
}

// entriesWithFS lists the files (relative to dataDir) that constitute a
// restorable backup: the snapshot file plus the wal/, auth/, lsa/, and
// edgestore/ trees. Transient *.tmp files are skipped.
func entriesWithFS(fsys vfs.FileSystem, dataDir string) (files []string, snapshotMode string, err error) {
	switch {
	case fileExistsWithFS(fsys, filepath.Join(dataDir, "snapshot.mmap")):
		files, snapshotMode = append(files, "snapshot.mmap"), "mmap"
	case fileExistsWithFS(fsys, filepath.Join(dataDir, "snapshot.json")):
		files, snapshotMode = append(files, "snapshot.json"), "json"
	default:
		snapshotMode = "none"
	}
	for _, dir := range []string{"wal", "auth", "lsa", "edgestore"} {
		full := filepath.Join(dataDir, dir)
		info, statErr := fsys.Stat(full)
		if statErr != nil || !info.IsDir() {
			continue
		}
		walkErr := walkFilesWithFS(fsys, full, func(p string) error {
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

func fileExistsWithFS(fsys vfs.FileSystem, p string) bool {
	info, err := fsys.Stat(p)
	return err == nil && !info.IsDir()
}

// WriteArchive streams a gzip+tar archive of the store's dataDir to w, on the
// default filesystem driver. See WriteArchiveWithFS.
func WriteArchive(w io.Writer, dataDir, version string) error {
	return WriteArchiveWithFS(vfs.Default(), w, dataDir, version)
}

// WriteArchiveWithFS streams a gzip+tar archive of the store's dataDir to w,
// through fsys: each backup file with its dataDir-relative path, then a
// manifest.json trailer.
//
// The driver exists so a test can observe the read, and so a fault or crash
// simulator can reach this path at all. Before it, WriteArchive called the os
// package (and filepath.Walk) directly: no driver saw a single one of its
// operations, so a crash sweep over graphdb ran, reported, and never observed
// this function. A path outside the seam produces no signal — "the sweep
// found nothing" and "the sweep never saw it" render identically.
func WriteArchiveWithFS(fsys vfs.FileSystem, w io.Writer, dataDir, version string) error {
	files, mode, err := entriesWithFS(fsys, dataDir)
	if err != nil {
		return fmt.Errorf("enumerate backup files: %w", err)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	// Stream each file first, hashing the exact bytes archived, then emit the
	// manifest trailer describing them.
	manifestFiles := make([]File, 0, len(files))
	for _, rel := range files {
		bf, err := writeTarFromFileWithFS(fsys, tw, dataDir, rel)
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

// writeTarFromFileWithFS copies one file into the tar, computing its SHA-256
// over exactly the bytes archived. It copies precisely info.Size() bytes so a
// file that grows after Stat (an actively-appended WAL segment) can neither
// corrupt the tar (header size mismatch) nor desync the recorded hash from
// the archived content.
func writeTarFromFileWithFS(fsys vfs.FileSystem, tw *tar.Writer, dataDir, rel string) (File, error) {
	full := filepath.Join(dataDir, filepath.FromSlash(rel))
	f, err := fsys.Open(full, os.O_RDONLY, 0)
	if err != nil {
		return File{}, fmt.Errorf("open %s: %w", rel, err)
	}
	// Discarded on purpose: this is a read-only handle, so there are no
	// buffered bytes for Close to lose, and the archived content is already
	// hashed and written into tw by the time this runs.
	//
	// The explicit discard (rather than a bare `defer f.Close()`) is required
	// now that f is a vfs.File and not an *os.File directly: errcheck's
	// exclusion for Close is keyed to the concrete os function, not to the
	// interface method, so a call that was invisible to the linter through
	// *os.File becomes a real finding the moment it goes through fsys. See
	// the same note in pkg/search/lsa_persistence.go's LoadLSAFromFileWithFS.
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only handle, nothing buffered to lose
	// vfs.File has no Stat method (the interface is deliberately the
	// intersection of what callers use, see pkg/vfs/vfs.go), so the size
	// comes from fsys.Stat by path rather than from the open handle —
	// pkg/lsm and pkg/btree read a file's size through their driver the same
	// way.
	info, err := fsys.Stat(full)
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
