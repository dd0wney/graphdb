package storage

// Reader for the mmap-able snapshot format. Maps the file read-only, verifies the CRC
// over the structural sections (header + directories + metadata) at open, and
// materializes nodes/edges lazily on access (copy-on-read, so results are safe to
// retain after close). The bytes come from a vfs driver: the OS driver mmaps
// (unix), and any driver may supply its own buffer, which is how fault and
// corruption tests drive this reader instead of a test-only substitute.

import (
	"encoding/binary"
	"fmt"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

type mmapSnapshot struct {
	data    []byte
	release func() error
	hdr     *mmapSnapshotHeader
	meta    *mmapMetadata
	membDir *membershipDir
}

func openMmapSnapshot(path string) (*mmapSnapshot, error) {
	return openMmapSnapshotWithFS(vfs.Default(), path)
}

// openMmapSnapshotWithFS maps the snapshot through a filesystem driver.
//
// The bytes come from vfs.MapFile, so the OS driver mmaps as before and a fault
// driver can hand back a buffer it chose. That is the whole point: a truncated
// or corrupt snapshot now reaches this function — the production reader — with
// no corrupt file ever written to disk. See ADR 0002 and vfs.Mapper.
func openMmapSnapshotWithFS(fs vfs.FileSystem, path string) (*mmapSnapshot, error) {
	data, release, err := vfs.MapFile(fs, path)
	if err != nil {
		return nil, err
	}
	if len(data) < mmapHeaderSize {
		_ = release()
		return nil, fmt.Errorf("mmap snapshot %q too small: %d bytes", path, len(data))
	}

	hdr, err := unmarshalMmapHeader(data)
	if err != nil {
		_ = release()
		return nil, err
	}

	nodeDir, edgeDir, adjDir, membDirBytes, metaBytes, err := sections(data, hdr)
	if err != nil {
		_ = release()
		return nil, err
	}
	if got := computeCRC(data[:hCRC], nodeDir, edgeDir, adjDir, membDirBytes, metaBytes); got != hdr.crc {
		_ = release()
		return nil, fmt.Errorf("mmap snapshot %q CRC mismatch: got %08x want %08x", path, got, hdr.crc)
	}
	// The ID range and the directory must agree. forEachNodeID/forEachEdgeID
	// iterate from minID to maxID and index the directory per step, so a header
	// claiming a range wider than its directory is not a slow read — it is an
	// unbounded one. FuzzMmapSnapshotCRCRepaired set the top byte of maxEdgeID
	// to 0xa0 and turned a 3-edge snapshot into 1.15e19 iterations. Adding a
	// bounds check inside dirEntry alone converted the panic into a hang, which
	// is the worse failure: a crash stops, a spin does not.
	if err := checkDirRange(hdr.nodeCount, hdr.minNodeID, hdr.maxNodeID, len(nodeDir), "node"); err != nil {
		_ = release()
		return nil, fmt.Errorf("mmap snapshot %q: %w", path, err)
	}
	if err := checkDirRange(hdr.edgeCount, hdr.minEdgeID, hdr.maxEdgeID, len(edgeDir), "edge"); err != nil {
		_ = release()
		return nil, fmt.Errorf("mmap snapshot %q: %w", path, err)
	}

	meta, err := unmarshalMmapMetadata(metaBytes)
	if err != nil {
		_ = release()
		return nil, err
	}
	mdir, err := parseMembershipDir(membDirBytes)
	if err != nil {
		_ = release()
		return nil, err
	}

	return &mmapSnapshot{data: data, release: release, hdr: hdr, meta: meta, membDir: mdir}, nil
}

// sliceRange returns data[start:end] when the range is inside data and
// correctly ordered, and reports false otherwise.
//
// The ordering test is the point. sections() used to check only that each
// section END was inside the file, which a corrupt offset satisfies while
// start > end — Go then panics with "slice bounds out of range [357:77]".
// FuzzMmapSnapshotCorruption found exactly that at open, before the CRC check
// that would have rejected the file.
func sliceRange(data []byte, start, end uint64) ([]byte, bool) {
	size := uint64(len(data))
	if end < start || start > size || end > size {
		return nil, false
	}
	return data[start:end], true
}

// addLen adds a length to an offset and reports false on unsigned overflow. A
// wrapped sum produces a small "end" that passes a naive bounds check while the
// start is enormous.
func addLen(off, n uint64) (uint64, bool) {
	sum := off + n
	if sum < off {
		return 0, false
	}
	return sum, true
}

// sections returns the directory and metadata byte ranges, bounds-checked.
func sections(data []byte, hdr *mmapSnapshotHeader) (nodeDir, edgeDir, adjDir, membDir, meta []byte, err error) {
	size := uint64(len(data))
	nodeDirEnd, ok1 := addLen(hdr.nodeDirOffset, hdr.nodeDirLen()*8)
	edgeDirEnd, ok2 := addLen(hdr.edgeDirOffset, hdr.edgeDirLen()*8)
	adjDirEnd, ok3 := addLen(hdr.adjDirOffset, hdr.nodeDirLen()*adjDirEntrySize)
	membDirEnd, ok4 := addLen(hdr.membDirOffset, hdr.membDirLen)
	metaEnd, ok5 := addLen(hdr.metaOffset, hdr.metaLen)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot section length overflows (size %d)", size)
	}
	if hdr.nodeCount > 0 && (nodeDirEnd > size || adjDirEnd > size) ||
		hdr.edgeCount > 0 && edgeDirEnd > size ||
		membDirEnd > size || metaEnd > size {
		return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot section out of bounds (size %d)", size)
	}
	// CSR section offsets must be ordered: header < outCSR <= inCSR <= adjDir < meta.
	// The adjacency directory entries hold absolute run offsets; this cheap ordering
	// check guards against a malformed header before those offsets are trusted.
	if hdr.nodeCount > 0 {
		if hdr.outCSROffset < uint64(mmapHeaderSize) ||
			hdr.inCSROffset < hdr.outCSROffset ||
			hdr.adjDirOffset < hdr.inCSROffset ||
			hdr.adjDirOffset >= hdr.metaOffset {
			return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot CSR sections out of order (out=%d in=%d adj=%d meta=%d)",
				hdr.outCSROffset, hdr.inCSROffset, hdr.adjDirOffset, hdr.metaOffset)
		}
	}
	if hdr.membDirLen > 0 {
		if (hdr.nodeCount > 0 && hdr.membDataOffset < hdr.adjDirOffset) ||
			hdr.membDirOffset < hdr.membDataOffset ||
			hdr.membDirOffset >= hdr.metaOffset {
			return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot membership sections out of order (nodeCount=%d adjDir=%d membData=%d membDir=%d meta=%d)",
				hdr.nodeCount, hdr.adjDirOffset, hdr.membDataOffset, hdr.membDirOffset, hdr.metaOffset)
		}
	}
	// Every slice goes through sliceRange: the checks above bound each END,
	// which a corrupt offset satisfies while start > end.
	var ok bool
	if hdr.nodeCount > 0 {
		if nodeDir, ok = sliceRange(data, hdr.nodeDirOffset, nodeDirEnd); !ok {
			return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot node directory range invalid (%d..%d)", hdr.nodeDirOffset, nodeDirEnd)
		}
		if adjDir, ok = sliceRange(data, hdr.adjDirOffset, adjDirEnd); !ok {
			return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot adjacency directory range invalid (%d..%d)", hdr.adjDirOffset, adjDirEnd)
		}
	}
	if hdr.edgeCount > 0 {
		if edgeDir, ok = sliceRange(data, hdr.edgeDirOffset, edgeDirEnd); !ok {
			return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot edge directory range invalid (%d..%d)", hdr.edgeDirOffset, edgeDirEnd)
		}
	}
	if hdr.membDirLen > 0 {
		if membDir, ok = sliceRange(data, hdr.membDirOffset, membDirEnd); !ok {
			return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot membership directory range invalid (%d..%d)", hdr.membDirOffset, membDirEnd)
		}
	}
	if meta, ok = sliceRange(data, hdr.metaOffset, metaEnd); !ok {
		return nil, nil, nil, nil, nil, fmt.Errorf("mmap snapshot metadata range invalid (%d..%d)", hdr.metaOffset, metaEnd)
	}
	return nodeDir, edgeDir, adjDir, membDir, meta, nil
}

func (m *mmapSnapshot) close() error            { return m.release() }
func (m *mmapSnapshot) nodeCount() int          { return int(m.hdr.nodeCount) }
func (m *mmapSnapshot) edgeCount() int          { return int(m.hdr.edgeCount) }
func (m *mmapSnapshot) metadata() *mmapMetadata { return m.meta }

func (m *mmapSnapshot) nodeIDRange() (uint64, uint64) { return m.hdr.minNodeID, m.hdr.maxNodeID }
func (m *mmapSnapshot) edgeIDRange() (uint64, uint64) { return m.hdr.minEdgeID, m.hdr.maxEdgeID }

// getNode returns the node's record, or ErrNodeNotFound when the directory has
// no entry for the ID. Any OTHER error means the entry exists and the record
// could not be produced — see ErrRecordUnreadable. The two must never be
// confused: one is a fact about the graph, the other about this file.
func (m *mmapSnapshot) getNode(id uint64) (*Node, error) {
	off, ok := m.nodeOffset(id)
	if !ok {
		return nil, ErrNodeNotFound
	}
	return decodeNodeRecordAt(m.data, off)
}

func (m *mmapSnapshot) getEdge(id uint64) (*Edge, error) {
	off, ok := m.edgeOffset(id)
	if !ok {
		return nil, ErrEdgeNotFound
	}
	return decodeEdgeRecordAt(m.data, off)
}

func (m *mmapSnapshot) nodeOffset(id uint64) (int64, bool) {
	if m.hdr.nodeCount == 0 || id < m.hdr.minNodeID || id > m.hdr.maxNodeID {
		return 0, false
	}
	off := m.dirEntry(m.hdr.nodeDirOffset, id-m.hdr.minNodeID)
	return off, off != dirAbsent
}

func (m *mmapSnapshot) edgeOffset(id uint64) (int64, bool) {
	if m.hdr.edgeCount == 0 || id < m.hdr.minEdgeID || id > m.hdr.maxEdgeID {
		return 0, false
	}
	off := m.dirEntry(m.hdr.edgeDirOffset, id-m.hdr.minEdgeID)
	return off, off != dirAbsent
}

// checkDirRange rejects a header whose declared ID range cannot fit in the
// directory the file actually carries. The range drives a loop; the directory
// bounds the reads. When they disagree the file is malformed, and refusing at
// open is the only place that costs nothing.
func checkDirRange(count, minID, maxID uint64, dirLen int, what string) error {
	if count == 0 {
		return nil
	}
	if maxID < minID {
		return fmt.Errorf("%s ID range inverted: min %d > max %d", what, minID, maxID)
	}
	span := maxID - minID + 1
	if span > uint64(dirLen)/8 {
		return fmt.Errorf("%s ID range %d..%d needs %d directory entries, the file carries %d",
			what, minID, maxID, span, uint64(dirLen)/8)
	}
	return nil
}

func (m *mmapSnapshot) dirEntry(dirOffset, idx uint64) int64 {
	// idx comes from the header's ID range and dirOffset from the header's
	// directory pointer. Both are file data. A snapshot whose maxEdgeID says
	// there are more entries than the directory holds walks this read straight
	// off the end of the mapping — FuzzMmapSnapshotCRCRepaired found exactly
	// that: "index out of range [7] with length 3", eight bytes wanted with
	// three left. The CRC hides it in practice, and hiding is not defending:
	// a partial write that lands on a plausible checksum reaches here.
	if idx > (^uint64(0)-dirOffset-8)/8 {
		return dirAbsent
	}
	p := dirOffset + idx*8
	if p+8 > uint64(len(m.data)) {
		return dirAbsent
	}
	return int64(binary.LittleEndian.Uint64(m.data[p:]))
}

// forEachNodeID calls fn for every present node ID with its record offset, in ascending
// ID order. Used by the loader to field-scan and build the in-memory indexes.
func (m *mmapSnapshot) forEachNodeID(fn func(id uint64, off int64)) {
	for id := m.hdr.minNodeID; m.hdr.nodeCount > 0 && id <= m.hdr.maxNodeID; id++ {
		if off := m.dirEntry(m.hdr.nodeDirOffset, id-m.hdr.minNodeID); off != dirAbsent {
			fn(id, off)
		}
	}
}

func (m *mmapSnapshot) forEachEdgeID(fn func(id uint64, off int64)) {
	for id := m.hdr.minEdgeID; m.hdr.edgeCount > 0 && id <= m.hdr.maxEdgeID; id++ {
		if off := m.dirEntry(m.hdr.edgeDirOffset, id-m.hdr.minEdgeID); off != dirAbsent {
			fn(id, off)
		}
	}
}

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
	ids, _, _ := readCSRRun(m.data, int(outOff))
	return ids
}

func (m *mmapSnapshot) incomingCSR(id uint64) []uint64 {
	_, _, inOff, inLen, ok := m.adjDirEntry(id)
	if !ok || inLen == 0 {
		return nil
	}
	ids, _, _ := readCSRRun(m.data, int(inOff))
	return ids
}

// membershipRun returns the persisted sorted ID run for (kind,tenant,name), or nil.
func (m *mmapSnapshot) membershipRun(kind byte, tenant, name string) []uint64 {
	if m.membDir == nil {
		return nil
	}
	off, idCount, ok := m.membDir.lookup(kind, tenant, name)
	if !ok || idCount == 0 {
		return nil
	}
	ids, _, _ := readCSRRun(m.data, int(off))
	return ids
}

// membershipContains reports whether id is in the (kind, tenant, name) run,
// WITHOUT copying the run out of the mapping. membershipRun goes through
// readCSRRun, which allocates a slice of every ID the tenant owns — far too
// much for a single-record read path. The run is sorted, so a binary search
// over the mapped bytes answers the same question at O(log n) and zero
// allocation.
//
// Every read is bounds-checked against len(m.data). The offset and the count
// come from the membership DIRECTORY, which computeCRC does cover; the run
// bytes this search then reads are NOT covered. Both arrive as a signed int64
// that the file chose. So the CRC hides a bad directory pair in practice, and
// hiding is not defending: a partial write that lands on a plausible checksum
// reaches here with any offset and any count, and no checksum at all stands
// behind the run. A read that would leave the mapping returns false rather
// than panicking: this function guards a security decision, and failing closed
// is the only safe direction.
func (m *mmapSnapshot) membershipContains(kind byte, tenant, name string, id uint64) bool {
	if m.membDir == nil {
		return false
	}
	off, idCount, ok := m.membDir.lookup(kind, tenant, name)
	// off and idCount are int64 read straight out of the file. A negative off
	// converts to a huge uint64 that the base check below would reject, EXCEPT
	// near -1, where uint64(off)+4 wraps to a small in-bounds base. Reject the
	// sign here so no later arithmetic has to survive it.
	if !ok || off < 0 || idCount <= 0 {
		return false
	}
	size := uint64(len(m.data))
	// The run is count(4) then count little-endian uint64s, per appendCSRRun.
	const runHeader = 4
	// off >= 0, so uint64(off) <= MaxInt64 and this addition cannot wrap.
	base := uint64(off) + runHeader
	// Order matters here. Validate base BEFORE it appears in a subtraction:
	// size - base UNDERFLOWS to a huge number when base is past the end, and
	// the count check would then pass for any count at all.
	if base > size {
		return false
	}
	// Reject a count that cannot fit before computing any index from it. After
	// this, count*8 <= size-base, so every mid*8 below fits in a uint64 and
	// every base+mid*8 stays inside the mapping.
	count := uint64(idCount)
	if count > (size-base)/8 {
		return false
	}
	lo, hi := uint64(0), count
	for lo < hi {
		mid := lo + (hi-lo)/2
		p := base + mid*8
		if p+8 > size {
			return false
		}
		if binary.LittleEndian.Uint64(m.data[p:]) < id {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= count {
		return false
	}
	p := base + lo*8
	if p+8 > size {
		return false
	}
	return binary.LittleEndian.Uint64(m.data[p:]) == id
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
