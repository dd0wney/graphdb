package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Verify reads a backup archive stream and checks its integrity against the
// manifest trailer: every manifest file must be present with a matching size
// and SHA-256, and every archived file (other than the manifest) must be
// listed. It returns the parsed manifest on success, or an error naming the
// offending path(s). It does not write anything — safe to run against an
// archive before restoring it over live data.
func Verify(r io.Reader) (*Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open archive (gzip): %w", err)
	}
	defer gz.Close()

	type seen struct {
		size int64
		hash string
	}
	got := map[string]seen{}
	var man *Manifest

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive (tar): %w", err)
		}
		if hdr.Name == ManifestName {
			var m Manifest
			if derr := json.NewDecoder(tr).Decode(&m); derr != nil {
				return nil, fmt.Errorf("parse %s: %w", ManifestName, derr)
			}
			man = &m
			continue
		}
		h := sha256.New()
		// Bound the copy to the entry's declared size (which tar enforces) so we
		// never read unboundedly from the decompressor (gosec G110). A truncated
		// entry yields ErrUnexpectedEOF here and fails verification.
		n, err := io.CopyN(h, tr, hdr.Size)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		got[hdr.Name] = seen{size: n, hash: hex.EncodeToString(h.Sum(nil))}
	}

	if man == nil {
		return nil, fmt.Errorf("archive has no %s manifest", ManifestName)
	}
	if man.ManifestVersion != ManifestVersion {
		return nil, fmt.Errorf("unsupported manifest version %d (this build understands %d)",
			man.ManifestVersion, ManifestVersion)
	}

	var problems []string
	listed := map[string]bool{}
	for _, f := range man.Files {
		listed[f.Path] = true
		s, ok := got[f.Path]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: listed in manifest but absent from archive", f.Path))
			continue
		}
		if s.size != f.SizeBytes {
			problems = append(problems, fmt.Sprintf("%s: size %d != manifest %d", f.Path, s.size, f.SizeBytes))
		}
		if s.hash != f.Sha256 {
			problems = append(problems, fmt.Sprintf("%s: sha256 mismatch", f.Path))
		}
	}
	for name := range got {
		if !listed[name] {
			problems = append(problems, fmt.Sprintf("%s: present in archive but not listed in manifest", name))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("archive failed verification:\n  %s", strings.Join(problems, "\n  "))
	}
	return man, nil
}
