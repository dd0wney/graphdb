package search

import (
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dd0wney/graphdb/pkg/vfs"
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
// rename), and syncs both the file and the parent directory.
//
// The comment here previously said "Same idiom as pkg/storage's snapshot to
// avoid leaving a half-written file if the process is killed mid-write". That
// asserted a safety property by reference to another function, and it went
// false in two directions: the idiom it copied was itself missing both syncs,
// and pkg/storage's version was fixed in PR #535 while this one was not. A
// comment that borrows its correctness from elsewhere goes stale when the
// elsewhere moves, and nothing points an instrument at it.
func (i *LSAIndex) SaveToFile(path string) error {
	return i.SaveToFileWithFS(vfs.Default(), path)
}

// SaveToFileWithFS is SaveToFile on a caller-supplied driver.
//
// The driver exists so a test can observe the publish, and so a fault or crash
// simulator can reach this path at all. Before it, SaveToFile called the os
// package directly: no driver saw a single one of its operations, so a crash
// sweep over graphdb ran, reported, and never observed this function. A path
// outside the seam produces no signal — "the sweep found nothing" and "the
// sweep never saw it" render identically.
func (i *LSAIndex) SaveToFileWithFS(fsys vfs.FileSystem, path string) error {
	if err := fsys.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir snapshot dir: %w", err)
	}
	// The three temp-file removals below drop their own error on purpose. Each
	// sits on a path that is already returning the failure that brought us
	// there, and a second error about the tidying would replace the one that
	// says what actually went wrong. A removal that does not happen leaves a
	// .tmp file beside the snapshot, which the next successful save overwrites.
	//
	// This was `os.Remove` before the driver was threaded through, and errcheck
	// did not fire on it: the exclusion is keyed to the os function, not to the
	// interface method. The rule was always applicable and was simply invisible
	// while the call went straight to the os package.
	tmp := path + ".tmp"
	f, err := fsys.Open(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if err := i.WriteSnapshot(f); err != nil {
		_ = f.Close()
		_ = fsys.Remove(tmp) //nolint:errcheck // see the paragraph above
		return err
	}
	// Sync before Close. POSIX does not make close(2) flush, so without this
	// the rename below can publish a name over bytes that never reached the
	// platter, leaving a file that exists and does not decode.
	//
	// MEASURED. A crash sweep by the github.com/dd0wney/fault session (v0.1.0,
	// harness fault-graphdb-sweep, beside this repository and not in it) drove
	// this path and reopened every state it produced:
	//
	//	main 6686def                        190 absent, 156 decoded,   0 torn
	//	the same, with this Sync removed    205 absent, 171 decoded, 255 torn
	//
	// A torn state holds a file that is present and does not decode:
	// "decode snapshot: unexpected EOF". That is a rename publishing a name
	// over bytes that never reached the platter, which is exactly what the
	// paragraph above claims and what nothing measured until now.
	//
	// The second row is what gives the first one a meaning. A sweep that only
	// ever ran against correct code reports a pass it cannot distinguish from
	// never having looked.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = fsys.Remove(tmp) //nolint:errcheck // see the paragraph above
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = fsys.Remove(tmp) //nolint:errcheck // see the paragraph above
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := fsys.Rename(tmp, path); err != nil {
		_ = fsys.Remove(tmp) //nolint:errcheck // see the paragraph above
		return fmt.Errorf("rename: %w", err)
	}
	// The rename is atomic, but the directory entry it creates is not durable
	// until the parent directory is itself synced. Without this a power cut
	// after the rename loses the publish whole.
	if err := vfs.SyncParentDir(fsys, path); err != nil {
		return fmt.Errorf("sync snapshot dir: %w", err)
	}
	return nil
}

// LoadLSAFromFile reads an LSA index from path on the default driver.
// Returns nil, os.ErrNotExist (wrapped) if the file is absent — callers should
// treat that as "no snapshot for this tenant yet" and fall through to the
// build path.
func LoadLSAFromFile(path string) (*LSAIndex, error) {
	return LoadLSAFromFileWithFS(vfs.Default(), path)
}

// LoadLSAFromFileWithFS reads an LSA index from path through fsys.
//
// It is the reload counterpart to SaveToFileWithFS, and it exists for the same
// reason: a path that calls the os package directly is invisible to every
// driver. The publish was threaded first, which left the two halves asymmetric.
// A crash sweep could then write a scenario's states through its driver and had
// no way to read them back through it. A check that reopened with
// LoadLSAFromFile read the real directory instead, found every state intact,
// and passed. It was not measuring the file the scenario had built.
//
// The absent contract is the caller-visible one and it is unchanged: a driver
// must return an error satisfying errors.Is(err, os.ErrNotExist) when the file
// is not there, because LoadAll tells "no snapshot yet" from a fault by that
// test alone.
//
// Every other failure must NOT satisfy it, and that is the whole requirement.
// A refusal that read as absent would make LoadAll drop a tenant's index and
// rebuild it from nothing. Refusals do not share one sentinel: a CAPABILITY
// refusal satisfies errors.ErrUnsupported (#534, ADR 0003), and a driver may
// refuse for reasons that are not about capability at all — the fault driver
// this function was added for refuses a path outside its recorded root, which
// matches neither sentinel. Do not test refusals by matching one error. Test
// absence, and treat the rest as a fault.
//
// The Close error is dropped on purpose. This is a read-only handle, so there
// are no buffered bytes for Close to lose, and the decoded index is already in
// hand. Note the explicit discard rather than a bare `defer f.Close()`:
// errcheck's exclusion is keyed to the os function, not to the interface
// method, so the rule that was invisible here becomes visible the moment the
// driver is threaded. SaveToFileWithFS records the same surprise above.
func LoadLSAFromFileWithFS(fsys vfs.FileSystem, path string) (*LSAIndex, error) {
	f, err := fsys.Open(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err // wrap-via-return; callers check errors.Is(err, os.ErrNotExist)
	}
	defer func() { _ = f.Close() }()
	return ReadLSASnapshot(f)
}

// DeleteLSASnapshot removes a tenant's on-disk LSA snapshot (<dir>/<tenant>.lsa).
// A missing file is not an error. Call this on tenant deletion so LoadAll
// doesn't resurrect the deleted tenant's index on the next restart. Mirrors
// LoadAll's filename sanitization so the path matches what SaveToFile wrote.
func DeleteLSASnapshot(dir, tenantID string) error {
	safe, err := sanitizeTenantForFilename(tenantID)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, safe+lsaSnapshotExt)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
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
