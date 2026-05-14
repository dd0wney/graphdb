package search

import (
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LSA snapshot format. The basis is numeric-heavy (dense float32 matrices
// dominated by docVecs ~D×k and the sketch b ~l×T); encoding/gob is the
// right choice over JSON because gob ships float32 as 4 raw bytes versus
// JSON's full decimal printout. For a 100K-doc tenant at 200 dims the
// dense surface alone is ~80MB; JSON would 3-5x that and the load-time
// cost would dominate server boot.
//
// File layout (all big-endian):
//
//	magic     [4]byte = "GLSA"
//	version   uint32  = lsaSnapshotVersion
//	gob-encoded lsaSnapshot
//
// Magic + version are out-of-band of the gob payload so a corrupt or
// version-mismatched file can be diagnosed without first having to
// reflectively parse the body.
const (
	// lsaSnapshotVersion bumps when the on-disk format changes incompatibly.
	// v1 (B1): initial format with augmented-TF × IDF weighting.
	// v2 (A2): IDF replaced with log-entropy global weight (Dumais 1991);
	//          local weight switched to log(1 + tf). Snapshot field name
	//          renamed IDF → GlobalWeight to reflect the new meaning.
	// v3 (C1): DocVecs (float32, D×k) quantized to int8 via lsaQuantScale
	//          for ~4× memory + disk reduction. Snapshot field renamed
	//          DocVecs → DocVecsQ to reflect the type change.
	// Old-version snapshots fail to load with the operator-actionable
	// "regenerate via admin endpoint" message.
	lsaSnapshotVersion uint32 = 3
	// lsaSnapshotExt is the on-disk extension for a single tenant's
	// LSA snapshot. Per-tenant file naming gives tenant isolation
	// for free at the filesystem layer — `<dir>/<tenantID>.lsa`.
	lsaSnapshotExt = ".lsa"
)

// lsaSnapshotMagic is "GLSA" — graphdb LSA snapshot. Held separate from
// the version so format-version skew can be diagnosed cleanly: wrong
// magic = "not an LSA snapshot at all"; right magic + wrong version =
// "stale schema, regenerate via the admin endpoint."
var lsaSnapshotMagic = [4]byte{'G', 'L', 'S', 'A'}

// lsaSnapshot is the on-disk representation of an LSAIndex. Field names
// are exported so encoding/gob serializes them; field order is irrelevant
// (gob is name-keyed). nodeIDMap is intentionally omitted — it's a
// derived index over NodeIDs that reconstructs on load in O(D) without
// adding to the file size.
type lsaSnapshot struct {
	Dims         int
	Vocab        map[string]int32
	GlobalWeight []float32   // log-entropy per term (v2; was IDF in v1, see lsaSnapshotVersion doc)
	B            [][]float32 // sketch matrix l×T
	UB           [][]float32 // top-k eigenvectors l×k
	DocVecsQ     [][]int8    // int8-quantized L2-normalized doc embeddings D×k (v3; scale=lsaQuantScale)
	NodeIDs      []uint64
	Content      map[uint64]string
	BM25Post     map[string][]bm25Entry
	BM25Dlen     []int
	BM25Avgdl    float64
}

// WriteSnapshot serializes the index to w in the on-disk format described
// at the top of this file. Caller is responsible for closing w. Holds no
// internal locks — callers writing a tenant snapshot should serialize
// against any rebuild path themselves (TenantLSAIndexes.SaveAll handles
// this via its RWMutex).
func (i *LSAIndex) WriteSnapshot(w io.Writer) error {
	if _, err := w.Write(lsaSnapshotMagic[:]); err != nil {
		return fmt.Errorf("write magic: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, lsaSnapshotVersion); err != nil {
		return fmt.Errorf("write version: %w", err)
	}
	snap := lsaSnapshot{
		Dims:         i.dims,
		Vocab:        i.vocab,
		GlobalWeight: i.globalWeight,
		B:            i.b,
		UB:           i.ub,
		DocVecsQ:     i.docVecsQ,
		NodeIDs:      i.nodeIDs,
		Content:      i.content,
		BM25Post:     i.bm25Post,
		BM25Dlen:     i.bm25Dlen,
		BM25Avgdl:    i.bm25Avgdl,
	}
	if err := gob.NewEncoder(w).Encode(&snap); err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	return nil
}

// ReadLSASnapshot deserializes an LSAIndex from r. Returns an error if
// the magic or version bytes don't match — callers should treat
// ErrLSASnapshotVersion as "regenerate via the admin endpoint" rather
// than retrying or falling back.
func ReadLSASnapshot(r io.Reader) (*LSAIndex, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if magic != lsaSnapshotMagic {
		return nil, fmt.Errorf("not an LSA snapshot: magic %q", magic)
	}
	var version uint32
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return nil, fmt.Errorf("read version: %w", err)
	}
	if version != lsaSnapshotVersion {
		return nil, fmt.Errorf("LSA snapshot version mismatch: got %d, want %d (regenerate via admin endpoint)", version, lsaSnapshotVersion)
	}
	var snap lsaSnapshot
	if err := gob.NewDecoder(r).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	// Rebuild the derived NodeID→rowIdx map. This is O(D) and worth the
	// trade — keeping it in the file would inflate snapshots by ~16 bytes
	// per doc with no information gained over NodeIDs.
	nodeIDMap := make(map[uint64]int, len(snap.NodeIDs))
	for idx, id := range snap.NodeIDs {
		nodeIDMap[id] = idx
	}
	return &LSAIndex{
		dims:         snap.Dims,
		vocab:        snap.Vocab,
		globalWeight: snap.GlobalWeight,
		b:            snap.B,
		ub:           snap.UB,
		docVecsQ:     snap.DocVecsQ,
		nodeIDs:      snap.NodeIDs,
		nodeIDMap:    nodeIDMap,
		content:      snap.Content,
		bm25Post:     snap.BM25Post,
		bm25Dlen:     snap.BM25Dlen,
		bm25Avgdl:    snap.BM25Avgdl,
	}, nil
}

// SaveToFile writes the index to path atomically (write to .tmp, then
// rename). Same idiom as pkg/storage's snapshot to avoid leaving a
// half-written file if the process is killed mid-write.
func (i *LSAIndex) SaveToFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir snapshot dir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if err := i.WriteSnapshot(f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// LoadLSAFromFile reads an LSA index from path. Returns nil, os.ErrNotExist
// (wrapped) if the file is absent — callers should treat that as "no
// snapshot for this tenant yet" and fall through to the build path.
func LoadLSAFromFile(path string) (*LSAIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err // wrap-via-return; callers check errors.Is(err, os.ErrNotExist)
	}
	defer f.Close()
	return ReadLSASnapshot(f)
}

// SaveAll writes every tenant's LSA index to dir/<tenantID>.lsa. Tenants
// with no registered index are skipped (no file written, no error).
// Errors per tenant are returned as a single aggregate; one tenant's
// failure doesn't block others. Holds the registry's read lock for the
// duration so a concurrent Set() can't race a snapshot mid-write — the
// in-memory map is read once, then file I/O happens unlocked per tenant.
func (tli *TenantLSAIndexes) SaveAll(dir string) error {
	tli.mu.RLock()
	snapshots := make(map[string]*LSAIndex, len(tli.indexes))
	for tenantID, idx := range tli.indexes {
		snapshots[tenantID] = idx
	}
	tli.mu.RUnlock()

	if len(snapshots) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	var errs []string
	for tenantID, idx := range snapshots {
		safe, err := sanitizeTenantForFilename(tenantID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("tenant %q: %v", tenantID, err))
			continue
		}
		path := filepath.Join(dir, safe+lsaSnapshotExt)
		if err := idx.SaveToFile(path); err != nil {
			errs = append(errs, fmt.Sprintf("tenant %q: %v", tenantID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("LSA SaveAll: %s", strings.Join(errs, "; "))
	}
	return nil
}

// LoadAll reads every <tenantID>.lsa file in dir and registers each with
// the receiver. A missing dir returns nil (treat as "no snapshots yet")
// rather than an error — fresh deployments would otherwise fail to boot.
// Per-tenant decode failures are logged via the returned aggregate error
// but do not block other tenants from loading.
//
// File-naming convention: filename stem is the tenant ID after the same
// sanitization SaveAll applies. Files whose stem doesn't survive
// round-trip sanitization are silently ignored (defense against
// hand-edited or attacker-planted files with traversal-like names).
func (tli *TenantLSAIndexes) LoadAll(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dir %s: %w", dir, err)
	}
	var errs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), lsaSnapshotExt) {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), lsaSnapshotExt)
		// Defensive: refuse filenames that wouldn't survive a
		// fresh sanitize round-trip. Stops a hand-placed file
		// named "../etc/passwd.lsa" from being treated as a
		// tenant ID.
		if safe, err := sanitizeTenantForFilename(stem); err != nil || safe != stem {
			errs = append(errs, fmt.Sprintf("%s: refused (unsafe filename)", e.Name()))
			continue
		}
		path := filepath.Join(dir, e.Name())
		idx, err := LoadLSAFromFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		tli.Set(stem, idx)
	}
	if len(errs) > 0 {
		return fmt.Errorf("LSA LoadAll: %s", strings.Join(errs, "; "))
	}
	return nil
}

// sanitizeTenantForFilename validates that a tenant ID is safe to use as
// a filesystem path component. Path separators, parent-dir markers, and
// the null byte are refused outright. Returns the input unchanged if
// safe so LoadAll can do a round-trip check.
//
// Conservative on purpose: the function refuses, not normalizes. A
// tenant ID like "acme/west" should fail loudly here, not silently
// become "acmewest" with no audit trail — that's the kind of collision
// that produces cross-tenant data leakage in adjacent systems.
func sanitizeTenantForFilename(tenantID string) (string, error) {
	if tenantID == "" {
		return "", fmt.Errorf("empty tenant ID")
	}
	if tenantID == "." || tenantID == ".." {
		return "", fmt.Errorf("reserved name %q", tenantID)
	}
	if strings.ContainsAny(tenantID, "/\\\x00") {
		return "", fmt.Errorf("tenant ID contains path separator or null byte: %q", tenantID)
	}
	return tenantID, nil
}
