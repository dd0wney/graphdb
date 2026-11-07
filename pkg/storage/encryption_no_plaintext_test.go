package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// An encrypted store must write none of the caller's names or values to
// disk in a form a reader of the raw files can recover. This test writes a
// random marker into every place a caller can put one, then searches every
// file under the data directory for each marker in raw and base64 form:
// JSON encodes a []byte as base64, so a raw search alone passes on a
// plaintext snapshot (v1.4-encryption-envelope-test-cannot-fail).
//
// The same writes on an unencrypted store are the positive control. Each
// marker must be found there, or the search is blind to that marker and its
// absence from the encrypted store proves nothing.

// noPlaintextMarkers holds one random marker per kind of caller-supplied
// name or value. Each is 24 hex characters: a multiple of 3 bytes, so its
// base64 form has no padding and appears unchanged inside the base64 of a
// value that is exactly the marker.
type noPlaintextMarkers map[string]string

func newNoPlaintextMarkers(t *testing.T) noPlaintextMarkers {
	t.Helper()
	m := noPlaintextMarkers{}
	for _, kind := range []string{"label", "propertyKey", "propertyValue", "edgeType", "edgePropertyKey", "edgePropertyValue", "ruleName"} {
		raw := make([]byte, 12)
		if _, err := rand.Read(raw); err != nil {
			t.Fatalf("random marker: %v", err)
		}
		// A leading letter keeps every marker a valid label, key and rule name.
		m[kind] = "m" + hex.EncodeToString(raw)[1:]
	}
	return m
}

// writeNoPlaintextMarkers puts every marker into the store: a node, an edge,
// and a uniqueness rule (rules.json).
func writeNoPlaintextMarkers(t *testing.T, gs *GraphStorage, m noPlaintextMarkers) {
	t.Helper()
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: m["ruleName"], Label: m["label"], PropertyKey: m["propertyKey"]}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
	from, err := gs.CreateNode([]string{m["label"]}, map[string]Value{m["propertyKey"]: StringValue(m["propertyValue"])})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	to, err := gs.CreateNode([]string{"Other"}, nil)
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if _, err := gs.CreateEdge(from.ID, to.ID, m["edgeType"], map[string]Value{m["edgePropertyKey"]: StringValue(m["edgePropertyValue"])}, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
}

// markerHit is one marker found in one file.
type markerHit struct {
	file, kind, form string
}

// findNoPlaintextMarkers walks every regular file under dir and reports each
// marker it finds, raw or base64, with the file's path relative to dir.
func findNoPlaintextMarkers(t *testing.T, dir string, m noPlaintextMarkers) []markerHit {
	t.Helper()
	var hits []markerHit
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		for kind, marker := range m {
			forms := map[string]string{"raw": marker, "base64": base64.StdEncoding.EncodeToString([]byte(marker))}
			for form, needle := range forms {
				if bytes.Contains(data, []byte(needle)) {
					hits = append(hits, markerHit{file: rel, kind: kind, form: form})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return hits
}

// plaintextByDesign names the files an encrypted store may leave in
// plaintext, and which markers each may hold. Every entry is a decision,
// not a convenience: it names the task or document that owns it.
var plaintextByDesign = map[string]map[string]string{
	// The registry is plaintext until v1.4-encrypt-uniqueness-rules-file,
	// which removes this entry and must see this test go red without it.
	uniquenessRulesFileName: {
		"ruleName":    "v1.4-encrypt-uniqueness-rules-file",
		"label":       "v1.4-encrypt-uniqueness-rules-file",
		"propertyKey": "v1.4-encrypt-uniqueness-rules-file",
	},
}

// assertMarkerNodeRecovers reopens the directory the search just read and
// finds the marker node. Without it, a store that wrote nothing would pass
// the search; with it, the files searched are known to carry the data.
func assertMarkerNodeRecovers(t *testing.T, cfg StorageConfig, closed bool, m noPlaintextMarkers) {
	t.Helper()
	open := NewGraphStorageWithConfig
	if !closed {
		open = reopenAfterSimulatedCrash
	}
	gs, err := open(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs.Close() }()

	nodes, err := gs.FindNodesByLabelAcrossTenants(m["label"])
	if err != nil {
		t.Fatalf("FindNodesByLabelAcrossTenants: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("reopen finds %d nodes with the label marker, want 1", len(nodes))
	}
	if got := string(nodes[0].Properties[m["propertyKey"]].Data); got != m["propertyValue"] {
		t.Fatalf("reopened marker node property = %q, want %q", got, m["propertyValue"])
	}
}

func TestEncryptedStore_WritesNoPlaintext(t *testing.T) {
	configs := []struct {
		name string
		cfg  func(dir string) StorageConfig
	}{
		// The default asks for mmap; encryption makes it fall back to JSON.
		{"default", DefaultStorageConfig},
		{"json", jsonConfig},
		{"json-batching-compression", func(dir string) StorageConfig {
			c := jsonConfig(dir)
			c.EnableBatching = true
			c.EnableCompression = true
			return c
		}},
		{"json-disk-backed-edges", func(dir string) StorageConfig {
			c := jsonConfig(dir)
			c.UseDiskBackedEdges = true
			return c
		}},
	}
	ends := []struct {
		name  string
		close bool
	}{
		{"closed", true},
		// The store stays open, as after a crash: only what reached the WAL
		// and the registry is on disk.
		{"crash", false},
	}

	for _, c := range configs {
		for _, end := range ends {
			t.Run(c.name+"/"+end.name, func(t *testing.T) {
				m := newNoPlaintextMarkers(t)

				run := func(encrypted bool) []markerHit {
					dir := t.TempDir()
					cfg := c.cfg(dir)
					if encrypted {
						cfg.EncryptionEngine = testEncryptionEngine(t)
						cfg.KeyManager = &mockKeyProvider{}
					}
					gs, err := NewGraphStorageWithConfig(cfg)
					if err != nil {
						t.Fatalf("open: %v", err)
					}
					writeNoPlaintextMarkers(t, gs, m)
					if end.close {
						if err := gs.Close(); err != nil {
							t.Fatalf("Close: %v", err)
						}
					} else {
						t.Cleanup(func() { _ = gs.Close() })
					}
					hits := findNoPlaintextMarkers(t, dir, m)
					assertMarkerNodeRecovers(t, cfg, end.close, m)
					return hits
				}

				// Positive control: on a plaintext store the search must find
				// every marker somewhere.
				found := map[string]bool{}
				for _, h := range run(false) {
					found[h.kind] = true
				}
				for kind := range m {
					if !found[kind] {
						t.Errorf("control: the plaintext store holds no %s marker on disk, so the search cannot see it", kind)
					}
				}

				for _, h := range run(true) {
					if owner, ok := plaintextByDesign[filepath.Base(h.file)][h.kind]; ok {
						t.Logf("plaintext by design (%s): %s marker in %s (%s)", owner, h.kind, h.file, h.form)
						continue
					}
					t.Errorf("encrypted store leaks the %s marker in %s (%s form)", h.kind, h.file, h.form)
				}
			})
		}
	}
}
