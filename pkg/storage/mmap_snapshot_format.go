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
//	[metadata blob]           JSON: property/vector indexes, stats, nextIDs, sticky label/type keys
//
// Integrity: a CRC32 over the header (excluding the CRC field) + both directories +
// the metadata blob protects the structural index — the parts read at open. Record
// bytes are paged in lazily and bounds-checked at decode. All integers little-endian.

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math"
)

var mmapSnapshotMagic = [4]byte{'G', 'M', 'N', 'P'}

const (
	mmapSnapshotVersion uint32 = 2 // v1 was the prototype (no CRC/metadata)
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
	hCRC           = 92
	mmapHeaderSize = 100 // hCRC(4) + pad(4) -> 100
)

type mmapSnapshotHeader struct {
	flags         uint32
	nodeCount     uint64
	edgeCount     uint64
	minNodeID     uint64
	maxNodeID     uint64
	minEdgeID     uint64
	maxEdgeID     uint64
	nodeDirOffset uint64
	edgeDirOffset uint64
	metaOffset    uint64
	metaLen       uint64
	crc           uint32
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
		flags:         binary.LittleEndian.Uint32(b[hFlags:]),
		nodeCount:     binary.LittleEndian.Uint64(b[hNodeCount:]),
		edgeCount:     binary.LittleEndian.Uint64(b[hEdgeCount:]),
		minNodeID:     binary.LittleEndian.Uint64(b[hMinNodeID:]),
		maxNodeID:     binary.LittleEndian.Uint64(b[hMaxNodeID:]),
		minEdgeID:     binary.LittleEndian.Uint64(b[hMinEdgeID:]),
		maxEdgeID:     binary.LittleEndian.Uint64(b[hMaxEdgeID:]),
		nodeDirOffset: binary.LittleEndian.Uint64(b[hNodeDir:]),
		edgeDirOffset: binary.LittleEndian.Uint64(b[hEdgeDir:]),
		metaOffset:    binary.LittleEndian.Uint64(b[hMetaOff:]),
		metaLen:       binary.LittleEndian.Uint64(b[hMetaLen:]),
		crc:           binary.LittleEndian.Uint32(b[hCRC:]),
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

// computeCRC hashes the header (excluding the CRC field) + both directories + the
// metadata blob — the structural sections read at open. Records are excluded so open
// need not page in the whole file to verify integrity.
func computeCRC(headerNoCRC, nodeDir, edgeDir, meta []byte) uint32 {
	h := crc32.NewIEEE()
	h.Write(headerNoCRC)
	h.Write(nodeDir)
	h.Write(edgeDir)
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
func readProps(buf []byte, p int) (map[string]Value, int) {
	n := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	props := make(map[string]Value, n)
	for i := 0; i < n; i++ {
		kl := int(binary.LittleEndian.Uint16(buf[p:]))
		p += 2
		key := string(buf[p : p+kl])
		p += kl
		vt := ValueType(buf[p])
		p++
		dl := int(binary.LittleEndian.Uint32(buf[p:]))
		p += 4
		data := make([]byte, dl) // copy-on-read: do not alias the mapping
		copy(data, buf[p:p+dl])
		props[key] = Value{Type: vt, Data: data}
		p += dl
	}
	return props, p
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
func decodeNodeRecordAt(buf []byte, off int64) *Node {
	p := int(off)
	n := &Node{}
	n.ID = binary.LittleEndian.Uint64(buf[p:])
	p += 8
	tl := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	n.TenantID = string(buf[p : p+tl])
	p += tl
	nl := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	if nl > 0 {
		n.Labels = make([]string, nl)
		for i := 0; i < nl; i++ {
			ll := int(binary.LittleEndian.Uint16(buf[p:]))
			p += 2
			n.Labels[i] = string(buf[p : p+ll])
			p += ll
		}
	}
	n.Properties, p = readProps(buf, p)
	n.CreatedAt = int64(binary.LittleEndian.Uint64(buf[p:]))
	p += 8
	n.UpdatedAt = int64(binary.LittleEndian.Uint64(buf[p:]))
	return n
}

// scanNodeFields reads only the indexed prefix (id, tenant, labels) without allocating
// the property bag — used by the loader to build the in-memory indexes cheaply.
func scanNodeFields(buf []byte, off int64) (id uint64, tenant string, labels []string) {
	p := int(off)
	id = binary.LittleEndian.Uint64(buf[p:])
	p += 8
	tl := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	tenant = string(buf[p : p+tl])
	p += tl
	nl := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	if nl > 0 {
		labels = make([]string, nl)
		for i := 0; i < nl; i++ {
			ll := int(binary.LittleEndian.Uint16(buf[p:]))
			p += 2
			labels[i] = string(buf[p : p+ll])
			p += ll
		}
	}
	return id, tenant, labels
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

func decodeEdgeRecordAt(buf []byte, off int64) *Edge {
	p := int(off)
	e := &Edge{}
	e.ID = binary.LittleEndian.Uint64(buf[p:])
	p += 8
	tl := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	e.TenantID = string(buf[p : p+tl])
	p += tl
	e.FromNodeID = binary.LittleEndian.Uint64(buf[p:])
	p += 8
	e.ToNodeID = binary.LittleEndian.Uint64(buf[p:])
	p += 8
	tyl := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	e.Type = string(buf[p : p+tyl])
	p += tyl
	e.Properties, p = readProps(buf, p)
	e.Weight = math.Float64frombits(binary.LittleEndian.Uint64(buf[p:]))
	p += 8
	e.CreatedAt = int64(binary.LittleEndian.Uint64(buf[p:]))
	return e
}

// scanEdgeFields reads only the indexed prefix (id, tenant, from, to, type).
func scanEdgeFields(buf []byte, off int64) (id, from, to uint64, tenant, etype string) {
	p := int(off)
	id = binary.LittleEndian.Uint64(buf[p:])
	p += 8
	tl := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	tenant = string(buf[p : p+tl])
	p += tl
	from = binary.LittleEndian.Uint64(buf[p:])
	p += 8
	to = binary.LittleEndian.Uint64(buf[p:])
	p += 8
	tyl := int(binary.LittleEndian.Uint16(buf[p:]))
	p += 2
	etype = string(buf[p : p+tyl])
	return id, from, to, tenant, etype
}
