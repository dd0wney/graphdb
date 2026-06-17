# mmap reopen Stage 2b — Persist Membership Indexes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist the membership inverted indexes (per-tenant node/edge enumeration + by-label/by-type) as mmap-native sorted-ID runs so the first enumeration query after an mmap reopen is ~0 instead of triggering the Stage-2a lazy build.

**Architecture:** Add a membership section to `snapshot.mmap` (format v3→v4): four per-tenant inverted indexes encoded as sorted-ID runs (reusing the 2a `appendCSRRun`/`readCSRRun` codec) plus a string-keyed directory. Enumeration readers route through `…Locked` accessor helpers that merge *base run − tombstones ∪ overlay* (mirroring `getEdgeIDsForNode`). This replaces the 2a lazy build (`membership_lazy.go`), which is deleted.

**Tech Stack:** Go, `pkg/storage`. Builds on Stage 2a (CSR codec, v3 format, tenant-count metadata). Off-by-default (`mmapSnap == nil` → byte-identical JSON behavior). Correctness gate = public-interface parity vs JSON (`mmap_reopen_test.go`).

**Spec:** `docs/superpowers/specs/2026-06-17-mmap-stage2b-persist-membership-design.md`

**Branch:** `feat/mmap-stage2b-persist-membership`, stacked on `feat/mmap-stage2-derived-indexes` (2a). Rebase onto main once 2a (PR #412) merges.

---

## Key existing facts (verified)

- `effectiveTenantID(string) tenantid.TenantID`; `tenantid.TenantID` is `type TenantID string`.
- `gs.tenantNodesByLabel map[tenantid.TenantID]labelIndex` where `labelIndex = map[string]map[uint64]struct{}`. `gs.tenantNodeIDs map[tenantid.TenantID]map[uint64]struct{}`. Edge equivalents: `gs.tenantEdgesByType`, `gs.tenantEdgeIDs`.
- `sortedBucketIDs(bucket map[uint64]struct{}) []uint64` exists (ascending).
- `gs.isNodeDeletedLocked(id) bool` / `gs.isEdgeDeletedLocked(id) bool` — tombstone checks; no-op (false) when `mmapSnap == nil`. Caller holds a lock.
- `gs.mmapSnap *mmapSnapshot`; `gs.mmapSnap.metadata() *mmapMetadata`; `meta.TenantStats map[string]TenantStats` (the persisted tenant list).
- From 2a: `appendCSRRun(buf, ids) []byte`, `readCSRRun(buf, p) ([]uint64, int)`; header offset constants `hOutCSR/hInCSR/hAdjDir/hCRC/mmapHeaderSize`; `computeCRC(headerNoCRC, nodeDir, edgeDir, adjDir, meta)`; `adjDirEntrySize`.
- Writer's `writeMmapSnapshotData(path, nodes, edges, meta)` writes nodes (sorted asc by ID) then edges (sorted asc) — so appending IDs to per-key buckets in iteration order yields **already-sorted** runs (no per-bucket sort needed).
- Enumeration readers currently start with `gs.ensureMembershipBuilt()` then take `gs.mu.RLock`.

## File Structure

- `pkg/storage/mmap_snapshot_format.go` — MODIFY: version 3→4; header `membDirOffset`/`membDataOffset`; membership-kind constants; membership directory encode + entry codec.
- `pkg/storage/mmap_snapshot_writer.go` — MODIFY: build the four inverted indexes from the sorted nodes/edges, emit runs + directory.
- `pkg/storage/mmap_snapshot_reader.go` — MODIFY: parse membership section; `membershipRun(kind, tenant, name) []uint64` (binary search + `readCSRRun`); `membershipKeys(kind, tenant) []string`; `tenantList() []string`; extend CRC/sections.
- `pkg/storage/membership_index.go` — CREATE: the `…Locked` accessor helpers (merge base+overlay−tombstones; JSON-mode fast path).
- `pkg/storage/membership_lazy.go` — DELETE (superseded).
- `pkg/storage/storage_types.go` — MODIFY: remove `membershipBuilt` field.
- `pkg/storage/tenant_operations.go`, `pkg/storage/query_operations.go`, `pkg/storage/pagination.go`, `pkg/storage/node_operations.go` — MODIFY: rewire readers to accessors; drop `ensureMembershipBuilt()` calls.
- `pkg/storage/mmap_snapshot_loader.go` — MODIFY: (no membership build already, post-2a) — no functional change beyond removing any dangling reference.
- Tests: `pkg/storage/mmap_snapshot_format_test.go`, `pkg/storage/mmap_reopen_test.go`, `pkg/storage/mmap_snapshot_bench_test.go`.
- `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` — MODIFY: append Stage 2b result.

---

## Task 1: Membership directory + kind constants (additive)

Additive only — no version bump, no wiring. Stays green.

**Files:** Modify `pkg/storage/mmap_snapshot_format.go`; Test `pkg/storage/mmap_snapshot_format_test.go`.

- [ ] **Step 1: Write the failing test**

```go
func TestMembershipDirectory_RoundTrip(t *testing.T) {
	// Build a directory with three buckets across two kinds, then look them up.
	b := newMembershipBuilder()
	b.add(membKindNodeTenant, "t1", "", []uint64{1, 2, 3})
	b.add(membKindNodeLabel, "t1", "Alpha", []uint64{1, 3})
	b.add(membKindEdgeType, "t1", "LINK", []uint64{10})

	data, dir := b.encode(0) // base offset 0 for the run-data section
	d, err := parseMembershipDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	get := func(kind byte, tenant, name string) []uint64 {
		off, ln, ok := d.lookup(kind, tenant, name)
		if !ok {
			return nil
		}
		ids, _ := readCSRRun(data, int(off))
		_ = ln
		return ids
	}
	eq := func(name string, got, want []uint64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v want %v", name, got, want)
			}
		}
	}
	eq("nodeTenant", get(membKindNodeTenant, "t1", ""), []uint64{1, 2, 3})
	eq("nodeLabel", get(membKindNodeLabel, "t1", "Alpha"), []uint64{1, 3})
	eq("edgeType", get(membKindEdgeType, "t1", "LINK"), []uint64{10})
	if _, _, ok := d.lookup(membKindNodeLabel, "t1", "Missing"); ok {
		t.Error("missing key should not be found")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/storage/ -run TestMembershipDirectory_RoundTrip -count=1`
Expected: FAIL — undefined `newMembershipBuilder`, `membKind*`, `parseMembershipDir`.

- [ ] **Step 3: Implement the membership directory codec**

Add to `pkg/storage/mmap_snapshot_format.go`:

```go
// Membership index kinds (graphdb ask #1, Stage 2b). Each kind maps a composite
// (tenant[,name]) key to a sorted []uint64 run.
const (
	membKindNodeTenant byte = 0 // key: tenant        -> all node IDs in tenant
	membKindNodeLabel  byte = 1 // key: tenant,label   -> node IDs with label
	membKindEdgeTenant byte = 2 // key: tenant         -> all edge IDs in tenant
	membKindEdgeType   byte = 3 // key: tenant,type    -> edge IDs of type
)

// membFullKey encodes a directory key as kind ++ tenant ++ 0x00 ++ name. name is
// empty for the tenant-enumeration kinds (0, 2).
func membFullKey(kind byte, tenant, name string) []byte {
	k := make([]byte, 0, 1+len(tenant)+1+len(name))
	k = append(k, kind)
	k = append(k, tenant...)
	k = append(k, 0x00)
	k = append(k, name...)
	return k
}

// membershipBuilder accumulates (kind,key)->[]uint64 buckets in insertion order
// (callers append IDs in ascending order, yielding sorted runs).
type membershipBuilder struct {
	order []string            // full-key string, in insertion order
	runs  map[string][]uint64 // full-key string -> IDs
}

func newMembershipBuilder() *membershipBuilder {
	return &membershipBuilder{runs: make(map[string][]uint64)}
}

func (b *membershipBuilder) add(kind byte, tenant, name string, ids ...uint64) {
	key := string(membFullKey(kind, tenant, name))
	if _, ok := b.runs[key]; !ok {
		b.order = append(b.order, key)
	}
	b.runs[key] = append(b.runs[key], ids...)
}

// encode returns (runData, directory). Run offsets in the directory are absolute:
// baseOffset is the file offset where runData will be written. The directory is
// sorted by full-key bytes for binary search at read.
func (b *membershipBuilder) encode(baseOffset int64) (runData, directory []byte) {
	sort.Strings(b.order)
	type ent struct {
		key      string
		off, ln  int64
	}
	ents := make([]ent, 0, len(b.order))
	offset := baseOffset
	for _, key := range b.order {
		ids := b.runs[key]
		rec := appendCSRRun(nil, ids)
		ents = append(ents, ent{key: key, off: offset, ln: int64(len(ids))})
		runData = append(runData, rec...)
		offset += int64(len(rec))
	}
	// Directory: count(4), then per entry keyLen(2)|key|runOff(8)|runLen(8).
	directory = binary.LittleEndian.AppendUint32(directory, uint32(len(ents)))
	for _, e := range ents {
		directory = binary.LittleEndian.AppendUint16(directory, uint16(len(e.key)))
		directory = append(directory, e.key...)
		directory = binary.LittleEndian.AppendUint64(directory, uint64(e.off))
		directory = binary.LittleEndian.AppendUint64(directory, uint64(e.ln))
	}
	return runData, directory
}

// membershipDir is the parsed, lookup-ready directory (sorted full-keys).
type membershipDir struct {
	keys []string
	offs []int64
	lens []int64
}

func parseMembershipDir(b []byte) (*membershipDir, error) {
	if len(b) == 0 {
		return &membershipDir{}, nil
	}
	if len(b) < 4 {
		return nil, fmt.Errorf("membership directory truncated")
	}
	n := int(binary.LittleEndian.Uint32(b))
	p := 4
	d := &membershipDir{keys: make([]string, n), offs: make([]int64, n), lens: make([]int64, n)}
	for i := 0; i < n; i++ {
		if p+2 > len(b) {
			return nil, fmt.Errorf("membership directory truncated at entry %d", i)
		}
		kl := int(binary.LittleEndian.Uint16(b[p:]))
		p += 2
		if p+kl+16 > len(b) {
			return nil, fmt.Errorf("membership directory truncated at entry %d", i)
		}
		d.keys[i] = string(b[p : p+kl])
		p += kl
		d.offs[i] = int64(binary.LittleEndian.Uint64(b[p:]))
		p += 8
		d.lens[i] = int64(binary.LittleEndian.Uint64(b[p:]))
		p += 8
	}
	return d, nil
}

// lookup binary-searches for (kind,tenant,name); returns (runOffset, runLen, ok).
func (d *membershipDir) lookup(kind byte, tenant, name string) (int64, int64, bool) {
	target := string(membFullKey(kind, tenant, name))
	i := sort.SearchStrings(d.keys, target)
	if i < len(d.keys) && d.keys[i] == target {
		return d.offs[i], d.lens[i], true
	}
	return 0, 0, false
}

// keysForKindTenant returns the `name` component of every directory key matching
// (kind, tenant) — used by the label/type-key readers.
func (d *membershipDir) keysForKindTenant(kind byte, tenant string) []string {
	prefix := string(membFullKey(kind, tenant, "")) // kind ++ tenant ++ 0x00
	var out []string
	i := sort.SearchStrings(d.keys, prefix)
	for ; i < len(d.keys); i++ {
		if !strings.HasPrefix(d.keys[i], prefix) {
			break
		}
		out = append(out, d.keys[i][len(prefix):])
	}
	return out
}
```

Add `"sort"` and `"strings"` to the file's imports if not present (it already imports `encoding/binary`, `fmt`).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./pkg/storage/ -run TestMembershipDirectory_RoundTrip -count=1`
Expected: PASS.

- [ ] **Step 5: Verify + commit**

Run: `go build ./pkg/storage/ && go test ./pkg/storage/ -run 'TestMmap|TestMembership|TestCSR' -count=1 -timeout 180s` (PASS — additive). `gofmt -l` clean.

```bash
git add pkg/storage/mmap_snapshot_format.go pkg/storage/mmap_snapshot_format_test.go
git commit -m "feat(storage): mmap membership directory codec + kind constants (Stage 2b)"
```

---

## Task 2: Persist the membership section in the file (format v4)

One logical change spanning format header + writer + reader so the file stays valid and green. Readers still use the 2a lazy build (ignore the new section) → green.

**Files:** Modify `mmap_snapshot_format.go` (header, version, CRC), `mmap_snapshot_writer.go` (emit), `mmap_snapshot_reader.go` (parse + accessors). Test: `mmap_snapshot_format_test.go`.

- [ ] **Step 1: Write the failing test**

```go
func TestMmapSnapshot_MembershipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.mmap")
	nodes := []*Node{
		{ID: 1, TenantID: "t", Labels: []string{"Alpha"}},
		{ID: 2, TenantID: "t", Labels: []string{"Beta"}},
		{ID: 3, TenantID: "t", Labels: []string{"Alpha"}},
	}
	edges := []*Edge{
		{ID: 10, TenantID: "t", FromNodeID: 1, ToNodeID: 2, Type: "LINK"},
		{ID: 11, TenantID: "t", FromNodeID: 2, ToNodeID: 3, Type: "REF"},
	}
	if err := writeMmapSnapshotData(path, nodes, edges, &mmapMetadata{}); err != nil {
		t.Fatal(err)
	}
	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.close()

	eq := func(name string, got, want []uint64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v want %v", name, got, want)
			}
		}
	}
	eq("nodeTenant", snap.membershipRun(membKindNodeTenant, "t", ""), []uint64{1, 2, 3})
	eq("alpha", snap.membershipRun(membKindNodeLabel, "t", "Alpha"), []uint64{1, 3})
	eq("beta", snap.membershipRun(membKindNodeLabel, "t", "Beta"), []uint64{2})
	eq("edgeTenant", snap.membershipRun(membKindEdgeTenant, "t", ""), []uint64{10, 11})
	eq("link", snap.membershipRun(membKindEdgeType, "t", "LINK"), []uint64{10})
	if got := snap.membershipRun(membKindNodeLabel, "t", "Missing"); got != nil {
		t.Errorf("missing label run = %v want nil", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/storage/ -run TestMmapSnapshot_MembershipRoundTrip -count=1`
Expected: FAIL — `snap.membershipRun` undefined.

- [ ] **Step 3: Header fields + version bump (mmap_snapshot_format.go)**

Bump version and extend the header (the 2a header ended at hCRC=116/size=124; insert the two membership offsets before hCRC and grow the header):

```go
	mmapSnapshotVersion uint32 = 4 // v4 adds the membership section
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
	hOutCSR        = 92
	hInCSR         = 100
	hAdjDir        = 108
	hMembData      = 116 // membership run-data section offset
	hMembDir       = 124 // membership directory offset
	hMembDirLen    = 132 // membership directory length in bytes
	hCRC           = 140
	mmapHeaderSize = 148 // hCRC(4) + pad(4) -> 148
```

Add to `mmapSnapshotHeader` (after `adjDirOffset`):
```go
	membDataOffset uint64
	membDirOffset  uint64
	membDirLen     uint64
```
In `marshal()` (after the `hAdjDir` write, before `hCRC`):
```go
	binary.LittleEndian.PutUint64(b[hMembData:], h.membDataOffset)
	binary.LittleEndian.PutUint64(b[hMembDir:], h.membDirOffset)
	binary.LittleEndian.PutUint64(b[hMembDirLen:], h.membDirLen)
```
In `unmarshalMmapHeader()` add:
```go
		membDataOffset: binary.LittleEndian.Uint64(b[hMembData:]),
		membDirOffset:  binary.LittleEndian.Uint64(b[hMembDir:]),
		membDirLen:     binary.LittleEndian.Uint64(b[hMembDirLen:]),
```
Change `computeCRC` to add the membership directory:
```go
func computeCRC(headerNoCRC, nodeDir, edgeDir, adjDir, membDir, meta []byte) uint32 {
	h := crc32.NewIEEE()
	h.Write(headerNoCRC)
	h.Write(nodeDir)
	h.Write(edgeDir)
	h.Write(adjDir)
	h.Write(membDir)
	h.Write(meta)
	return h.Sum32()
}
```

- [ ] **Step 4: Writer emits the membership section (mmap_snapshot_writer.go)**

In `writeMmapSnapshotData`, AFTER the CSR adjacency block and BEFORE `hdr.metaOffset = uint64(offset)`, insert:

```go
	// Membership inverted indexes (Stage 2b): per-tenant node/edge enumeration +
	// by-label/by-type. Built by appending IDs in the (already ascending) node/edge
	// iteration order, so each run is sorted without an extra sort.
	mb := newMembershipBuilder()
	for _, n := range nodes {
		t := string(effectiveTenantID(n.TenantID))
		mb.add(membKindNodeTenant, t, "", n.ID)
		for _, label := range n.Labels {
			mb.add(membKindNodeLabel, t, label, n.ID)
		}
	}
	for _, e := range edges {
		t := string(effectiveTenantID(e.TenantID))
		mb.add(membKindEdgeTenant, t, "", e.ID)
		mb.add(membKindEdgeType, t, e.Type, e.ID)
	}
	membRunData, membDirBytes := mb.encode(offset)
	hdr.membDataOffset = uint64(offset)
	if _, err := w.Write(membRunData); err != nil {
		return err
	}
	offset += int64(len(membRunData))
	hdr.membDirOffset = uint64(offset)
	hdr.membDirLen = uint64(len(membDirBytes))
	if _, err := w.Write(membDirBytes); err != nil {
		return err
	}
	offset += int64(len(membDirBytes))
```

Update the writer's CRC call to pass `membDirBytes`:
```go
	hdr.crc = computeCRC(hdr.marshal()[:hCRC], nodeDirBytes, edgeDirBytes, adjDirBytes, membDirBytes, metaBytes)
```
(Declare `var membDirBytes []byte` alongside `var adjDirBytes []byte` near the top if the block is conditional; here it is unconditional, so the `:=` above defines it before the CRC line — ensure the CRC line is after the membership block. If `membDirBytes` is out of scope at the CRC line, hoist its declaration: add `var membRunData, membDirBytes []byte` before the membership block and use `=` from `mb.encode`.)

- [ ] **Step 5: Reader parses + accessors (mmap_snapshot_reader.go)**

Extend `sections` to return the membership directory bytes and bounds-check, and parse the directory at open. In `openMmapSnapshot`, after the existing CRC check passes and `meta` is parsed, add the membership directory parse. First change `sections` signature to also return `membDir`:

```go
func sections(data []byte, hdr *mmapSnapshotHeader) (nodeDir, edgeDir, adjDir, membDir, meta []byte, err error) {
	size := uint64(len(data))
	nodeDirEnd := hdr.nodeDirOffset + hdr.nodeDirLen()*8
	edgeDirEnd := hdr.edgeDirOffset + hdr.edgeDirLen()*8
	adjDirEnd := hdr.adjDirOffset + hdr.nodeDirLen()*adjDirEntrySize
	membDirEnd := hdr.membDirOffset + hdr.membDirLen
	metaEnd := hdr.metaOffset + hdr.metaLen
	if hdr.nodeCount > 0 && (nodeDirEnd > size || adjDirEnd > size) ||
		hdr.edgeCount > 0 && edgeDirEnd > size ||
		membDirEnd > size || metaEnd > size {
		return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot section out of bounds (size %d)", size)
	}
	if hdr.nodeCount > 0 {
		nodeDir = data[hdr.nodeDirOffset:nodeDirEnd]
		adjDir = data[hdr.adjDirOffset:adjDirEnd]
	}
	if hdr.edgeCount > 0 {
		edgeDir = data[hdr.edgeDirOffset:edgeDirEnd]
	}
	if hdr.membDirLen > 0 {
		membDir = data[hdr.membDirOffset:membDirEnd]
	}
	meta = data[hdr.metaOffset:metaEnd]
	return nodeDir, edgeDir, adjDir, membDir, meta, nil
}
```

Update the caller in `openMmapSnapshot`:
```go
	nodeDir, edgeDir, adjDir, membDir, metaBytes, err := sections(data, hdr)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}
	if got := computeCRC(data[:hCRC], nodeDir, edgeDir, adjDir, membDir, metaBytes); got != hdr.crc {
		_ = syscall.Munmap(data)
		return nil, fmt.Errorf("mmap snapshot %q CRC mismatch: got %08x want %08x", path, got, hdr.crc)
	}
```
Add a `membDir *membershipDir` field to the `mmapSnapshot` struct, and parse it after the meta parse:
```go
	mdir, err := parseMembershipDir(membDir)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}
	return &mmapSnapshot{data: data, hdr: hdr, meta: meta, membDir: mdir}, nil
```
(Update the `mmapSnapshot` struct literal accordingly; add `membDir *membershipDir` to the struct definition.)

Add accessors at the end of the file:
```go
// membershipRun returns the persisted sorted ID run for (kind,tenant,name), or nil.
func (m *mmapSnapshot) membershipRun(kind byte, tenant, name string) []uint64 {
	if m.membDir == nil {
		return nil
	}
	off, ln, ok := m.membDir.lookup(kind, tenant, name)
	if !ok || ln == 0 {
		return nil
	}
	ids, _ := readCSRRun(m.data, int(off))
	return ids
}

// membershipKeys returns the `name` components present for (kind,tenant).
func (m *mmapSnapshot) membershipKeys(kind byte, tenant string) []string {
	if m.membDir == nil {
		return nil
	}
	return m.membDir.keysForKindTenant(kind, tenant)
}

// tenantList returns every tenant ID known to the snapshot (from persisted stats).
func (m *mmapSnapshot) tenantList() []string {
	out := make([]string, 0, len(m.meta.TenantStats))
	for t := range m.meta.TenantStats {
		out = append(out, t)
	}
	return out
}
```

- [ ] **Step 6: Run round-trip + full mmap suite**

Run: `go test ./pkg/storage/ -run 'TestMmapSnapshot_MembershipRoundTrip|TestMmap' -count=1 -timeout 180s` — PASS. `go build ./pkg/storage/`, `gofmt -l` clean.

- [ ] **Step 7: Commit**

```bash
git add pkg/storage/mmap_snapshot_format.go pkg/storage/mmap_snapshot_writer.go pkg/storage/mmap_snapshot_reader.go pkg/storage/mmap_snapshot_format_test.go
git commit -m "feat(storage): persist membership section in mmap snapshot v4 (Stage 2b)"
```

---

## Task 3: Membership accessor helpers (merge base+overlay−tombstones)

Add the `…Locked` accessors. Not yet wired into readers — the 2a build still active → green. The accessors are unit-tested directly through a reopen.

**Files:** Create `pkg/storage/membership_index.go`; Test: `pkg/storage/mmap_reopen_test.go`.

- [ ] **Step 1: Write the failing test**

```go
func TestMmapStage2b_MembershipAccessors(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	mk := func(label string) uint64 {
		n, err := gs.CreateNodeWithTenant(tenant, []string{label}, map[string]Value{})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	a1, a2 := mk("Alpha"), mk("Alpha")
	mk("Beta")
	if err := gs.Close(); err != nil {
		t.Fatal(err)
	}

	mr, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	tid := effectiveTenantID(tenant)
	read := func(fn func() []uint64) []uint64 {
		mr.mu.RLock()
		defer mr.mu.RUnlock()
		return fn()
	}
	// Base run, no prior enumeration / no lazy build.
	if got := read(func() []uint64 { return mr.membershipNodeIDsByLabelLocked(tid, "Alpha") }); len(got) != 2 {
		t.Errorf("Alpha base=%d want 2", len(got))
	}
	if got := read(func() []uint64 { return mr.membershipNodeIDsForTenantLocked(tid) }); len(got) != 3 {
		t.Errorf("tenant-all base=%d want 3", len(got))
	}
	// Overlay add.
	a3, err := mr.CreateNodeWithTenant(tenant, []string{"Alpha"}, map[string]Value{})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(func() []uint64 { return mr.membershipNodeIDsByLabelLocked(tid, "Alpha") }); len(got) != 3 {
		t.Errorf("Alpha after add=%d want 3", len(got))
	}
	// Tombstone filter.
	if err := mr.DeleteNode(a1); err != nil {
		t.Fatal(err)
	}
	if got := read(func() []uint64 { return mr.membershipNodeIDsByLabelLocked(tid, "Alpha") }); len(got) != 2 {
		t.Errorf("Alpha after delete=%d want 2", len(got))
	}
	_ = a2
	_ = a3
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/storage/ -run TestMmapStage2b_MembershipAccessors -count=1`
Expected: FAIL — `membershipNodeIDsByLabelLocked` undefined.

- [ ] **Step 3: Implement the accessors (new file `pkg/storage/membership_index.go`)**

```go
package storage

// mmap-native membership accessors (graphdb ask #1, Stage 2b). Each returns the
// sorted ID set for a membership key, merging the persisted base run (minus
// tombstones) with the post-open overlay map. When mmapSnap == nil (JSON mode)
// each returns the in-memory map's set directly — byte-identical to pre-2b.
//
// Caller holds gs.mu (R or W). New post-open IDs are disjoint from base IDs, so
// the union needs no dedup.

import "sort"

// mergeBaseOverlay returns sorted (base − tombstoned) ∪ overlayIDs. deleted is the
// tombstone predicate (isNodeDeletedLocked / isEdgeDeletedLocked).
func mergeBaseOverlay(base []uint64, overlay map[uint64]struct{}, deleted func(uint64) bool) []uint64 {
	out := make([]uint64, 0, len(base)+len(overlay))
	for _, id := range base {
		if !deleted(id) {
			out = append(out, id)
		}
	}
	for id := range overlay {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (gs *GraphStorage) membershipNodeIDsForTenantLocked(tid tenantid.TenantID) []uint64 {
	overlay := gs.tenantNodeIDs[tid]
	if gs.mmapSnap == nil {
		return sortedBucketIDs(overlay)
	}
	base := gs.mmapSnap.membershipRun(membKindNodeTenant, string(tid), "")
	return mergeBaseOverlay(base, overlay, gs.isNodeDeletedLocked)
}

func (gs *GraphStorage) membershipNodeIDsByLabelLocked(tid tenantid.TenantID, label string) []uint64 {
	var overlay map[uint64]struct{}
	if lm := gs.tenantNodesByLabel[tid]; lm != nil {
		overlay = lm[label]
	}
	if gs.mmapSnap == nil {
		return sortedBucketIDs(overlay)
	}
	base := gs.mmapSnap.membershipRun(membKindNodeLabel, string(tid), label)
	return mergeBaseOverlay(base, overlay, gs.isNodeDeletedLocked)
}

func (gs *GraphStorage) membershipEdgeIDsForTenantLocked(tid tenantid.TenantID) []uint64 {
	overlay := gs.tenantEdgeIDs[tid]
	if gs.mmapSnap == nil {
		return sortedBucketIDs(overlay)
	}
	base := gs.mmapSnap.membershipRun(membKindEdgeTenant, string(tid), "")
	return mergeBaseOverlay(base, overlay, gs.isEdgeDeletedLocked)
}

func (gs *GraphStorage) membershipEdgeIDsByTypeLocked(tid tenantid.TenantID, etype string) []uint64 {
	var overlay map[uint64]struct{}
	if tm := gs.tenantEdgesByType[tid]; tm != nil {
		overlay = tm[etype]
	}
	if gs.mmapSnap == nil {
		return sortedBucketIDs(overlay)
	}
	base := gs.mmapSnap.membershipRun(membKindEdgeType, string(tid), etype)
	return mergeBaseOverlay(base, overlay, gs.isEdgeDeletedLocked)
}

// membershipTenantsLocked returns every tenant ID (base ∪ overlay).
func (gs *GraphStorage) membershipTenantsLocked() []tenantid.TenantID {
	seen := make(map[tenantid.TenantID]struct{})
	if gs.mmapSnap != nil {
		for _, t := range gs.mmapSnap.tenantList() {
			seen[tenantid.TenantID(t)] = struct{}{}
		}
	}
	for tid := range gs.tenantNodeIDs {
		seen[tid] = struct{}{}
	}
	for tid := range gs.tenantEdgeIDs {
		seen[tid] = struct{}{}
	}
	out := make([]tenantid.TenantID, 0, len(seen))
	for tid := range seen {
		out = append(out, tid)
	}
	return out
}

// membershipNodeIDsByLabelGlobalLocked unions kind-1 runs across all tenants.
func (gs *GraphStorage) membershipNodeIDsByLabelGlobalLocked(label string) []uint64 {
	if gs.mmapSnap == nil {
		return sortedBucketIDs(gs.nodesByLabel[label])
	}
	var all []uint64
	for _, tid := range gs.membershipTenantsLocked() {
		all = append(all, gs.membershipNodeIDsByLabelLocked(tid, label)...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	return all
}

// membershipEdgeIDsByTypeGlobalLocked unions kind-3 runs across all tenants.
func (gs *GraphStorage) membershipEdgeIDsByTypeGlobalLocked(etype string) []uint64 {
	if gs.mmapSnap == nil {
		return sortedBucketIDs(gs.edgesByType[etype])
	}
	var all []uint64
	for _, tid := range gs.membershipTenantsLocked() {
		all = append(all, gs.membershipEdgeIDsByTypeLocked(tid, etype)...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	return all
}

// membershipLabelsForTenantLocked returns the label keys present for a tenant
// (base directory ∪ overlay map keys).
func (gs *GraphStorage) membershipLabelsForTenantLocked(tid tenantid.TenantID) []string {
	seen := make(map[string]struct{})
	if gs.mmapSnap != nil {
		for _, k := range gs.mmapSnap.membershipKeys(membKindNodeLabel, string(tid)) {
			seen[k] = struct{}{}
		}
	}
	if lm := gs.tenantNodesByLabel[tid]; lm != nil {
		for k := range lm {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// membershipEdgeTypesForTenantLocked mirrors the above for edge types.
func (gs *GraphStorage) membershipEdgeTypesForTenantLocked(tid tenantid.TenantID) []string {
	seen := make(map[string]struct{})
	if gs.mmapSnap != nil {
		for _, k := range gs.mmapSnap.membershipKeys(membKindEdgeType, string(tid)) {
			seen[k] = struct{}{}
		}
	}
	if tm := gs.tenantEdgesByType[tid]; tm != nil {
		for k := range tm {
			seen[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
```

Add the import for `tenantid`: `"github.com/dd0wney/graphdb/pkg/tenantid"`. Confirm `sortedBucketIDs(nil)` returns an empty/nil slice safely (it does — ranging a nil map yields nothing); if not, guard for nil overlay.

- [ ] **Step 4: Run the accessor test + full mmap suite**

Run: `go test ./pkg/storage/ -run 'TestMmapStage2b_MembershipAccessors|TestMmap' -count=1 -timeout 180s` — PASS.
Run: `go test -race ./pkg/storage/ -run TestMmapStage2b_MembershipAccessors -count=3 -timeout 180s` — PASS.
`gofmt -l` clean.

- [ ] **Step 5: Commit**

```bash
git add pkg/storage/membership_index.go pkg/storage/mmap_reopen_test.go
git commit -m "feat(storage): mmap membership accessors (base+overlay-tombstones) (Stage 2b)"
```

---

## Task 4: Rewire readers to accessors; delete the 2a lazy build

The big integration task. Replace each enumeration reader's membership-map read with the accessor; remove every `gs.ensureMembershipBuilt()` call; delete `membership_lazy.go` and the `membershipBuilt` field.

**Files:** Modify `tenant_operations.go`, `query_operations.go`, `pagination.go`, `node_operations.go`; Delete `membership_lazy.go`; Modify `storage_types.go`. Test: `mmap_reopen_test.go`.

- [ ] **Step 1: Write the failing parity test (the whole point of 2b)**

```go
func TestMmapStage2b_EnumerationAtOpenNoBuild(t *testing.T) {
	dir := t.TempDir()
	const tenant = "t"
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	// 3 Alpha (i=0,2,4), 3 Beta (i=1,3,5); 6 total.
	for i := 0; i < 6; i++ {
		lbl := "Alpha"
		if i%2 == 1 {
			lbl = "Beta"
		}
		if _, err := gs.CreateNodeWithTenant(tenant, []string{lbl}, map[string]Value{}); err != nil {
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

	// Enumerate with NO prior call — results must come from the persisted section,
	// not a lazy build (which no longer exists).
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Alpha")); got != 3 {
		t.Errorf("Alpha=%d want 3", got)
	}
	if got := len(mr.GetNodesByLabelForTenant(tenant, "Beta")); got != 3 {
		t.Errorf("Beta=%d want 3", got)
	}
	if got := len(mr.GetAllNodesForTenant(tenant)); got != 6 {
		t.Errorf("all-nodes=%d want 6", got)
	}
}
```
(Exact-count assertions keep this test self-contained — no JSON-config helper needed. Broad JSON-vs-mmap parity across the rewired readers is covered by the full `-short` suite in Step 5, which runs the existing parity tests.)

- [ ] **Step 2: Run to verify it fails or passes for the wrong reason**

Run: `go test ./pkg/storage/ -run TestMmapStage2b_EnumerationParityNoBuild -count=1`
Expected: PASS currently (the 2a lazy build still fires inside the readers). It becomes the real guard once the build is removed. Proceed.

- [ ] **Step 3: Rewire the readers (apply the per-site swap)**

For EACH method below: remove the leading `gs.ensureMembershipBuilt()` line, and replace the membership-map read with the accessor call (which returns an already-sorted `[]uint64`). The accessor must be called UNDER `gs.mu.RLock` (callers already hold it). Worked examples:

`tenant_operations.go` — `GetNodesByLabelForTenant` becomes:
```go
func (gs *GraphStorage) GetNodesByLabelForTenant(tenantID, label string) []*Node {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	tid := effectiveTenantID(tenantID)
	nodeIDs := gs.membershipNodeIDsByLabelLocked(tid, label)
	nodes := make([]*Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if node, exists := gs.resolveNodeRefLocked(id); exists {
			nodes = append(nodes, node.Clone())
		}
	}
	return nodes
}
```

`tenant_operations.go` — `GetAllNodesForTenant` becomes (collect IDs under RLock via the accessor, release, then resolve under shard locks):
```go
func (gs *GraphStorage) GetAllNodesForTenant(tenantID string) []*Node {
	tid := effectiveTenantID(tenantID)
	gs.mu.RLock()
	ids := gs.membershipNodeIDsForTenantLocked(tid)
	gs.mu.RUnlock()
	nodes := make([]*Node, 0, len(ids))
	for _, id := range ids {
		gs.rlockShard(id)
		node, exists := gs.resolveNodeRefLocked(id)
		if exists {
			node = node.Clone()
		}
		gs.runlockShard(id)
		if exists {
			nodes = append(nodes, node)
		}
	}
	return nodes
}
```

Apply the analogous swap to every site (use the matching accessor; keep each method's existing lock/resolve structure, only swapping the ID-collection):

| File | Method | Accessor |
|---|---|---|
| tenant_operations.go | `GetNodesByLabelForTenant` | `membershipNodeIDsByLabelLocked(tid, label)` |
| tenant_operations.go | `CountNodesByLabelForTenant` | `len(membershipNodeIDsByLabelLocked(tid, label))` |
| tenant_operations.go | `GetEdgesByTypeForTenant` | `membershipEdgeIDsByTypeLocked(tid, etype)` |
| tenant_operations.go | `GetAllNodesForTenant` | `membershipNodeIDsForTenantLocked(tid)` |
| tenant_operations.go | `GetAllEdgesForTenant` | `membershipEdgeIDsForTenantLocked(tid)` |
| tenant_operations.go | `GetLabelsForTenant` | `membershipLabelsForTenantLocked(tid)` |
| tenant_operations.go | `GetEdgeTypesForTenant` | `membershipEdgeTypesForTenantLocked(tid)` |
| tenant_operations.go | `ListTenants` | `membershipTenantsLocked()` |
| pagination.go | `NodesPageForTenant` | `membershipNodeIDsForTenantLocked(tid)` (already-sorted; drop the local sort) |
| pagination.go | `NodesByLabelPageForTenant` | `membershipNodeIDsByLabelLocked(tid, label)` (drop the nil/empty guards — accessor returns nil for missing; keep the `if len(ids)==0 { return nil, 0 }` early return) |
| pagination.go | `EdgesPageForTenant` | `membershipEdgeIDsForTenantLocked(tid)` |
| pagination.go | `EdgesByTypePageForTenant` | `membershipEdgeIDsByTypeLocked(tid, etype)` |
| query_operations.go | `FindNodesByLabelAcrossTenants` | `membershipNodeIDsByLabelGlobalLocked(label)` |
| query_operations.go | `FindEdgesByTypeAcrossTenants` | `membershipEdgeIDsByTypeGlobalLocked(etype)` |
| query_operations.go | `GetAllLabels` | leave as-is BUT remove the `ensureMembershipBuilt()` line — it reads `gs.nodesByLabel` keys, which are sticky-registered at open; confirm parity (see note) |
| node_operations.go | `CreateNodeWithUniquePropertyForTenant` | replace the `gs.ensureMembershipBuilt()` + `gs.tenantNodesByLabel[tid][uniqueLabel]` scan loop with iterating `gs.membershipNodeIDsByLabelLocked(tid, uniqueLabel)` and resolving each to check the property |

> **`GetAllLabels` note:** it ranges `gs.nodesByLabel` for KEYS. In mmap mode the global label keys are registered as sticky buckets at open (`StickyNodeLabels`), so the keys are present without a build. Removing the `ensureMembershipBuilt()` call is correct; verify with a parity assertion if unsure. For per-tenant `GetLabelsForTenant`, use the `membershipLabelsForTenantLocked` accessor (sticky keys are global, not per-tenant).

> **`CreateNodeWithUniquePropertyForTenant`:** the 2a fix added `gs.ensureMembershipBuilt()` before `gs.mu.Lock()`. Remove that line. Inside the lock, replace `for existingID := range labelMap[uniqueLabel]` with `for _, existingID := range gs.membershipNodeIDsByLabelLocked(tid, uniqueLabel)` (the accessor works under the write lock too). Keep the rest of the scan (resolve + property compare + `UniqueConstraintError`) unchanged.

- [ ] **Step 4: Delete the 2a lazy build**

```bash
git rm pkg/storage/membership_lazy.go
```
In `pkg/storage/storage_types.go`, remove the `membershipBuilt bool` field and its comment block. Then `grep -rn "ensureMembershipBuilt\|membershipBuilt\|addNodeToTenantIndexNoCount\|addEdgeToTenantIndexNoCount" pkg/storage/` and confirm ZERO remaining references (any left → fix the call site). The loader (`mmap_snapshot_loader.go`) had no build loop after 2a, so no change there beyond confirming no dangling reference.

- [ ] **Step 5: Build, parity, race, full suite**

- `go build ./pkg/storage/` — must compile (zero references to the deleted symbols).
- `go test ./pkg/storage/ -run 'TestMmapStage2|TestMmap' -count=1 -timeout 240s` — PASS.
- `go test -race ./pkg/storage/ -run 'TestMmapStage2b' -count=3 -timeout 240s` — PASS.
- `go test ./pkg/storage/ -short -timeout 300s -count=1` — PASS (the big regression net: all tenant/label/pagination/uniqueness paths now go through the accessors).

If any enumeration test fails in mmap mode (wrong counts), the most likely cause is a reader whose swap dropped a needed early-return or a tenant/label key mismatch — debug that specific method. If stuck, report BLOCKED with the failing test.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(storage): serve mmap membership from persisted section; drop lazy build (Stage 2b)"
```

---

## Task 5: Measure first-enumeration; document the result

**Files:** Test `pkg/storage/mmap_snapshot_bench_test.go`; Doc `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md`.

- [ ] **Step 1: Add a first-enumeration measurement to the end-to-end bench**

In `pkg/storage/mmap_snapshot_bench_test.go`, in `TestMmapReopen_EndToEnd`, after the mmap reopen timing and before the parity assertions, add a timed first-enumeration on the freshly-reopened mmap store (no prior enumeration):

```go
	te := time.Now()
	_ = mr.GetAllNodesForTenant(tenant)
	firstEnum := time.Since(te)
	fmt.Fprintf(os.Stderr, "  mmap first GetAllNodesForTenant  %8s\n", firstEnum.Round(time.Millisecond))
```
(Place it so it runs on `mr` before any other enumeration. The reopen-time assertion is unchanged.)

- [ ] **Step 2: Run the full-scale bench**

Run:
```bash
GRAPHDB_REOPEN_BENCH=1 go test ./pkg/storage/ -run TestMmapReopen_EndToEnd -count=1 -timeout 600s -v 2>&1 | tail -20
```
Record: mmap reopen wall (should still be ~ms) AND `mmap first GetAllNodesForTenant` (should now be ~ms — served from the persisted section, vs the ~2s Stage-2a lazy build). Capture the real numbers.

- [ ] **Step 3: Append the Stage 2b result to the spike doc**

Append a `## Stage 2b — RESULT (2026-06-17)` section to `docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md` with: one line on the approach (membership persisted as mmap-native sorted-ID runs, served by base+overlay−tombstone accessors, 2a lazy build removed); the measured reopen wall AND first-enumeration wall (before: ~2s lazy build → after: <FILL>ms); and a note that open + first-enumeration are now both ~0, closing ask #1's "~0.1s dream." Fill with the real Step-2 numbers.

- [ ] **Step 4: Lint/vet/format/commit**

- `go vet ./pkg/storage/ && gofmt -l pkg/storage/` — clean.
- `golangci-lint run ./pkg/storage/...` if available (may be unavailable due to a toolchain version gate — note it; CI runs the real lint).

```bash
git add pkg/storage/mmap_snapshot_bench_test.go docs/internals/design/SPIKE_REOPEN_COST_2026-06-16.md
git commit -m "feat(storage): measure first-enumeration + Stage 2b result (Stage 2b)"
```

---

## Final verification (before PR)

- [ ] `go build ./... && go vet ./...` — clean.
- [ ] `go test ./pkg/storage/ -short -timeout 300s -count=1` — PASS.
- [ ] `go test -race ./pkg/storage/ -run 'TestMmap' -count=3 -timeout 300s` — race-clean.
- [ ] `grep -rn "ensureMembershipBuilt\|membershipBuilt" pkg/` returns nothing (lazy build fully removed).
- [ ] JSON-mode parity untouched: every accessor's `mmapSnap == nil` branch returns the in-memory set as before.
- [ ] `/review` then `/preflight`, then open the PR (stacked on 2a — base it on `feat/mmap-stage2-derived-indexes` until 2a merges, then retarget to main).

## Notes for the implementer

- **Off-by-default:** every accessor's `mmapSnap == nil` branch must reproduce the prior in-memory-map behavior exactly. The JSON path and JSON snapshot format are untouched.
- **Lock discipline:** accessors are `…Locked` — callers hold `gs.mu` (R for reads, W for the unique-create). They read the overlay maps + tombstone sets (both gs.mu-guarded) and the immutable mmap base (no lock). Never take `gs.mu` inside an accessor.
- **Sorted runs for free:** the writer appends IDs in ascending node/edge iteration order, so runs need no per-bucket sort. The accessor's final `sort.Slice` handles the base+overlay merge ordering.
- **Disjointness:** post-open IDs > snapshot's NextNodeID/NextEdgeID, so base and overlay never overlap — no dedup needed.
- **Correctness gate:** public-interface parity vs JSON (`checkGraphInvariants` is incompatible with the lazy representation).
