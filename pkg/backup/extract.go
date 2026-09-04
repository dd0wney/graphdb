package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Extract unpacks a backup archive into destDir, on the default filesystem
// driver. See ExtractWithFS.
func Extract(r io.Reader, destDir string) error {
	return ExtractWithFS(vfs.Default(), r, destDir)
}

// ExtractWithFS unpacks a backup archive into destDir, through fsys,
// reconstructing the store's dataDir layout. The manifest.json metadata
// entry is skipped — only the store's own files are written. destDir is
// created if absent.
//
// Each entry's destination is constrained to destDir: an entry whose path
// escapes the directory (a "zip-slip" traversal) aborts the extraction with
// an error. Callers should Verify the archive first; ExtractWithFS performs
// no integrity checking of its own.
//
// The driver exists so a test can observe the write, and so a fault or crash
// simulator can reach this path at all — the same reason WriteArchiveWithFS
// has one. Before it, Extract called the os package directly: no driver saw
// a restore ever happen.
func ExtractWithFS(fsys vfs.FileSystem, r io.Reader, destDir string) error {
	if err := fsys.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open archive (gzip): %w", err)
	}
	defer func() { _ = gz.Close() }() //nolint:errcheck // read-only decompressor, nothing buffered to lose

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive (tar): %w", err)
		}
		if hdr.Name == ManifestName {
			continue
		}
		dst, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		if err := fsys.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create dir for %s: %w", hdr.Name, err)
		}
		if err := writeFileWithFS(fsys, dst, tr, hdr.Size); err != nil {
			return fmt.Errorf("write %s: %w", hdr.Name, err)
		}
	}
	return nil
}

// safeJoin resolves name (a slash-separated archive path) under destDir and
// rejects any result that escapes destDir.
func safeJoin(destDir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path %q (escapes destination)", name)
	}
	dst := filepath.Join(destDir, clean)
	rel, err := filepath.Rel(destDir, dst)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path %q (escapes destination)", name)
	}
	return dst, nil
}

func writeFileWithFS(fsys vfs.FileSystem, dst string, r io.Reader, size int64) error {
	f, err := fsys.Open(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	// Bound the write to the entry's declared size (tar enforces it) so a
	// crafted archive can't stream an unbounded decompression bomb to disk.
	if _, err := io.CopyN(f, r, size); err != nil {
		// Discarded on purpose: the copy already failed, and a second error
		// from cleaning up the partially-written file would replace the one
		// that says what actually went wrong.
		//
		// Explicit because f is now a vfs.File and not an *os.File directly —
		// errcheck's exclusion for Close is keyed to the concrete os
		// function, not the interface method. See the same note in
		// writeTarFromFileWithFS (archive.go) and
		// pkg/search/lsa_persistence.go's SaveToFileWithFS.
		_ = f.Close() //nolint:errcheck // write already failed; see the paragraph above
		return err
	}
	return f.Close()
}
