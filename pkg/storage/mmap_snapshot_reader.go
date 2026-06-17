package storage

// Reader for the mmap-able snapshot format. Maps the file read-only, verifies the CRC
// over the structural sections (header + directories + metadata) at open, and
// materializes nodes/edges lazily on access (copy-on-read, so results are safe to
// retain after close). Uses syscall.Mmap (unix) — consistent with the package already
// being unix-only (graceful.go / verification.go use syscall.SIGUSR1 / Stat_t).

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
)

type mmapSnapshot struct {
	data    []byte
	hdr     *mmapSnapshotHeader
	meta    *mmapMetadata
	membDir *membershipDir
}

func openMmapSnapshot(path string) (*mmapSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := int(fi.Size())
	if size < mmapHeaderSize {
		return nil, fmt.Errorf("mmap snapshot %q too small: %d bytes", path, size)
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap %q: %w", path, err)
	}

	hdr, err := unmarshalMmapHeader(data)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}

	nodeDir, edgeDir, adjDir, membDirBytes, metaBytes, err := sections(data, hdr)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}
	if got := computeCRC(data[:hCRC], nodeDir, edgeDir, adjDir, membDirBytes, metaBytes); got != hdr.crc {
		_ = syscall.Munmap(data)
		return nil, fmt.Errorf("mmap snapshot %q CRC mismatch: got %08x want %08x", path, got, hdr.crc)
	}
	meta, err := unmarshalMmapMetadata(metaBytes)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}
	mdir, err := parseMembershipDir(membDirBytes)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}

	return &mmapSnapshot{data: data, hdr: hdr, meta: meta, membDir: mdir}, nil
}

// sections returns the directory and metadata byte ranges, bounds-checked.
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

func (m *mmapSnapshot) close() error            { return syscall.Munmap(m.data) }
func (m *mmapSnapshot) nodeCount() int          { return int(m.hdr.nodeCount) }
func (m *mmapSnapshot) edgeCount() int          { return int(m.hdr.edgeCount) }
func (m *mmapSnapshot) metadata() *mmapMetadata { return m.meta }

func (m *mmapSnapshot) nodeIDRange() (uint64, uint64) { return m.hdr.minNodeID, m.hdr.maxNodeID }
func (m *mmapSnapshot) edgeIDRange() (uint64, uint64) { return m.hdr.minEdgeID, m.hdr.maxEdgeID }

func (m *mmapSnapshot) getNode(id uint64) (*Node, bool) {
	off, ok := m.nodeOffset(id)
	if !ok {
		return nil, false
	}
	return decodeNodeRecordAt(m.data, off), true
}

func (m *mmapSnapshot) getEdge(id uint64) (*Edge, bool) {
	off, ok := m.edgeOffset(id)
	if !ok {
		return nil, false
	}
	return decodeEdgeRecordAt(m.data, off), true
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

func (m *mmapSnapshot) dirEntry(dirOffset, idx uint64) int64 {
	p := dirOffset + idx*8
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

// membershipRun returns the persisted sorted ID run for (kind,tenant,name), or nil.
func (m *mmapSnapshot) membershipRun(kind byte, tenant, name string) []uint64 {
	if m.membDir == nil {
		return nil
	}
	off, idCount, ok := m.membDir.lookup(kind, tenant, name)
	if !ok || idCount == 0 {
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
