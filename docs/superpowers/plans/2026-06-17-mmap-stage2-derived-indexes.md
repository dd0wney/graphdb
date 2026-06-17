# mmap reopen Stage 2 — Derived Indexes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the residual ~2.8s eager derived-index field-scan from mmap-mode reopen by persisting adjacency as immutable CSR and lazy-building membership indexes on first enumeration.

**Architecture:** The binary `snapshot.mmap` format (v2→v3) gains two CSR adjacency sections + a combined adjacency directory; `getEdgeIDsForNode` reads the base CSR (filtered by Stage-1 edge tombstones) unioned with the post-open overlay — no copy-on-write. Membership indexes (`nodesByLabel`/`edgesByType`/per-tenant) are no longer built at open; a guarded `ensureMembershipBuilt()` builds them once on the first enumeration query, skipping overlay-shadowed and tombstoned base entities. Per-tenant counts move into persisted metadata so `CountNodesForTenant` stays correct without triggering the build.

**Tech Stack:** Go, `pkg/storage`. Existing mmap machinery: `mmap_snapshot_{format,writer,reader,loader,persist}.go`. Off-by-default (`StorageConfig.UseMmapSnapshot`); JSON path unchanged. Correctness gate = public-interface parity vs JSON (`mmap_reopen_test.go`); `checkGraphInvariants` does NOT apply in mmap mode.

**Spec:** `docs/superpowers/specs/2026-06-17-mmap-stage2-derived-indexes-design.md`

---

## File Structure

- `pkg/storage/mmap_snapshot_format.go` — MODIFY: version 2→3; header CSR section/dir offsets; `mmapMetadata.TenantStats`; CSR encode/decode helpers.
- `pkg/storage/mmap_snapshot_writer.go` — MODIFY: emit CSR sections; gather `tenantStats` in `buildMmapMetadata`.
- `pkg/storage/mmap_snapshot_reader.go` — MODIFY: parse CSR sections; `outgoingCSR`/`incomingCSR` accessors; extend CRC/sections bounds.
- `pkg/storage/mmap_snapshot_loader.go` — MODIFY: drop eager index loops; restore `tenantStats`; clean profiler marks.
- `pkg/storage/storage_helpers.go` — MODIFY: `getEdgeIDsForNode` mmap-base branch.
- `pkg/storage/membership_lazy.go` — CREATE: `ensureMembershipBuilt` + build-only inserts + enumeration-entry guards.
- `pkg/storage/tenant_operations.go`, `pkg/storage/query_operations.go`, `pkg/storage/pagination.go` — MODIFY: call `gs.ensureMembershipBuilt()` at enumeration entry points.
- `pkg/storage/mmap_snapshot_format_test.go`, `pkg/storage/mmap_reopen_test.go`, `pkg/storage/mmap_snapshot_bench_test.go` — MODIFY: CSR round-trip + parity tests.
- `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` — MODIFY: append Stage 2a result.

---

## Task 1: CSR codec helpers + metadata TenantStats field

Additive only — no wiring, no version bump. Keeps the build and all existing tests green.

**Files:**
- Modify: `pkg/storage/mmap_snapshot_format.go`
- Test: `pkg/storage/mmap_snapshot_format_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/storage/mmap_snapshot_format_test.go`:

```go
func TestCSRRunCodec_RoundTrip(t *testing.T) {
	// A CSR run is a length-prefixed []uint64: count(4) then count*uint64.
	in := []uint64{7, 11, 13, 9000000001}
	buf := appendCSRRun(nil, in)

	got, n := readCSRRun(buf, 0)
	if n != len(buf) {
		t.Fatalf("readCSRRun consumed %d, want %d", n, len(buf))
	}
	if len(got) != len(in) {
		t.Fatalf("len got %d want %d", len(got), len(in))
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("got[%d]=%d want %d", i, got[i], in[i])
		}
	}

	// Empty run encodes to a 4-byte zero count and decodes to nil.
	empty := appendCSRRun(nil, nil)
	if len(empty) != 4 {
		t.Fatalf("empty run len %d want 4", len(empty))
	}
	if got, _ := readCSRRun(empty, 0); got != nil {
		t.Errorf("empty run decoded to %v want nil", got)
	}
}

func TestMmapMetadata_TenantStatsRoundTrip(t *testing.T) {
	m := &mmapMetadata{TenantStats: map[string]TenantStats{
		"acme": {NodeCount: 5, EdgeCount: 9, StorageBytes: 100, LastUpdated: 42},
	}}
	b, err := m.marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := unmarshalMmapMetadata(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantStats["acme"].EdgeCount != 9 {
		t.Errorf("EdgeCount=%d want 9", got.TenantStats["acme"].EdgeCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/storage/ -run 'TestCSRRunCodec_RoundTrip|TestMmapMetadata_TenantStatsRoundTrip' -count=1`
Expected: FAIL — `undefined: appendCSRRun` / `readCSRRun` / `m.TenantStats`.

- [ ] **Step 3: Add the CSR codec helpers + metadata field**

In `pkg/storage/mmap_snapshot_format.go`, add the field to `mmapMetadata` (after `StickyEdgeTypes`):

```go
	StickyEdgeTypes  []string
	// TenantStats persists per-tenant counts so reopen restores them without the
	// (now-lazy) membership build. Keyed by tenant ID string.
	TenantStats map[string]TenantStats
```

Add the CSR run codec (a length-prefixed `[]uint64`) near the record codec section:

```go
// --- CSR adjacency run codec ----------------------------------------------
//
// A run is count(4) then count little-endian uint64 edge IDs. Empty runs encode
// as a bare zero count (4 bytes) and decode to nil.

func appendCSRRun(buf []byte, ids []uint64) []byte {
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(ids)))
	for _, id := range ids {
		buf = binary.LittleEndian.AppendUint64(buf, id)
	}
	return buf
}

// readCSRRun decodes a run at buf[p:], COPYING the IDs into a fresh slice so the
// result is safe to retain after the mapping is closed. Returns nil for an empty run.
func readCSRRun(buf []byte, p int) ([]uint64, int) {
	n := int(binary.LittleEndian.Uint32(buf[p:]))
	p += 4
	if n == 0 {
		return nil, p
	}
	ids := make([]uint64, n)
	for i := 0; i < n; i++ {
		ids[i] = binary.LittleEndian.Uint64(buf[p:])
		p += 8
	}
	return ids, p
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/storage/ -run 'TestCSRRunCodec_RoundTrip|TestMmapMetadata_TenantStatsRoundTrip' -count=1`
Expected: PASS.

- [ ] **Step 5: Verify nothing else broke + commit**

Run: `go build ./pkg/storage/ && go test ./pkg/storage/ -run 'TestMmap|TestCSR' -count=1 -timeout 120s`
Expected: PASS (existing mmap tests still pass; metadata field is additive — old v2 files unmarshal with `TenantStats == nil`).

```bash
git add pkg/storage/mmap_snapshot_format.go pkg/storage/mmap_snapshot_format_test.go
git commit -m "feat(storage): mmap CSR run codec + metadata TenantStats field (Stage 2a)"
```

---

## Task 2: Persist CSR adjacency sections in the file (format v3)

One logical change spanning header + writer + reader so the file stays internally valid (and green) at the commit boundary. The loader is untouched — it keeps its eager adjacency rebuild and ignores the new sections, so behavior is unchanged.

**Files:**
- Modify: `pkg/storage/mmap_snapshot_format.go` (header fields, version bump, CRC)
- Modify: `pkg/storage/mmap_snapshot_writer.go` (emit CSR)
- Modify: `pkg/storage/mmap_snapshot_reader.go` (parse CSR, accessors)
- Test: `pkg/storage/mmap_snapshot_format_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/storage/mmap_snapshot_format_test.go`:

```go
func TestMmapSnapshot_CSRRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.mmap")

	nodes := []*Node{{ID: 1, TenantID: "t"}, {ID: 2, TenantID: "t"}, {ID: 3, TenantID: "t"}}
	// edges: 1->2 (id10), 1->3 (id11), 2->3 (id12)
	edges := []*Edge{
		{ID: 10, TenantID: "t", FromNodeID: 1, ToNodeID: 2, Type: "E"},
		{ID: 11, TenantID: "t", FromNodeID: 1, ToNodeID: 3, Type: "E"},
		{ID: 12, TenantID: "t", FromNodeID: 2, ToNodeID: 3, Type: "E"},
	}
	if err := writeMmapSnapshotData(path, nodes, edges, &mmapMetadata{}); err != nil {
		t.Fatal(err)
	}
	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.close()

	assertU64Set := func(name string, got, want []uint64) {
		t.Helper()
		gm := map[uint64]bool{}
		for _, x := range got {
			gm[x] = true
		}
		if len(got) != len(want) {
			t.Fatalf("%s len got %d want %d (%v)", name, len(got), len(want), got)
		}
		for _, w := range want {
			if !gm[w] {
				t.Errorf("%s missing %d (got %v)", name, w, got)
			}
		}
	}
	assertU64Set("out(1)", snap.outgoingCSR(1), []uint64{10, 11})
	assertU64Set("out(2)", snap.outgoingCSR(2), []uint64{12})
	assertU64Set("out(3)", snap.outgoingCSR(3), nil)
	assertU64Set("in(3)", snap.incomingCSR(3), []uint64{11, 12})
	assertU64Set("in(1)", snap.incomingCSR(1), nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/storage/ -run TestMmapSnapshot_CSRRoundTrip -count=1`
Expected: FAIL — `snap.outgoingCSR` undefined.

- [ ] **Step 3: Add header fields + version bump in `mmap_snapshot_format.go`**

Bump the version and extend the header. Replace the const block offsets so the new fields follow `hCRC`'s old position; the simplest non-fragile change is to append the new offsets AFTER the existing ones and grow the header. Update:

```go
	mmapSnapshotVersion uint32 = 3 // v3 adds CSR adjacency sections + TenantStats
	dirAbsent           int64  = -1

	// Header field byte offsets.
	hMagic         = 0
	hVersion       = 4
	hFlags         = 8
	hNodeCount     = 12
	hEdgeCount     = 20
	hMinNodeID     = 28
	hMaxNodeID     = 36
	hMinEdgeID     = 44
	hMaxEdgeID     = 52
	hNodeDir       = 60
	hEdgeDir       = 68
	hMetaOff       = 76
	hMetaLen       = 84
	hOutCSR        = 92  // outgoing CSR data section offset
	hInCSR         = 100 // incoming CSR data section offset
	hAdjDir        = 108 // combined adjacency directory offset
	hCRC           = 116
	mmapHeaderSize = 124 // hCRC(4) + pad(4) -> 124
)
```

Add the three offsets to `mmapSnapshotHeader`:

```go
	metaOffset    uint64
	metaLen       uint64
	outCSROffset  uint64
	inCSROffset   uint64
	adjDirOffset  uint64
	crc           uint32
```

In `marshal()`, after the `hMetaLen` write and before `hCRC`:

```go
	binary.LittleEndian.PutUint64(b[hOutCSR:], h.outCSROffset)
	binary.LittleEndian.PutUint64(b[hInCSR:], h.inCSROffset)
	binary.LittleEndian.PutUint64(b[hAdjDir:], h.adjDirOffset)
```

In `unmarshalMmapHeader()`, add to the returned struct:

```go
		outCSROffset: binary.LittleEndian.Uint64(b[hOutCSR:]),
		inCSROffset:  binary.LittleEndian.Uint64(b[hInCSR:]),
		adjDirOffset: binary.LittleEndian.Uint64(b[hAdjDir:]),
```

Add the combined adjacency directory entry layout (4×int64 per node: outOff, outLen, inOff, inLen) as a constant near the directory helpers:

```go
const adjDirEntrySize = 32 // 4 * int64: outOff, outLen, inOff, inLen
```

- [ ] **Step 4: Emit CSR in `mmap_snapshot_writer.go`**

In `writeMmapSnapshotData`, after the edge section and before `hdr.metaOffset` is set, build per-node buckets and write the two CSR data sections + the combined directory. Insert:

```go
	// CSR adjacency: bucket edge IDs per endpoint, then emit outgoing data,
	// incoming data, and a dense combined directory indexed by nodeID-minNodeID.
	var adjDirBytes []byte
	if len(nodes) > 0 {
		out := make(map[uint64][]uint64, len(nodes))
		in := make(map[uint64][]uint64, len(nodes))
		for _, e := range edges {
			out[e.FromNodeID] = append(out[e.FromNodeID], e.ID)
			in[e.ToNodeID] = append(in[e.ToNodeID], e.ID)
		}
		type adjEntry struct{ outOff, outLen, inOff, inLen int64 }
		entries := make([]adjEntry, hdr.maxNodeID-hdr.minNodeID+1)

		hdr.outCSROffset = uint64(offset)
		for _, n := range nodes {
			ids := out[n.ID]
			entries[n.ID-hdr.minNodeID].outOff = offset
			entries[n.ID-hdr.minNodeID].outLen = int64(len(ids))
			rec := appendCSRRun(nil, ids)
			if _, err := w.Write(rec); err != nil {
				return err
			}
			offset += int64(len(rec))
		}
		hdr.inCSROffset = uint64(offset)
		for _, n := range nodes {
			ids := in[n.ID]
			entries[n.ID-hdr.minNodeID].inOff = offset
			entries[n.ID-hdr.minNodeID].inLen = int64(len(ids))
			rec := appendCSRRun(nil, ids)
			if _, err := w.Write(rec); err != nil {
				return err
			}
			offset += int64(len(rec))
		}
		hdr.adjDirOffset = uint64(offset)
		adjDirBytes = make([]byte, len(entries)*adjDirEntrySize)
		for i, e := range entries {
			b := adjDirBytes[i*adjDirEntrySize:]
			binary.LittleEndian.PutUint64(b[0:], uint64(e.outOff))
			binary.LittleEndian.PutUint64(b[8:], uint64(e.outLen))
			binary.LittleEndian.PutUint64(b[16:], uint64(e.inOff))
			binary.LittleEndian.PutUint64(b[24:], uint64(e.inLen))
		}
		if _, err := w.Write(adjDirBytes); err != nil {
			return err
		}
		offset += int64(len(adjDirBytes))
	}
```

Extend the CRC to cover the adjacency directory. Change the `computeCRC` signature in `mmap_snapshot_format.go`:

```go
func computeCRC(headerNoCRC, nodeDir, edgeDir, adjDir, meta []byte) uint32 {
	h := crc32.NewIEEE()
	h.Write(headerNoCRC)
	h.Write(nodeDir)
	h.Write(edgeDir)
	h.Write(adjDir)
	h.Write(meta)
	return h.Sum32()
}
```

And update the writer's CRC call:

```go
	hdr.crc = computeCRC(hdr.marshal()[:hCRC], nodeDirBytes, edgeDirBytes, adjDirBytes, metaBytes)
```

- [ ] **Step 5: Parse CSR + accessors in `mmap_snapshot_reader.go`**

Extend `sections()` to also return and bounds-check the adjacency directory, and update the CRC call in `openMmapSnapshot`. Replace `sections` signature/body:

```go
func sections(data []byte, hdr *mmapSnapshotHeader) (nodeDir, edgeDir, adjDir, meta []byte, err error) {
	size := uint64(len(data))
	nodeDirEnd := hdr.nodeDirOffset + hdr.nodeDirLen()*8
	edgeDirEnd := hdr.edgeDirOffset + hdr.edgeDirLen()*8
	adjDirEnd := hdr.adjDirOffset + hdr.nodeDirLen()*adjDirEntrySize
	metaEnd := hdr.metaOffset + hdr.metaLen
	if hdr.nodeCount > 0 && (nodeDirEnd > size || adjDirEnd > size) ||
		hdr.edgeCount > 0 && edgeDirEnd > size ||
		metaEnd > size {
		return nil, nil, nil, nil, fmt.Errorf("mmap snapshot section out of bounds (size %d)", size)
	}
	if hdr.nodeCount > 0 {
		nodeDir = data[hdr.nodeDirOffset:nodeDirEnd]
		adjDir = data[hdr.adjDirOffset:adjDirEnd]
	}
	if hdr.edgeCount > 0 {
		edgeDir = data[hdr.edgeDirOffset:edgeDirEnd]
	}
	meta = data[hdr.metaOffset:metaEnd]
	return nodeDir, edgeDir, adjDir, meta, nil
}
```

Update the caller in `openMmapSnapshot`:

```go
	nodeDir, edgeDir, adjDir, metaBytes, err := sections(data, hdr)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}
	if got := computeCRC(data[:hCRC], nodeDir, edgeDir, adjDir, metaBytes); got != hdr.crc {
		_ = syscall.Munmap(data)
		return nil, fmt.Errorf("mmap snapshot %q CRC mismatch: got %08x want %08x", path, got, hdr.crc)
	}
```

Add the accessors at the end of `mmap_snapshot_reader.go`:

```go
// adjDirEntry returns (outOff, outLen, inOff, inLen) for a node, or false if the
// node is outside the directory range.
func (m *mmapSnapshot) adjDirEntry(id uint64) (outOff, outLen, inOff, inLen int64, ok bool) {
	if m.hdr.nodeCount == 0 || id < m.hdr.minNodeID || id > m.hdr.maxNodeID {
		return 0, 0, 0, 0, false
	}
	p := m.hdr.adjDirOffset + (id-m.hdr.minNodeID)*adjDirEntrySize
	b := m.data[p:]
	return int64(binary.LittleEndian.Uint64(b[0:])),
		int64(binary.LittleEndian.Uint64(b[8:])),
		int64(binary.LittleEndian.Uint64(b[16:])),
		int64(binary.LittleEndian.Uint64(b[24:])), true
}

// outgoingCSR / incomingCSR return a freshly-decoded copy of the node's base
// adjacency run (nil if none). Safe to retain after close.
func (m *mmapSnapshot) outgoingCSR(id uint64) []uint64 {
	outOff, outLen, _, _, ok := m.adjDirEntry(id)
	if !ok || outLen == 0 {
		return nil
	}
	ids, _ := readCSRRun(m.data, int(outOff))
	return ids
}

func (m *mmapSnapshot) incomingCSR(id uint64) []uint64 {
	_, _, inOff, inLen, ok := m.adjDirEntry(id)
	if !ok || inLen == 0 {
		return nil
	}
	ids, _ := readCSRRun(m.data, int(inOff))
	return ids
}
```

- [ ] **Step 6: Run the round-trip test + full mmap suite**

Run: `go test ./pkg/storage/ -run 'TestMmapSnapshot_CSRRoundTrip|TestMmap' -count=1 -timeout 180s`
Expected: PASS. (Existing tests still pass: writer emits v3, reader parses it; loader still does eager rebuild and ignores CSR.)

- [ ] **Step 7: Commit**

```bash
git add pkg/storage/mmap_snapshot_format.go pkg/storage/mmap_snapshot_writer.go pkg/storage/mmap_snapshot_reader.go pkg/storage/mmap_snapshot_format_test.go
git commit -m "feat(storage): persist CSR adjacency sections in mmap snapshot v3 (Stage 2a)"
```

---

## Task 3: Read adjacency from CSR + drop the eager adjacency loop

Flip `getEdgeIDsForNode` to the CSR base (filtered by edge tombstones) ∪ overlay, and remove the eager adjacency rebuild from the loader. These land together so adjacency stays correct.

**Files:**
- Modify: `pkg/storage/storage_helpers.go` (`getEdgeIDsForNode`)
- Modify: `pkg/storage/mmap_snapshot_loader.go` (drop adjacency loop)
- Test: `pkg/storage/mmap_reopen_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/storage/mmap_reopen_test.go` (reuse the file's existing `mmapConfig` / build helpers — see `TestMmapReopen_*` for the pattern):

```go
func TestMmapStage2_AdjacencyFromCSR(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"

	// Build: 1->2, 1->3, 2->3. Snapshot. Reopen in mmap mode.
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	mustNode := func(g *GraphStorage) uint64 {
		n, err := g.CreateNodeWithTenant(tenant, []string{"N"}, map[string]Value{})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	n1, n2, n3 := mustNode(gs), mustNode(gs), mustNode(gs)
	mkEdge := func(from, to uint64) uint64 {
		e, err := gs.CreateEdgeWithTenant(tenant, from, to, "E", map[string]Value{}, 1.0)
		if err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	e12 := mkEdge(n1, n2)
	mkEdge(n1, n3)
	mkEdge(n2, n3)
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	outLen := func(n uint64) int {
		e, _ := mr.GetOutgoingEdgesForTenant(n, tenant)
		return len(e)
	}
	// Base CSR read.
	if outLen(n1) != 2 {
		t.Errorf("base out(n1)=%d want 2", outLen(n1))
	}
	// Tombstone filter: delete a base edge, adjacency drops it.
	if err := mr.DeleteEdgeForTenant(e12, tenant); err != nil {
		t.Fatal(err)
	}
	if outLen(n1) != 1 {
		t.Errorf("after delete out(n1)=%d want 1", outLen(n1))
	}
	// Overlay append: add a new edge from a base node.
	mkEdgeMR := func(from, to uint64) {
		if _, err := mr.CreateEdgeWithTenant(tenant, from, to, "E", map[string]Value{}, 1.0); err != nil {
			t.Fatal(err)
		}
	}
	mkEdgeMR(n1, n3)
	if outLen(n1) != 2 {
		t.Errorf("after overlay add out(n1)=%d want 2", outLen(n1))
	}
	// Survives a second reopen.
	if err := mr.Close(); err != nil {
		t.Fatal(err)
	}
	mr2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr2.Close()
	e, _ := mr2.GetOutgoingEdgesForTenant(n1, tenant)
	if len(e) != 2 {
		t.Errorf("after 2nd reopen out(n1)=%d want 2", len(e))
	}
}
```

> Signatures (verified against `pkg/storage`): `CreateNodeWithTenant(tenantID string, labels []string, props map[string]Value) (*Node, error)`; `CreateEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, props map[string]Value, weight float64) (*Edge, error)`; `GetOutgoingEdgesForTenant(nodeID uint64, tenantID string) ([]*Edge, error)`; `DeleteEdgeForTenant(edgeID uint64, tenantID string) error`. `mmapConfig(dir)` / `NewGraphStorageWithConfig` are the existing mmap test helpers.

- [ ] **Step 2: Run test to verify it fails (or passes for the wrong reason)**

Run: `go test ./pkg/storage/ -run TestMmapStage2_AdjacencyFromCSR -count=1`
Expected: PASS currently (eager rebuild still populates `outgoingEdges`). This is intentional — it is the regression guard. Proceed to make it pass *without* the eager loop.

- [ ] **Step 3: Add the mmap-base branch to `getEdgeIDsForNode`**

In `pkg/storage/storage_helpers.go`, in the `else` branch (non-disk-backed), after the compressed/uncompressed lookups, add the mmap-base merge. Replace the "Fall back to uncompressed storage" block with:

```go
		// Overlay: edges created since open live in the uncompressed map.
		var overlay []uint64
		if outgoing {
			overlay = gs.outgoingEdges[nodeID]
		} else {
			overlay = gs.incomingEdges[nodeID]
		}

		// mmap base: immutable CSR run, minus tombstoned edges. New IDs in the
		// overlay are disjoint from base IDs, so the union needs no dedup.
		if gs.mmapSnap != nil {
			var base []uint64
			if outgoing {
				base = gs.mmapSnap.outgoingCSR(nodeID)
			} else {
				base = gs.mmapSnap.incomingCSR(nodeID)
			}
			result := make([]uint64, 0, len(base)+len(overlay))
			for _, eid := range base {
				if !gs.isEdgeDeletedLocked(eid) {
					result = append(result, eid)
				}
			}
			for _, eid := range overlay {
				if !gs.isEdgeDeletedLocked(eid) {
					result = append(result, eid)
				}
			}
			if len(result) == 0 {
				return nil
			}
			return result
		}

		if len(overlay) > 0 {
			return overlay
		}
	}

	return nil
}
```

> Note: this preserves the existing non-mmap behavior exactly (when `mmapSnap == nil`, it returns the uncompressed overlay as before). The `isEdgeDeletedLocked` filter on the overlay is harmless in JSON mode (the tombstone set is empty / the branch short-circuits on `mmapSnap == nil`).

- [ ] **Step 4: Drop the eager adjacency rebuild from the loader**

In `pkg/storage/mmap_snapshot_loader.go`, the edge loop currently builds both membership AND adjacency. Remove ONLY the two adjacency append lines (membership stays for now — it is removed in Task 4):

```go
	snap.forEachEdgeID(func(id uint64, off int64) {
		eid, from, to, tenant, etype := scanEdgeFields(snap.data, off)
		stub := &Edge{ID: eid, TenantID: tenant, Type: etype, FromNodeID: from, ToNodeID: to}
		addToLabelIndex(gs.edgesByType, etype, eid)
		gs.addEdgeToTenantIndex(stub)
		// adjacency now served from CSR base in getEdgeIDsForNode (Stage 2a)
	})
```

(`from`/`to` are still read by `scanEdgeFields` but now unused in the body — keep the destructuring with `_` to satisfy the compiler: `eid, _, _, tenant, etype := scanEdgeFields(...)`.)

- [ ] **Step 5: Run the adjacency test + race + full mmap suite**

Run: `go test ./pkg/storage/ -run 'TestMmapStage2_AdjacencyFromCSR|TestMmap' -count=1 -timeout 180s`
Expected: PASS.
Run: `go test -race ./pkg/storage/ -run 'TestMmapStage2_AdjacencyFromCSR' -count=3 -timeout 180s`
Expected: PASS, race-clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/storage/storage_helpers.go pkg/storage/mmap_snapshot_loader.go pkg/storage/mmap_reopen_test.go
git commit -m "feat(storage): serve mmap adjacency from CSR base + tombstones (Stage 2a)"
```

---

## Task 4: Lazy membership build + restore tenant counts from metadata

Stop building membership at open; build it once on first enumeration. Restore per-tenant counts from persisted metadata so counts are correct at open without the build.

**Files:**
- Create: `pkg/storage/membership_lazy.go`
- Modify: `pkg/storage/mmap_snapshot_writer.go` (`buildMmapMetadata` gathers tenantStats)
- Modify: `pkg/storage/mmap_snapshot_loader.go` (drop membership loops; restore tenantStats)
- Modify: `pkg/storage/storage_types.go` (add `membershipBuilt` field)
- Modify: `pkg/storage/tenant_operations.go`, `pkg/storage/query_operations.go`, `pkg/storage/pagination.go` (call the guard)
- Test: `pkg/storage/mmap_reopen_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `pkg/storage/mmap_reopen_test.go`:

```go
func TestMmapStage2_LazyMembershipParity(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := gs.CreateNodeWithTenant(tenant, []string{"Alpha"}, map[string]Value{}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := gs.CreateNodeWithTenant(tenant, []string{"Beta"}, map[string]Value{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	// Counts correct WITHOUT triggering an enumeration (stats decoupled).
	if got := mr.CountNodesForTenant(tenant); got != 8 {
		t.Errorf("CountNodesForTenant=%d want 8 (must not need membership build)", got)
	}
	// First enumeration triggers the lazy build; results match.
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Alpha")); got != 5 {
		t.Errorf("Alpha=%d want 5", got)
	}
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Beta")); got != 3 {
		t.Errorf("Beta=%d want 3", got)
	}
	// A post-open create is reflected (overlay indexed at write time).
	if _, err := mr.CreateNodeWithTenant(tenant, []string{"Alpha"}, map[string]Value{}); err != nil {
		t.Fatal(err)
	}
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Alpha")); got != 6 {
		t.Errorf("Alpha after create=%d want 6", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/storage/ -run TestMmapStage2_LazyMembershipParity -count=1`
Expected: PASS currently (eager build still runs). It becomes the regression guard once the eager build is removed. Proceed.

- [ ] **Step 3: Add `membershipBuilt` field**

In `pkg/storage/storage_types.go`, in the `GraphStorage` struct near the membership maps, add:

```go
	// membershipBuilt is false after an mmap reopen until the first enumeration
	// query triggers ensureMembershipBuilt. Guarded by gs.mu. Always true (no-op)
	// in JSON mode, where membership is built at load.
	membershipBuilt bool
```

- [ ] **Step 4: Create `pkg/storage/membership_lazy.go`**

```go
package storage

// Lazy membership index build for the mmap reopen mode (graphdb ask #1, Stage 2a).
//
// In mmap mode the membership indexes (nodesByLabel, edgesByType, per-tenant
// label/type maps and ID sets) are NOT built at open — that field-scan was 74%
// of the residual reopen cost. They are built once, on the first enumeration
// query, from the mmap base, skipping any base entity that is already shadowed by
// the shard overlay or tombstoned (those were maintained at write time). Per-tenant
// COUNTS are restored from persisted metadata at open, so this build does not touch
// them (avoids double-counting).
//
// No-op when mmapSnap == nil or membership is already built.

// ensureMembershipBuilt builds the membership indexes from the mmap base exactly
// once. Safe under concurrent first-enumeration: takes gs.mu and double-checks.
func (gs *GraphStorage) ensureMembershipBuilt() {
	if gs.mmapSnap == nil {
		return
	}
	gs.mu.RLock()
	built := gs.membershipBuilt
	gs.mu.RUnlock()
	if built {
		return
	}

	gs.mu.Lock()
	defer gs.mu.Unlock()
	if gs.membershipBuilt {
		return
	}

	gs.mmapSnap.forEachNodeID(func(id uint64, off int64) {
		if _, shadowed := gs.lookupNodeShard(id); shadowed || gs.isNodeDeletedLocked(id) {
			return // overlay entity already indexed at write time; tombstone skipped
		}
		nid, tenant, labels := scanNodeFields(gs.mmapSnap.data, off)
		for _, label := range labels {
			addToLabelIndex(gs.nodesByLabel, label, nid)
		}
		gs.addNodeToTenantIndexNoCount(nid, tenant, labels)
	})

	gs.mmapSnap.forEachEdgeID(func(id uint64, off int64) {
		if _, shadowed := gs.lookupEdgeShard(id); shadowed || gs.isEdgeDeletedLocked(id) {
			return
		}
		eid, _, _, tenant, etype := scanEdgeFields(gs.mmapSnap.data, off)
		addToLabelIndex(gs.edgesByType, etype, eid)
		gs.addEdgeToTenantIndexNoCount(eid, tenant, etype)
	})

	gs.membershipBuilt = true
}

// addNodeToTenantIndexNoCount mirrors addNodeToTenantIndex's label/ID-set inserts
// but does NOT touch tenant counts (restored from metadata at open).
func (gs *GraphStorage) addNodeToTenantIndexNoCount(id uint64, tenantID string, labels []string) {
	tid := effectiveTenantID(tenantID)
	if gs.tenantNodesByLabel[tid] == nil {
		gs.tenantNodesByLabel[tid] = make(labelIndex)
	}
	for _, label := range labels {
		addToLabelIndex(gs.tenantNodesByLabel[tid], label, id)
	}
	if gs.tenantNodeIDs[tid] == nil {
		gs.tenantNodeIDs[tid] = make(map[uint64]struct{})
	}
	gs.tenantNodeIDs[tid][id] = struct{}{}
}

// addEdgeToTenantIndexNoCount mirrors addEdgeToTenantIndex without count side effects.
func (gs *GraphStorage) addEdgeToTenantIndexNoCount(id uint64, tenantID, etype string) {
	tid := effectiveTenantID(tenantID)
	if gs.tenantEdgesByType[tid] == nil {
		gs.tenantEdgesByType[tid] = make(labelIndex)
	}
	addToLabelIndex(gs.tenantEdgesByType[tid], etype, id)
	if gs.tenantEdgeIDs[tid] == nil {
		gs.tenantEdgeIDs[tid] = make(map[uint64]struct{})
	}
	gs.tenantEdgeIDs[tid][id] = struct{}{}
}
```

> `effectiveTenantID` takes a `string` and returns `tenantid.TenantID` (see `tenant_operations.go`). Confirm `tenantNodesByLabel`/`tenantEdgesByType`/`tenantNodeIDs`/`tenantEdgeIDs` are keyed by `tenantid.TenantID` (they are — see `addNodeToTenantIndex`).

- [ ] **Step 5: Gather tenantStats in `buildMmapMetadata`**

In `pkg/storage/mmap_snapshot_writer.go`, in `buildMmapMetadata`, add a tenantStats snapshot (caller already holds the snapshot RLock / quiescent):

```go
	tenantStats := make(map[string]TenantStats, len(gs.tenantStats))
	for tid, st := range gs.tenantStats {
		if st != nil {
			tenantStats[string(tid)] = *st
		}
	}
	return &mmapMetadata{
		PropertyIndexes:  propIdx,
		VectorIndexes:    gs.vectorIndex.IndexDefinitions(),
		Stats:            gs.GetStatistics(),
		NextNodeID:       atomic.LoadUint64(&gs.nextNodeID),
		NextEdgeID:       atomic.LoadUint64(&gs.nextEdgeID),
		StickyNodeLabels: labelIndexKeys(gs.nodesByLabel),
		StickyEdgeTypes:  labelIndexKeys(gs.edgesByType),
		TenantStats:      tenantStats,
	}
```

- [ ] **Step 6: In the loader — drop membership loops, restore tenantStats**

In `pkg/storage/mmap_snapshot_loader.go`, replace the node membership loop and the (now membership-only) edge loop with nothing, and after `gs.stats = meta.Stats` restore tenant counts:

```go
	// Membership indexes are built lazily on first enumeration (Stage 2a) — see
	// membership_lazy.go. Only sticky keys (above) are registered at open.

	// ... property indexes, vector defs unchanged ...

	gs.nextNodeID = meta.NextNodeID
	gs.nextEdgeID = meta.NextEdgeID
	gs.stats = meta.Stats
	atomic.StoreUint64(&gs.avgQueryTimeBits, math.Float64bits(meta.Stats.AvgQueryTime))

	// Restore per-tenant counts from metadata so CountNodesForTenant is correct
	// without triggering the lazy membership build.
	for tid, st := range meta.TenantStats {
		s := st
		gs.tenantStats[tenantid.TenantID(tid)] = &s
	}
	// membershipBuilt stays false: built on first enumeration.
	return nil
```

Delete the two `snap.forEachNodeID(...)` / `snap.forEachEdgeID(...)` membership loops entirely. (`tenantid` is already imported.)

- [ ] **Step 7: Call the guard at enumeration entry points**

Add `gs.ensureMembershipBuilt()` as the FIRST statement (before taking `gs.mu`) in each enumeration reader. The guard takes `gs.mu` itself, so it must be called before the method's own `RLock`. Apply to:

- `pkg/storage/query_operations.go`: `GetAllLabels` (the `for label := range gs.nodesByLabel` reader ~line 131), `GetNodesByLabel` (~line 148), the edge-type reader (~line 221).
- `pkg/storage/tenant_operations.go`: `GetNodesByLabelForTenant` (~153/187), `GetEdgesByTypeForTenant` (~201), `GetAllNodesForTenant` (~240), `GetAllEdgesForTenant` (~279), and the all-tenant iterators (~338/343).
- `pkg/storage/pagination.go`: the four readers at ~73, ~107, ~143, ~177.

Pattern for each (example, `GetNodesByLabelForTenant`):

```go
func (gs *GraphStorage) GetNodesByLabelForTenant(tenantID, label string) []*Node {
	gs.ensureMembershipBuilt()
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	// ... unchanged ...
}
```

> If any of these methods already takes `gs.mu.Lock`/`RLock` at a non-top line, place `ensureMembershipBuilt()` before that acquisition. It must never be called while holding `gs.mu` (it acquires the lock itself → deadlock).

- [ ] **Step 8: Run the membership + adjacency + full mmap suite, with race**

Run: `go test ./pkg/storage/ -run 'TestMmapStage2|TestMmap' -count=1 -timeout 240s`
Expected: PASS.
Run: `go test -race ./pkg/storage/ -run 'TestMmapStage2_LazyMembershipParity' -count=3 -timeout 240s`
Expected: PASS, race-clean (validates the `ensureMembershipBuilt` guard under concurrency).

- [ ] **Step 9: Full storage suite (regression) + commit**

Run: `go test ./pkg/storage/ -short -timeout 300s -count=1`
Expected: PASS (JSON path unaffected; mmap helpers no-op when `mmapSnap == nil`).

```bash
git add pkg/storage/membership_lazy.go pkg/storage/storage_types.go pkg/storage/mmap_snapshot_writer.go pkg/storage/mmap_snapshot_loader.go pkg/storage/tenant_operations.go pkg/storage/query_operations.go pkg/storage/pagination.go pkg/storage/mmap_reopen_test.go
git commit -m "feat(storage): lazy-build mmap membership indexes + persist tenant counts (Stage 2a)"
```

---

## Task 5: Clean profiler marks, measure, document the result

**Files:**
- Modify: `pkg/storage/mmap_snapshot_loader.go` (single-loop profiler marks)
- Modify: `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` (append Stage 2a result)

- [ ] **Step 1: Wire the profiler into the loader (clean, single-pass)**

In `pkg/storage/mmap_snapshot_loader.go`, at the top of `loadFromDiskMmap`:

```go
	prof := newLoadProfiler()
```

After `openMmapSnapshot` succeeds: `prof.mark("mmap open+CRC")`. After sticky keys: `prof.mark("sticky keys")`. After property/vector defs: `prof.mark("property+vector defs")`. Before the final `return nil`: `prof.mark("nextIDs+stats+tenantStats")` then `prof.report()`. (No membership/adjacency marks remain — both are gone from open. This is permanent, zero-overhead-when-disabled diagnostics, matching `loadFromDisk`.)

- [ ] **Step 2: Build + run the full-scale end-to-end bench with profiling**

Run:
```bash
GRAPHDB_REOPEN_BENCH=1 GRAPHDB_LOAD_PROFILE=1 \
  go test ./pkg/storage/ -run TestMmapReopen_EndToEnd -count=1 -timeout 600s -v 2>&1 | tail -25
```
Expected: PASS; the mmap reopen line should now be a small fraction of the prior 2.8s (open dominated by `mmap open+CRC` ~3ms; membership/adjacency absent). Record the actual `mmap reopen` time and `reopen/rebuild` ratio.

- [ ] **Step 3: Append the result to the spike doc**

Add a section `## Stage 2a — RESULT (2026-06-17)` to `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` with: the before (2.8s, membership 74% / adjacency 26%) → after numbers from Step 2; one line on the approach (CSR adjacency + lazy membership + persisted tenant counts); and a note that Stage 2b (persist membership) remains the open item for the first-enumeration latency.

- [ ] **Step 4: Lint + vet + commit**

Run: `go vet ./pkg/storage/ && gofmt -l pkg/storage/ && golangci-lint run ./pkg/storage/...`
Expected: clean (no output from `gofmt -l`; lint passes).

```bash
git add pkg/storage/mmap_snapshot_loader.go docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md
git commit -m "feat(storage): profiler marks for mmap loader + Stage 2a result (Stage 2a)"
```

---

## Final verification (before PR)

- [ ] `go build ./... && go vet ./...` — clean.
- [ ] `go test ./pkg/storage/ -short -timeout 300s -count=1` — PASS.
- [ ] `go test -race ./pkg/storage/ -run 'TestMmap' -count=3 -timeout 300s` — race-clean.
- [ ] `golangci-lint run ./...` — passes (per CLAUDE.md, cap is "same issue × 3"; may need 1-2 follow-up runs).
- [ ] Confirm JSON-mode parity is untouched: every new mmap helper no-ops when `mmapSnap == nil`.
- [ ] `/review` then `/preflight` per the user's global workflow, then open the PR.

## Notes for the implementer

- **Off-by-default invariant:** every change must be inert when `UseMmapSnapshot` is false / `mmapSnap == nil`. The JSON `loadFromDisk` path and on-disk JSON snapshot format are NOT touched.
- **No `checkGraphInvariants` in mmap mode:** it assumes shards hold every node, which the lazy representation breaks. Correctness is asserted via public-interface parity vs JSON only.
- **Lock discipline:** `ensureMembershipBuilt` acquires `gs.mu` — never call it while already holding `gs.mu`. The adjacency merge in `getEdgeIDsForNode` runs under the caller's existing lock (it already did); `isEdgeDeletedLocked` requires that lock.
- **CSR is immutable base; deltas live in the overlay maps + tombstone sets** — do not write back into the mapping.
