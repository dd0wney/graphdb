package search

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildTinyLSA builds a tiny but real LSA index suitable for round-trip
// tests. The corpus is small enough to fit in tests but large enough to
// produce a non-trivial vocab/BM25/SVD state worth round-tripping.
func buildTinyLSA(t *testing.T) *LSAIndex {
	t.Helper()
	docs := []Document{
		{ID: 1, Title: "Graph databases", Body: "Graph databases store nodes and edges. Edges connect nodes."},
		{ID: 2, Title: "Vector search", Body: "Vector search uses embeddings to find similar documents."},
		{ID: 3, Title: "Latent semantic analysis", Body: "Latent semantic analysis projects documents into a lower-dimensional space using singular value decomposition."},
		{ID: 4, Title: "BM25 scoring", Body: "BM25 is a term-frequency-based ranking function used in information retrieval."},
		{ID: 5, Title: "Hybrid retrieval", Body: "Hybrid retrieval combines lexical BM25 scores with semantic embedding similarity."},
	}
	cfg := DefaultLSAConfig()
	cfg.Dims = 4 // small enough that we don't need many docs
	cfg.MinDocFreq = 1
	idx, err := BuildLSAIndex(docs, cfg)
	if err != nil {
		t.Fatalf("BuildLSAIndex: %v", err)
	}
	return idx
}

// TestLSASnapshot_RoundTripInMemory pins that an LSAIndex serialized via
// WriteSnapshot and read back via ReadLSASnapshot is functionally
// equivalent to the original. "Functionally equivalent" is the load-
// bearing assertion — bit-equality on the float matrices is overspec
// (gob serialization rounds-trip exact for float32, but tests that
// rely on bitwise equality become brittle under harmless refactors).
// Instead, we assert that a query against the loaded index produces
// the same top-K as the original.
func TestLSASnapshot_RoundTripInMemory(t *testing.T) {
	orig := buildTinyLSA(t)

	var buf bytes.Buffer
	if err := orig.WriteSnapshot(&buf); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	loaded, err := ReadLSASnapshot(&buf)
	if err != nil {
		t.Fatalf("ReadLSASnapshot: %v", err)
	}

	// Structural equivalence.
	if loaded.Dimensions() != orig.Dimensions() {
		t.Errorf("dimensions: orig=%d loaded=%d", orig.Dimensions(), loaded.Dimensions())
	}
	if loaded.NumDocs() != orig.NumDocs() {
		t.Errorf("numdocs: orig=%d loaded=%d", orig.NumDocs(), loaded.NumDocs())
	}

	// Functional equivalence on the LSA path: FoldQuery produces the
	// same vector for the same input.
	origVec, _, err := orig.FoldQuery("graph database edges")
	if err != nil {
		t.Fatalf("orig FoldQuery: %v", err)
	}
	loadedVec, _, err := loaded.FoldQuery("graph database edges")
	if err != nil {
		t.Fatalf("loaded FoldQuery: %v", err)
	}
	if len(origVec) != len(loadedVec) {
		t.Fatalf("vector length: orig=%d loaded=%d", len(origVec), len(loadedVec))
	}
	for i := range origVec {
		if math.Abs(float64(origVec[i]-loadedVec[i])) > 1e-6 {
			t.Errorf("vector[%d] drift: orig=%v loaded=%v", i, origVec[i], loadedVec[i])
		}
	}

	// Functional equivalence on the BM25 path: same query yields same
	// scores. BM25 state survives serialization only if bm25Post,
	// bm25Dlen, bm25Avgdl all round-trip — assert via behavior, not
	// internal-field inspection.
	tokens := []string{"graph", "edges"}
	origScores := orig.BM25Score(tokens, nil)
	loadedScores := loaded.BM25Score(tokens, nil)
	if len(origScores) != len(loadedScores) {
		t.Fatalf("BM25 score count: orig=%d loaded=%d", len(origScores), len(loadedScores))
	}
	for id, want := range origScores {
		got := loadedScores[id]
		if math.Abs(want-got) > 1e-9 {
			t.Errorf("BM25 score for nodeID=%d: orig=%v loaded=%v", id, want, got)
		}
	}
}

// TestLSASnapshot_NodeIDMapReconstructed pins that the derived
// nodeIDMap (intentionally omitted from the on-disk format) is
// faithfully reconstructed during load. We can't observe nodeIDMap
// directly without exposing it, but DocVector(id) routes through it —
// a load that forgot the reconstruction step would return false on
// every lookup.
func TestLSASnapshot_NodeIDMapReconstructed(t *testing.T) {
	orig := buildTinyLSA(t)

	var buf bytes.Buffer
	if err := orig.WriteSnapshot(&buf); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	loaded, err := ReadLSASnapshot(&buf)
	if err != nil {
		t.Fatalf("ReadLSASnapshot: %v", err)
	}

	// Every original NodeID must resolve via the loaded index.
	for _, id := range []uint64{1, 2, 3, 4, 5} {
		origVec, origOk := orig.DocVector(id)
		loadedVec, loadedOk := loaded.DocVector(id)
		if origOk != loadedOk {
			t.Errorf("DocVector(%d) presence drift: orig=%v loaded=%v", id, origOk, loadedOk)
			continue
		}
		if !origOk {
			continue
		}
		if len(origVec) != len(loadedVec) {
			t.Errorf("DocVector(%d) length drift: orig=%d loaded=%d", id, len(origVec), len(loadedVec))
		}
	}
}

// TestLSASnapshot_RejectsBadMagic confirms a corrupt or unrelated file
// fails fast rather than producing a half-decoded index. Without the
// magic check, a gob-formatted file from an unrelated source would
// half-decode into nonsense fields.
func TestLSASnapshot_RejectsBadMagic(t *testing.T) {
	bad := []byte("WRONGmagic and gibberish")
	_, err := ReadLSASnapshot(bytes.NewReader(bad))
	if err == nil {
		t.Fatal("ReadLSASnapshot accepted file with bad magic")
	}
	if !strings.Contains(err.Error(), "not an LSA snapshot") {
		t.Errorf("error doesn't name the failure mode: %v", err)
	}
}

// TestLSASnapshot_RejectsVersionMismatch confirms a future-version file
// fails with a clear "regenerate" message rather than silently producing
// stale state. The message must mention the admin endpoint so operators
// have an actionable next step.
func TestLSASnapshot_RejectsVersionMismatch(t *testing.T) {
	// Construct a synthetic header with right magic but wrong version.
	var buf bytes.Buffer
	buf.Write(lsaSnapshotMagic[:])
	if err := binary.Write(&buf, binary.BigEndian, uint32(99999)); err != nil {
		t.Fatalf("synth header: %v", err)
	}
	// gob payload is irrelevant — version check fires first.

	_, err := ReadLSASnapshot(&buf)
	if err == nil {
		t.Fatal("ReadLSASnapshot accepted version-mismatched file")
	}
	if !strings.Contains(err.Error(), "version mismatch") {
		t.Errorf("error doesn't name the failure mode: %v", err)
	}
	if !strings.Contains(err.Error(), "admin endpoint") {
		t.Errorf("error doesn't tell operator what to do: %v", err)
	}
}

// TestLSASnapshot_SaveLoadFile pins the atomic-rename file path. The
// invariant under test: an interrupted save leaves no half-written file
// at the target path. We can't easily inject mid-write failure here
// without monkey-patching os.Rename, so the test confirms the happy path
// produces a clean file and a no-op-on-disk for SaveToFile errors via
// the missing-dir behavior (MkdirAll covers it).
func TestLSASnapshot_SaveLoadFile(t *testing.T) {
	tmpDir := t.TempDir()
	orig := buildTinyLSA(t)

	path := filepath.Join(tmpDir, "deep", "nested", "test.lsa")
	if err := orig.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}

	// File exists and no .tmp leftover.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("snapshot file missing after SaveToFile: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".tmp file leaked: stat err=%v (want ErrNotExist)", err)
	}

	// Load round-trips structurally.
	loaded, err := LoadLSAFromFile(path)
	if err != nil {
		t.Fatalf("LoadLSAFromFile: %v", err)
	}
	if loaded.NumDocs() != orig.NumDocs() || loaded.Dimensions() != orig.Dimensions() {
		t.Errorf("structural drift: orig=(%d docs, %d dims) loaded=(%d docs, %d dims)",
			orig.NumDocs(), orig.Dimensions(), loaded.NumDocs(), loaded.Dimensions())
	}
}

// TestLSASnapshot_LoadFromMissingFile confirms the missing-file case
// returns a wrapped os.ErrNotExist so callers can distinguish "first
// boot, no snapshot yet" from "snapshot file is corrupt" via
// errors.Is. The TenantLSAIndexes.LoadAll path relies on this to skip
// fresh deployments without spurious error logs.
func TestLSASnapshot_LoadFromMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := LoadLSAFromFile(filepath.Join(tmpDir, "absent.lsa"))
	if err == nil {
		t.Fatal("LoadLSAFromFile returned nil error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("LoadLSAFromFile missing file: want ErrNotExist, got %v", err)
	}
}

// TestTenantLSAIndexes_SaveAllLoadAll pins the per-tenant snapshot
// round-trip via the registry. Asserts that loading produces the same
// tenant set and that each tenant's index is functionally equivalent.
func TestTenantLSAIndexes_SaveAllLoadAll(t *testing.T) {
	tmpDir := t.TempDir()
	snapshotDir := filepath.Join(tmpDir, "lsa")

	src := NewTenantLSAIndexes()
	src.Set("tenant-A", buildTinyLSA(t))
	src.Set("tenant-B", buildTinyLSA(t))

	if err := src.SaveAll(snapshotDir); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Confirm two files written.
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 snapshot files, got %d", len(entries))
	}

	dst := NewTenantLSAIndexes()
	if err := dst.LoadAll(snapshotDir); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	srcTenants := src.Tenants()
	dstTenants := dst.Tenants()
	if len(srcTenants) != len(dstTenants) {
		t.Errorf("tenant count: src=%d dst=%d", len(srcTenants), len(dstTenants))
	}
	for _, tid := range srcTenants {
		if dst.Get(tid) == nil {
			t.Errorf("tenant %q missing after LoadAll", tid)
		}
	}
}

// TestTenantLSAIndexes_LoadAllMissingDir confirms that loading from a
// directory that doesn't exist returns nil (treat as "no snapshots yet")
// rather than an error. This is what enables fresh deployments to boot
// without intervention.
func TestTenantLSAIndexes_LoadAllMissingDir(t *testing.T) {
	dst := NewTenantLSAIndexes()
	if err := dst.LoadAll(filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Errorf("LoadAll missing dir: want nil, got %v", err)
	}
	if len(dst.Tenants()) != 0 {
		t.Errorf("tenants after LoadAll-on-missing-dir: want 0, got %d", len(dst.Tenants()))
	}
}

// TestSanitizeTenantForFilename pins the filesystem-safety contract.
// The rules are conservative-on-purpose: any tenant ID that wouldn't
// survive a fresh sanitize round-trip is refused, not normalized. This
// prevents the class of bug where "acme/west" and "acmewest" silently
// collide on disk.
func TestSanitizeTenantForFilename(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"plain tenant", "acme", false},
		{"with hyphen", "tenant-a", false},
		{"with underscore", "tenant_a", false},
		{"empty", "", true},
		{"dot", ".", true},
		{"double dot", "..", true},
		{"path separator", "acme/west", true},
		{"backslash separator", "acme\\west", true},
		{"null byte", "acme\x00west", true},
		{"path traversal in name", "../etc/passwd", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeTenantForFilename(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("input %q: want error, got %q", tt.input, got)
				}
			} else {
				if err != nil {
					t.Errorf("input %q: unexpected error %v", tt.input, err)
				}
				if got != tt.input {
					t.Errorf("input %q: sanitize changed value to %q (must round-trip exactly for safe IDs)", tt.input, got)
				}
			}
		})
	}
}

// TestTenantLSAIndexes_LoadAllRejectsUnsafeFilenames confirms that a
// hand-placed file with an unsafe stem (e.g. ../etc/passwd.lsa) is
// silently ignored rather than treated as a tenant. The behavior is
// defense-in-depth: SaveAll won't produce such a file, but an attacker
// or operator error could. The aggregate error names the rejection so
// operators see it in logs.
func TestTenantLSAIndexes_LoadAllRejectsUnsafeFilenames(t *testing.T) {
	tmpDir := t.TempDir()
	snapshotDir := filepath.Join(tmpDir, "lsa")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Plant one legit snapshot...
	legit := buildTinyLSA(t)
	if err := legit.SaveToFile(filepath.Join(snapshotDir, "tenant-legit.lsa")); err != nil {
		t.Fatalf("legit SaveToFile: %v", err)
	}

	// ...and one file whose stem fails sanitize. Note: we can't create
	// "../etc/passwd.lsa" as a literal file in a tempdir, so we test the
	// inner filter directly via a file with a stem containing ".."
	// (".._etc_passwd.lsa" would round-trip clean — we need the literal).
	// The case the test pins: a stem containing a real path separator
	// can't exist on most filesystems as a single dir entry, but a stem
	// of literally ".." (the file named "...lsa") does collide with the
	// sanitize rules.
	if err := os.WriteFile(filepath.Join(snapshotDir, "...lsa"), []byte("not a snapshot"), 0o644); err != nil {
		t.Fatalf("plant: %v", err)
	}

	dst := NewTenantLSAIndexes()
	err := dst.LoadAll(snapshotDir)
	// We expect an aggregate error naming the unsafe file...
	if err == nil {
		t.Fatal("LoadAll: want aggregate error naming unsafe filename")
	}
	if !strings.Contains(err.Error(), "unsafe filename") {
		t.Errorf("error doesn't name the failure mode: %v", err)
	}
	// ...but the legit tenant should still be loaded.
	if dst.Get("tenant-legit") == nil {
		t.Error("legit tenant not loaded despite peer file being rejected")
	}
}
