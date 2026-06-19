package api

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// backupManifest is the provenance record written as the archive's first entry.
type backupManifest struct {
	GraphdbVersion string   `json:"graphdb_version"`
	CreatedAtUTC   string   `json:"created_at_utc"`
	SnapshotMode   string   `json:"snapshot_mode"` // "json" | "mmap" | "none"
	Files          []string `json:"files"`
}

// backupEntries lists the files (relative to dataDir) that constitute a
// restorable backup: the snapshot file plus the wal/, auth/, lsa/, and
// edgestore/ trees. Transient *.tmp files are skipped.
func backupEntries(dataDir string) (files []string, snapshotMode string, err error) {
	switch {
	case fileExistsForBackup(filepath.Join(dataDir, "snapshot.mmap")):
		files, snapshotMode = append(files, "snapshot.mmap"), "mmap"
	case fileExistsForBackup(filepath.Join(dataDir, "snapshot.json")):
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

func fileExistsForBackup(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// writeBackupArchive streams a gzip+tar archive of the store's dataDir to w:
// a manifest.json first, then each backup file with its dataDir-relative path.
func writeBackupArchive(w io.Writer, dataDir, version string) error {
	entries, mode, err := backupEntries(dataDir)
	if err != nil {
		return fmt.Errorf("enumerate backup files: %w", err)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	man := backupManifest{
		GraphdbVersion: version,
		CreatedAtUTC:   time.Now().UTC().Format(time.RFC3339),
		SnapshotMode:   mode,
		Files:          entries,
	}
	manBytes, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := writeTarBytes(tw, "manifest.json", manBytes); err != nil {
		return err
	}
	for _, rel := range entries {
		if err := writeTarFromFile(tw, dataDir, rel); err != nil {
			return err
		}
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

func writeTarFromFile(tw *tar.Writer, dataDir, rel string) error {
	full := filepath.Join(dataDir, filepath.FromSlash(rel))
	f, err := os.Open(full)
	if err != nil {
		return fmt.Errorf("open %s: %w", rel, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}
