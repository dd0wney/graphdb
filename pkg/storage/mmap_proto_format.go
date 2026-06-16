package storage

// PROTOTYPE (graphdb ask #1, Stage 1). Binary, mmap-able snapshot format with a
// dense ID->offset directory, designed so reopen can map the file and materialize
// nodes/edges lazily on access instead of allocating the whole graph up front (the
// allocation-bound cost the parse-vs-alloc spike measured). This is a self-contained
// prototype: it is NOT wired into loadFromDisk/reopen, has no write path, and assumes
// plaintext (mmap is incompatible with at-rest encryption). See
// SPIKE_REOPEN_COST_2026-06-16.md.
//
// File layout:
//
//	[header]
//	[node records]            variable-length, in ascending ID order
//	[node directory]          dense []int64 absolute offsets, indexed by id-minNodeID (-1 = absent)
//	[edge records]
//	[edge directory]          dense []int64 offsets, indexed by id-minEdgeID
//
// All integers little-endian (matching pkg/wal + pkg/storage/edgestore). Property
// Value.Data is stored verbatim (it is already the canonical binary encoding) so a
// reader can alias it directly into the mapped bytes — zero-copy, no per-type decode.

import (
	"encoding/binary"
	"fmt"
)

var mmapProtoMagic = [4]byte{'G', 'M', 'N', 'P'} // graphdb mmap node proto

const (
	mmapProtoVersion uint32 = 1
	// headerSize is fixed; section offsets are absolute from file start.
	mmapHeaderSize = 4 + 4 + // magic + version
		8 + 8 + // nodeCount + edgeCount
		8 + 8 + // minNodeID + maxNodeID
		8 + 8 + // minEdgeID + maxEdgeID
		8 + 8 // nodeDirOffset + edgeDirOffset
	dirAbsent int64 = -1
)

type mmapProtoHeader struct {
	nodeCount     uint64
	edgeCount     uint64
	minNodeID     uint64
	maxNodeID     uint64
	minEdgeID     uint64
	maxEdgeID     uint64
	nodeDirOffset uint64
	edgeDirOffset uint64
}

func (h *mmapProtoHeader) marshal() []byte {
	b := make([]byte, mmapHeaderSize)
	copy(b[0:4], mmapProtoMagic[:])
	binary.LittleEndian.PutUint32(b[4:8], mmapProtoVersion)
	binary.LittleEndian.PutUint64(b[8:16], h.nodeCount)
	binary.LittleEndian.PutUint64(b[16:24], h.edgeCount)
	binary.LittleEndian.PutUint64(b[24:32], h.minNodeID)
	binary.LittleEndian.PutUint64(b[32:40], h.maxNodeID)
	binary.LittleEndian.PutUint64(b[40:48], h.minEdgeID)
	binary.LittleEndian.PutUint64(b[48:56], h.maxEdgeID)
	binary.LittleEndian.PutUint64(b[56:64], h.nodeDirOffset)
	binary.LittleEndian.PutUint64(b[64:72], h.edgeDirOffset)
	return b
}

func unmarshalMmapProtoHeader(b []byte) (*mmapProtoHeader, error) {
	if len(b) < mmapHeaderSize {
		return nil, fmt.Errorf("mmap snapshot truncated: %d bytes < header %d", len(b), mmapHeaderSize)
	}
	if [4]byte{b[0], b[1], b[2], b[3]} != mmapProtoMagic {
		return nil, fmt.Errorf("mmap snapshot bad magic %q", b[0:4])
	}
	if v := binary.LittleEndian.Uint32(b[4:8]); v != mmapProtoVersion {
		return nil, fmt.Errorf("mmap snapshot version %d unsupported (want %d)", v, mmapProtoVersion)
	}
	return &mmapProtoHeader{
		nodeCount:     binary.LittleEndian.Uint64(b[8:16]),
		edgeCount:     binary.LittleEndian.Uint64(b[16:24]),
		minNodeID:     binary.LittleEndian.Uint64(b[24:32]),
		maxNodeID:     binary.LittleEndian.Uint64(b[32:40]),
		minEdgeID:     binary.LittleEndian.Uint64(b[40:48]),
		maxEdgeID:     binary.LittleEndian.Uint64(b[48:56]),
		nodeDirOffset: binary.LittleEndian.Uint64(b[56:64]),
		edgeDirOffset: binary.LittleEndian.Uint64(b[64:72]),
	}, nil
}

// --- record codec ---------------------------------------------------------
//
// Shared property-bag framing: nProps(2) then per prop keyLen(2)|key|type(1)|dataLen(4)|data.

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

// readProps decodes a property bag at buf[p:], aliasing each Value.Data directly
// into buf (zero-copy). Returns the map and the offset just past the bag.
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
		props[key] = Value{Type: vt, Data: buf[p : p+dl]} // alias into mmap
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

// decodeNodeRecordAt materializes a *Node from the record at buf[off:]. Property
// Data slices alias buf; the returned struct/map/strings are freshly allocated, so
// it satisfies the clone-by-value read contract while avoiding per-property byte copies.
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
	e.CreatedAt = int64(binary.LittleEndian.Uint64(buf[p:]))
	return e
}
