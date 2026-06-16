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
	data []byte
	hdr  *mmapSnapshotHeader
	meta *mmapMetadata
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

	nodeDir, edgeDir, metaBytes, err := sections(data, hdr)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}
	if got := computeCRC(data[:hCRC], nodeDir, edgeDir, metaBytes); got != hdr.crc {
		_ = syscall.Munmap(data)
		return nil, fmt.Errorf("mmap snapshot %q CRC mismatch: got %08x want %08x", path, got, hdr.crc)
	}
	meta, err := unmarshalMmapMetadata(metaBytes)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}

	return &mmapSnapshot{data: data, hdr: hdr, meta: meta}, nil
}

// sections returns the directory and metadata byte ranges, bounds-checked.
func sections(data []byte, hdr *mmapSnapshotHeader) (nodeDir, edgeDir, meta []byte, err error) {
	size := uint64(len(data))
	nodeDirEnd := hdr.nodeDirOffset + hdr.nodeDirLen()*8
	edgeDirEnd := hdr.edgeDirOffset + hdr.edgeDirLen()*8
	metaEnd := hdr.metaOffset + hdr.metaLen
	if hdr.nodeCount > 0 && nodeDirEnd > size ||
		hdr.edgeCount > 0 && edgeDirEnd > size ||
		metaEnd > size {
		return nil, nil, nil, fmt.Errorf("mmap snapshot section out of bounds (size %d)", size)
	}
	if hdr.nodeCount > 0 {
		nodeDir = data[hdr.nodeDirOffset:nodeDirEnd]
	}
	if hdr.edgeCount > 0 {
		edgeDir = data[hdr.edgeDirOffset:edgeDirEnd]
	}
	meta = data[hdr.metaOffset:metaEnd]
	return nodeDir, edgeDir, meta, nil
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
