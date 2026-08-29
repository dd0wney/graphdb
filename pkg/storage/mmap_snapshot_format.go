package storage

// Binary, mmap-able snapshot format for the lazy-reopen storage mode (graphdb ask #1,
// Stage 1). Reopen maps this file and builds only the in-memory indexes via a field
// scan, materializing full nodes/edges lazily on access — avoiding the up-front
// allocation of the whole graph that makes the JSON path allocation-bound (see
// SPIKE_REOPEN_COST_2026-06-16.md). Plaintext only (mmap maps the file as-is; encrypted
// stores fall back to the JSON path).
//
// File layout:
//
//	[header]                  fixed mmapHeaderSize bytes, magic GMNP
//	[node records]            variable-length, ascending ID order
//	[node directory]          dense []int64 absolute offsets, indexed by id-minNodeID (-1 = absent)
//	[edge records]
//	[edge directory]          dense []int64 offsets, indexed by id-minEdgeID
//	[outgoing CSR runs]       one length-prefixed []uint64 run per node (count 0 = no edges), ascending node order
//	[incoming CSR runs]       same shape
//	[adjacency directory]     dense []entry of 32 bytes (outOff,outLen,inOff,inLen int64), indexed by id-minNodeID
//	[membership run data]     sorted []uint64 runs, length-prefixed (CSR codec)
//	[membership directory]    sorted full-key entries: keyLen(2)|key(incl. leading kind byte)|runOffset(8)|idCount(8)
//	[metadata blob]           JSON: property/vector indexes, stats, nextIDs, sticky label/type keys
//
// Integrity: a CRC32 over the header (excluding the CRC field) + all three directories
// (node, edge, adjacency) + the metadata blob protects the structural index — the parts
// read at open. Record bytes are paged in lazily and bounds-checked at decode. All
// integers little-endian.

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math"
	"sort"
	"strings"

	"github.com/dd0wney/graphdb/pkg/alloc"
)

var mmapSnapshotMagic = [4]byte{'G', 'M', 'N', 'P'}

const (
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
	hOutCSR        = 92  // outgoing CSR data section offset
	hInCSR         = 100 // incoming CSR data section offset
	hAdjDir        = 108 // combined adjacency directory offset
	hMembData      = 116 // membership run-data section offset
	hMembDir       = 124 // membership directory offset
	hMembDirLen    = 132 // membership directory length in bytes
	hCRC           = 140
	mmapHeaderSize = 148 // hCRC(4) + pad(4) -> 148
)

type mmapSnapshotHeader struct {
	flags          uint32
	nodeCount      uint64
	edgeCount      uint64
	minNodeID      uint64
	maxNodeID      uint64
	minEdgeID      uint64
	maxEdgeID      uint64
	nodeDirOffset  uint64
	edgeDirOffset  uint64
	metaOffset     uint64
	metaLen        uint64
	outCSROffset   uint64
	inCSROffset    uint64
	adjDirOffset   uint64
	membDataOffset uint64
	membDirOffset  uint64
	membDirLen     uint64
	crc            uint32
}

// mmapMetadata is the small, eagerly-loaded tail: everything the JSON snapshot struct
// holds except the bulk node/edge records (which live in the lazy mmap'd sections).
type mmapMetadata struct {
	PropertyIndexes  map[string]PropertyIndexSnapshot
	VectorIndexes    []VectorIndexDef
	Stats            Statistics
	NextNodeID       uint64
	NextEdgeID       uint64
	StickyNodeLabels []string // label keys that must survive even with no members
	StickyEdgeTypes  []string
	// TenantStats persists per-tenant counts so reopen restores them without the
	// (now-lazy) membership build. Keyed by tenant ID string.
	TenantStats map[string]TenantStats
}

func (h *mmapSnapshotHeader) marshal() []byte {
	b := make([]byte, mmapHeaderSize)
	copy(b[hMagic:hMagic+4], mmapSnapshotMagic[:])
	binary.LittleEndian.PutUint32(b[hVersion:], mmapSnapshotVersion)
	binary.LittleEndian.PutUint32(b[hFlags:], h.flags)
	binary.LittleEndian.PutUint64(b[hNodeCount:], h.nodeCount)
	binary.LittleEndian.PutUint64(b[hEdgeCount:], h.edgeCount)
	binary.LittleEndian.PutUint64(b[hMinNodeID:], h.minNodeID)
	binary.LittleEndian.PutUint64(b[hMaxNodeID:], h.maxNodeID)
	binary.LittleEndian.PutUint64(b[hMinEdgeID:], h.minEdgeID)
	binary.LittleEndian.PutUint64(b[hMaxEdgeID:], h.maxEdgeID)
	binary.LittleEndian.PutUint64(b[hNodeDir:], h.nodeDirOffset)
	binary.LittleEndian.PutUint64(b[hEdgeDir:], h.edgeDirOffset)
	binary.LittleEndian.PutUint64(b[hMetaOff:], h.metaOffset)
	binary.LittleEndian.PutUint64(b[hMetaLen:], h.metaLen)
	binary.LittleEndian.PutUint64(b[hOutCSR:], h.outCSROffset)
	binary.LittleEndian.PutUint64(b[hInCSR:], h.inCSROffset)
	binary.LittleEndian.PutUint64(b[hAdjDir:], h.adjDirOffset)
	binary.LittleEndian.PutUint64(b[hMembData:], h.membDataOffset)
	binary.LittleEndian.PutUint64(b[hMembDir:], h.membDirOffset)
	binary.LittleEndian.PutUint64(b[hMembDirLen:], h.membDirLen)
	binary.LittleEndian.PutUint32(b[hCRC:], h.crc)
	return b
}

func unmarshalMmapHeader(b []byte) (*mmapSnapshotHeader, error) {
	if len(b) < mmapHeaderSize {
		return nil, fmt.Errorf("mmap snapshot truncated: %d bytes < header %d", len(b), mmapHeaderSize)
	}
	if [4]byte{b[0], b[1], b[2], b[3]} != mmapSnapshotMagic {
		return nil, fmt.Errorf("mmap snapshot bad magic %q", b[0:4])
	}
	if v := binary.LittleEndian.Uint32(b[hVersion:]); v != mmapSnapshotVersion {
		return nil, fmt.Errorf("mmap snapshot version %d unsupported (want %d)", v, mmapSnapshotVersion)
	}
	return &mmapSnapshotHeader{
		flags:          binary.LittleEndian.Uint32(b[hFlags:]),
		nodeCount:      binary.LittleEndian.Uint64(b[hNodeCount:]),
		edgeCount:      binary.LittleEndian.Uint64(b[hEdgeCount:]),
		minNodeID:      binary.LittleEndian.Uint64(b[hMinNodeID:]),
		maxNodeID:      binary.LittleEndian.Uint64(b[hMaxNodeID:]),
		minEdgeID:      binary.LittleEndian.Uint64(b[hMinEdgeID:]),
		maxEdgeID:      binary.LittleEndian.Uint64(b[hMaxEdgeID:]),
		nodeDirOffset:  binary.LittleEndian.Uint64(b[hNodeDir:]),
		edgeDirOffset:  binary.LittleEndian.Uint64(b[hEdgeDir:]),
		metaOffset:     binary.LittleEndian.Uint64(b[hMetaOff:]),
		metaLen:        binary.LittleEndian.Uint64(b[hMetaLen:]),
		outCSROffset:   binary.LittleEndian.Uint64(b[hOutCSR:]),
		inCSROffset:    binary.LittleEndian.Uint64(b[hInCSR:]),
		adjDirOffset:   binary.LittleEndian.Uint64(b[hAdjDir:]),
		membDataOffset: binary.LittleEndian.Uint64(b[hMembData:]),
		membDirOffset:  binary.LittleEndian.Uint64(b[hMembDir:]),
		membDirLen:     binary.LittleEndian.Uint64(b[hMembDirLen:]),
		crc:            binary.LittleEndian.Uint32(b[hCRC:]),
	}, nil
}

// nodeDirLen / edgeDirLen return the directory length in entries (0 when empty).
func (h *mmapSnapshotHeader) nodeDirLen() uint64 {
	if h.nodeCount == 0 {
		return 0
	}
	return h.maxNodeID - h.minNodeID + 1
}

func (h *mmapSnapshotHeader) edgeDirLen() uint64 {
	if h.edgeCount == 0 {
		return 0
	}
	return h.maxEdgeID - h.minEdgeID + 1
}

const adjDirEntrySize = 32 // 4 * int64: outOff, outLen, inOff, inLen

// computeCRC hashes the header (excluding the CRC field) + the node, edge,
// adjacency, and membership directories + the metadata blob — the structural
// sections read at open. Record and run bytes are excluded so open need not page
// in the whole file to verify integrity.
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

func (m *mmapMetadata) marshal() ([]byte, error) { return json.Marshal(m) }

func unmarshalMmapMetadata(b []byte) (*mmapMetadata, error) {
	var m mmapMetadata
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("mmap snapshot metadata: %w", err)
	}
	return &m, nil
}

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

// --- bounds-checked record reading ----------------------------------------
//
// The record decoders below read length prefixes out of the mapped file and
// slice by them. The snapshot CRC covers the header, the directories and the
// metadata blob — it does NOT cover the node and edge record bodies. So a
// record damaged in place (bit rot, a partial write, a truncated copy, a
// hostile file) survives open and reaches a decoder, which then slices past
// the end of the mapping and panics.
//
// Found by FuzzMmapSnapshotCorruption on its seed corpus, before any fuzzing:
// one byte changed at mmapHeaderSize+8 produced
// "slice bounds out of range [:9558] with capacity 1040". SQLite's
// malformed-database tests exist for exactly this class
// (sqlite.org/testing.html §11) and require the reader to report the error
// rather than perform "unwholesome actions".
//
// recordCursor makes every read bounds-checked. Once a read fails the cursor
// is poisoned and every later read is a no-op, so a decoder can run to
// completion and be discarded by its caller on !ok rather than checking after
// each field.
type recordCursor struct {
	buf []byte
	p   int
	ok  bool
}

func newRecordCursor(buf []byte, off int64) *recordCursor {
	c := &recordCursor{buf: buf, ok: true}
	if off < 0 || off > int64(len(buf)) {
		c.ok = false
		return c
	}
	c.p = int(off)
	return c
}

func (c *recordCursor) has(n int) bool {
	if !c.ok || n < 0 || c.p+n > len(c.buf) {
		c.ok = false
		return false
	}
	return true
}

func (c *recordCursor) u64() uint64 {
	if !c.has(8) {
		return 0
	}
	v := binary.LittleEndian.Uint64(c.buf[c.p:])
	c.p += 8
	return v
}

func (c *recordCursor) u32() int {
	if !c.has(4) {
		return 0
	}
	v := int(binary.LittleEndian.Uint32(c.buf[c.p:]))
	c.p += 4
	return v
}

func (c *recordCursor) u16() int {
	if !c.has(2) {
		return 0
	}
	v := int(binary.LittleEndian.Uint16(c.buf[c.p:]))
	c.p += 2
	return v
}

func (c *recordCursor) u8() byte {
	if !c.has(1) {
		return 0
	}
	v := c.buf[c.p]
	c.p++
	return v
}

// str copies out of the mapping, so the result never aliases it.
func (c *recordCursor) str(n int) string {
	if !c.has(n) {
		return ""
	}
	v := string(c.buf[c.p : c.p+n])
	c.p += n
	return v
}

// blob copies n bytes out of the mapping.
//
// n is a length read from the file, so this is a length-driven allocation and
// goes through alloc.Bytes — scorecard row 4. has() has already bounded n by
// what remains in the buffer, so a corrupt length cannot ask for gigabytes;
// the allocator is here so that a refusal can be INJECTED and the read path
// swept, which make() can never be.
//
// A refusal marks the cursor not-ok, and the decode fails the same way a
// damaged record does. See TestSnapshotReadPathUnderAllocationFailure for what
// that costs.
func (c *recordCursor) blob(n int) []byte {
	if !c.has(n) {
		return nil
	}
	v, err := alloc.Bytes(n)
	if err != nil {
		c.ok = false
		return nil
	}
	copy(v, c.buf[c.p:c.p+n])
	c.p += n
	return v
}

// readCSRRun decodes a run at buf[p:], COPYING the IDs into a fresh slice so the
// result is safe to retain after the mapping is closed. Returns nil for an empty run.
func readCSRRun(buf []byte, p int) ([]uint64, int, bool) {
	c := newRecordCursor(buf, int64(p))
	n := c.u32()
	if !c.ok {
		return nil, p, false
	}
	if n == 0 {
		return nil, c.p, true
	}
	// Check the whole run fits before allocating: a corrupt count would
	// otherwise allocate gigabytes and then fail read by read.
	if !c.has(n * 8) {
		return nil, p, false
	}
	ids := make([]uint64, n)
	for i := 0; i < n; i++ {
		ids[i] = c.u64()
	}
	if !c.ok {
		return nil, p, false
	}
	return ids, c.p, true
}

// --- membership directory codec -------------------------------------------
//
// Membership index kinds (graphdb ask #1, Stage 2b). Each kind maps a composite
// (tenant[,name]) key to a sorted []uint64 run.
const (
	membKindNodeTenant byte = 0 // key: tenant        -> all node IDs in tenant
	membKindNodeLabel  byte = 1 // key: tenant,label   -> node IDs with label
	membKindEdgeTenant byte = 2 // key: tenant         -> all edge IDs in tenant
	membKindEdgeType   byte = 3 // key: tenant,type    -> edge IDs of type
)

// membFullKey encodes a directory key as kind ++ tenant ++ 0x00 ++ name. name is
// empty for the tenant-enumeration kinds (0, 2). The 0x00 separator prevents a
// tenant whose name prefixes another (e.g. "t" vs "t2") from colliding on lookup.
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
	// Sort the accumulated keys so the directory is binary-search-ready. The
	// builder is single-use (build then encode once), so the in-place sort is fine.
	sort.Strings(b.order)
	type ent struct {
		key     string
		off     int64
		idCount int64
	}
	ents := make([]ent, 0, len(b.order))
	offset := baseOffset
	for _, key := range b.order {
		ids := b.runs[key]
		rec := appendCSRRun(nil, ids)
		ents = append(ents, ent{key: key, off: offset, idCount: int64(len(ids))})
		runData = append(runData, rec...)
		offset += int64(len(rec))
	}
	directory = binary.LittleEndian.AppendUint32(directory, uint32(len(ents)))
	for _, e := range ents {
		directory = binary.LittleEndian.AppendUint16(directory, uint16(len(e.key)))
		directory = append(directory, e.key...)
		directory = binary.LittleEndian.AppendUint64(directory, uint64(e.off))
		directory = binary.LittleEndian.AppendUint64(directory, uint64(e.idCount))
	}
	return runData, directory
}

// membershipDir is the parsed, lookup-ready directory (sorted full-keys).
type membershipDir struct {
	keys   []string
	offs   []int64
	counts []int64
}

func parseMembershipDir(b []byte) (*membershipDir, error) {
	if len(b) == 0 {
		return &membershipDir{}, nil
	}
	if len(b) < 4 {
		return nil, fmt.Errorf("membership directory truncated")
	}
	n := int(binary.LittleEndian.Uint32(b))
	if maxEntries := (len(b) - 4) / 19; n > maxEntries {
		return nil, fmt.Errorf("membership directory entry count %d exceeds buffer capacity (%d max)", n, maxEntries)
	}
	p := 4
	d := &membershipDir{keys: make([]string, n), offs: make([]int64, n), counts: make([]int64, n)}
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
		d.counts[i] = int64(binary.LittleEndian.Uint64(b[p:]))
		p += 8
	}
	return d, nil
}

// lookup binary-searches for (kind,tenant,name); returns (runByteOffset, idCount, ok).
// idCount==0 means an empty/absent run.
func (d *membershipDir) lookup(kind byte, tenant, name string) (int64, int64, bool) {
	target := string(membFullKey(kind, tenant, name))
	i := sort.SearchStrings(d.keys, target)
	if i < len(d.keys) && d.keys[i] == target {
		return d.offs[i], d.counts[i], true
	}
	return 0, 0, false
}

// keysForKindTenant returns the `name` component of every directory key matching
// (kind, tenant) — used by the label/type-key readers.
// Intended for the label/type kinds (1, 3); for tenant-only kinds (0, 2) the name
// component is empty and this returns a single "" entry.
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

// --- record codec ---------------------------------------------------------
//
// Property bag framing: nProps(2) then per prop keyLen(2)|key|type(1)|dataLen(4)|data.

func appendProps(buf []byte, props map[string]Value) []byte {
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(props)))
	for k, v := range props {
		buf = binary.LittleEndian.AppendUint16(buf, uint16(len(k)))
		buf = append(buf, k...)
		buf = append(buf, byte(v.Type))
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(v.Data)))
		buf = append(buf, v.Data...)
	}
	return buf
}

// readProps decodes a property bag at buf[p:], COPYING each Value.Data into a fresh
// heap slice so the returned node is safe to retain after the mapping is closed.
func readProps(buf []byte, p int) (map[string]Value, int, bool) {
	c := newRecordCursor(buf, int64(p))
	n := c.u16()
	if !c.ok {
		return nil, p, false
	}
	props := make(map[string]Value, min(n, 64)) // cap the hint: n is untrusted
	for i := 0; i < n; i++ {
		key := c.str(c.u16())
		vt := ValueType(c.u8())
		data := c.blob(c.u32()) // copy-on-read: do not alias the mapping
		if !c.ok {
			return nil, p, false
		}
		props[key] = Value{Type: vt, Data: data}
	}
	return props, c.p, true
}

func encodeNodeRecord(n *Node) []byte {
	buf := make([]byte, 0, 64)
	buf = binary.LittleEndian.AppendUint64(buf, n.ID)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(n.TenantID)))
	buf = append(buf, n.TenantID...)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(n.Labels)))
	for _, l := range n.Labels {
		buf = binary.LittleEndian.AppendUint16(buf, uint16(len(l)))
		buf = append(buf, l...)
	}
	buf = appendProps(buf, n.Properties)
	buf = binary.LittleEndian.AppendUint64(buf, uint64(n.CreatedAt))
	buf = binary.LittleEndian.AppendUint64(buf, uint64(n.UpdatedAt))
	return buf
}

// decodeNodeRecordAt materializes a fully heap-owned *Node from buf[off:].
func decodeNodeRecordAt(buf []byte, off int64) (*Node, bool) {
	c := newRecordCursor(buf, off)
	n := &Node{}
	n.ID = c.u64()
	n.TenantID = c.str(c.u16())
	if nl := c.u16(); nl > 0 {
		if !c.has(nl * 2) { // cheapest possible floor: 2 bytes per label prefix
			return nil, false
		}
		n.Labels = make([]string, nl)
		for i := 0; i < nl; i++ {
			n.Labels[i] = c.str(c.u16())
		}
	}
	if !c.ok {
		return nil, false
	}
	props, p, ok := readProps(buf, c.p)
	if !ok {
		return nil, false
	}
	n.Properties = props
	c.p = p
	n.CreatedAt = int64(c.u64())
	n.UpdatedAt = int64(c.u64())
	if !c.ok {
		return nil, false
	}
	return n, true
}

// scanNodeFields reads only the indexed prefix (id, tenant, labels) without allocating
// the property bag — used by the loader to build the in-memory indexes cheaply.
func scanNodeFields(buf []byte, off int64) (id uint64, tenant string, labels []string, ok bool) {
	c := newRecordCursor(buf, off)
	id = c.u64()
	tenant = c.str(c.u16())
	if nl := c.u16(); nl > 0 {
		if !c.has(nl * 2) {
			return 0, "", nil, false
		}
		labels = make([]string, nl)
		for i := 0; i < nl; i++ {
			labels[i] = c.str(c.u16())
		}
	}
	if !c.ok {
		return 0, "", nil, false
	}
	return id, tenant, labels, true
}

func encodeEdgeRecord(e *Edge) []byte {
	buf := make([]byte, 0, 64)
	buf = binary.LittleEndian.AppendUint64(buf, e.ID)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(e.TenantID)))
	buf = append(buf, e.TenantID...)
	buf = binary.LittleEndian.AppendUint64(buf, e.FromNodeID)
	buf = binary.LittleEndian.AppendUint64(buf, e.ToNodeID)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(e.Type)))
	buf = append(buf, e.Type...)
	buf = appendProps(buf, e.Properties)
	buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(e.Weight))
	buf = binary.LittleEndian.AppendUint64(buf, uint64(e.CreatedAt))
	return buf
}

func decodeEdgeRecordAt(buf []byte, off int64) (*Edge, bool) {
	c := newRecordCursor(buf, off)
	e := &Edge{}
	e.ID = c.u64()
	e.TenantID = c.str(c.u16()) // str copies — no alias into the mmap region
	e.FromNodeID = c.u64()
	e.ToNodeID = c.u64()
	e.Type = c.str(c.u16())
	if !c.ok {
		return nil, false
	}
	props, p, ok := readProps(buf, c.p)
	if !ok {
		return nil, false
	}
	e.Properties = props
	c.p = p
	e.Weight = math.Float64frombits(c.u64())
	e.CreatedAt = int64(c.u64())
	if !c.ok {
		return nil, false
	}
	return e, true
}

// scanEdgeFields reads only the indexed prefix (id, tenant, from, to, type).
func scanEdgeFields(buf []byte, off int64) (id, from, to uint64, tenant, etype string, ok bool) {
	c := newRecordCursor(buf, off)
	id = c.u64()
	tenant = c.str(c.u16())
	from = c.u64()
	to = c.u64()
	etype = c.str(c.u16())
	if !c.ok {
		return 0, 0, 0, "", "", false
	}
	return id, from, to, tenant, etype, true
}
