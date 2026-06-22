package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Extract unpacks a backup archive into destDir, reconstructing the store's
// dataDir layout. The manifest.json metadata entry is skipped — only the
// store's own files are written. destDir is created if absent.
//
// Each entry's destination is constrained to destDir: an entry whose path
// escapes the directory (a "zip-slip" traversal) aborts the extraction with an
// error. Callers should Verify the archive first; Extract performs no integrity
// checking of its own.
func Extract(r io.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open archive (gzip): %w", err)
	}
	defer gz.Close()

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
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create dir for %s: %w", hdr.Name, err)
		}
		if err := writeFile(dst, tr); err != nil {
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

func writeFile(dst string, r io.Reader) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
