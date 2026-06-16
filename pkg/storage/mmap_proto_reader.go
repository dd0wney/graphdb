package storage

// PROTOTYPE reader: maps the snapshot file read-only and materializes nodes/edges
// lazily on access. Open parses only the header (O(1), ~0 allocation); the dense
// directory is read in place from the mapped bytes. Property Value.Data slices alias
// the mapping, so they are valid only while the snapshot is open (close() munmaps).
// Uses syscall.Mmap (unix); consistent with the package already being unix-only
// (graceful.go / verification.go use syscall.SIGUSR1 / Stat_t without build tags).

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
)

type mmapSnapshot struct {
	data []byte
	hdr  *mmapProtoHeader
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
	hdr, err := unmarshalMmapProtoHeader(data)
	if err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}
	return &mmapSnapshot{data: data, hdr: hdr}, nil
}

func (m *mmapSnapshot) close() error   { return syscall.Munmap(m.data) }
func (m *mmapSnapshot) nodeCount() int { return int(m.hdr.nodeCount) }
func (m *mmapSnapshot) edgeCount() int { return int(m.hdr.edgeCount) }

func (m *mmapSnapshot) nodeIDRange() (uint64, uint64) { return m.hdr.minNodeID, m.hdr.maxNodeID }
func (m *mmapSnapshot) edgeIDRange() (uint64, uint64) { return m.hdr.minEdgeID, m.hdr.maxEdgeID }

func (m *mmapSnapshot) getNode(id uint64) (*Node, bool) {
	if m.hdr.nodeCount == 0 || id < m.hdr.minNodeID || id > m.hdr.maxNodeID {
		return nil, false
	}
	off := m.dirEntry(m.hdr.nodeDirOffset, id-m.hdr.minNodeID)
	if off == dirAbsent {
		return nil, false
	}
	return decodeNodeRecordAt(m.data, off), true
}

func (m *mmapSnapshot) getEdge(id uint64) (*Edge, bool) {
	if m.hdr.edgeCount == 0 || id < m.hdr.minEdgeID || id > m.hdr.maxEdgeID {
		return nil, false
	}
	off := m.dirEntry(m.hdr.edgeDirOffset, id-m.hdr.minEdgeID)
	if off == dirAbsent {
		return nil, false
	}
	return decodeEdgeRecordAt(m.data, off), true
}

func (m *mmapSnapshot) dirEntry(dirOffset, idx uint64) int64 {
	p := dirOffset + idx*8
	return int64(binary.LittleEndian.Uint64(m.data[p:]))
}
