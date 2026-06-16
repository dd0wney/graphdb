package storage

// Writer for the mmap-able snapshot format (see mmap_snapshot_format.go).
// writeMmapSnapshotData is the pure-format writer (testable without a store);
// writeMmapSnapshot + buildMmapMetadata adapt a quiescent GraphStorage to it.

import (
	"bufio"
	"encoding/binary"
	"os"
	"sort"
	"sync/atomic"
)

// writeMmapSnapshotData writes nodes (sorted ascending by ID), edges, and metadata to
// path in the mmap-able v2 format with a CRC over the structural sections.
func writeMmapSnapshotData(path string, nodes []*Node, edges []*Edge, meta *mmapMetadata) error {
	metaBytes, err := meta.marshal()
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 1<<20)
	hdr := &mmapSnapshotHeader{nodeCount: uint64(len(nodes)), edgeCount: uint64(len(edges))}
	offset := int64(mmapHeaderSize)
	if _, err := w.Write(make([]byte, mmapHeaderSize)); err != nil {
		return err
	}

	var nodeDirBytes, edgeDirBytes []byte

	if len(nodes) > 0 {
		hdr.minNodeID, hdr.maxNodeID = nodes[0].ID, nodes[len(nodes)-1].ID
		dir := newDirectory(hdr.minNodeID, hdr.maxNodeID)
		for _, n := range nodes {
			dir[n.ID-hdr.minNodeID] = offset
			rec := encodeNodeRecord(n)
			if _, err := w.Write(rec); err != nil {
				return err
			}
			offset += int64(len(rec))
		}
		hdr.nodeDirOffset = uint64(offset)
		nodeDirBytes = directoryBytes(dir)
		if _, err := w.Write(nodeDirBytes); err != nil {
			return err
		}
		offset += int64(len(nodeDirBytes))
	}

	if len(edges) > 0 {
		hdr.minEdgeID, hdr.maxEdgeID = edges[0].ID, edges[len(edges)-1].ID
		dir := newDirectory(hdr.minEdgeID, hdr.maxEdgeID)
		for _, e := range edges {
			dir[e.ID-hdr.minEdgeID] = offset
			rec := encodeEdgeRecord(e)
			if _, err := w.Write(rec); err != nil {
				return err
			}
			offset += int64(len(rec))
		}
		hdr.edgeDirOffset = uint64(offset)
		edgeDirBytes = directoryBytes(dir)
		if _, err := w.Write(edgeDirBytes); err != nil {
			return err
		}
		offset += int64(len(edgeDirBytes))
	}

	hdr.metaOffset = uint64(offset)
	hdr.metaLen = uint64(len(metaBytes))
	if _, err := w.Write(metaBytes); err != nil {
		return err
	}

	hdr.crc = computeCRC(hdr.marshal()[:hCRC], nodeDirBytes, edgeDirBytes, metaBytes)

	if err := w.Flush(); err != nil {
		return err
	}
	if _, err := f.WriteAt(hdr.marshal(), 0); err != nil {
		return err
	}
	return f.Sync()
}

func newDirectory(minID, maxID uint64) []int64 {
	dir := make([]int64, maxID-minID+1)
	for i := range dir {
		dir[i] = dirAbsent
	}
	return dir
}

func directoryBytes(dir []int64) []byte {
	b := make([]byte, len(dir)*8)
	for i, off := range dir {
		binary.LittleEndian.PutUint64(b[i*8:], uint64(off))
	}
	return b
}

// writeMmapSnapshot serializes a quiescent GraphStorage (caller ensures no concurrent
// writers, or holds the snapshot RLock) to path in the mmap format.
func writeMmapSnapshot(path string, gs *GraphStorage) error {
	return writeMmapSnapshotData(path, collectNodesSorted(gs), collectEdgesSorted(gs), buildMmapMetadata(gs))
}

// buildMmapMetadata gathers the small eager tail (property/vector indexes, stats,
// nextIDs, sticky label/type keys), mirroring snapshotWithBoundary's extraction.
func buildMmapMetadata(gs *GraphStorage) *mmapMetadata {
	propIdx := make(map[string]PropertyIndexSnapshot, len(gs.propertyIndexes))
	for key, idx := range gs.propertyIndexes {
		idx.mu.RLock()
		propIdx[key] = PropertyIndexSnapshot{
			PropertyKey: idx.propertyKey,
			IndexType:   idx.indexType,
			Index:       cloneStringIDIndex(idx.index),
		}
		idx.mu.RUnlock()
	}
	return &mmapMetadata{
		PropertyIndexes:  propIdx,
		VectorIndexes:    gs.vectorIndex.IndexDefinitions(),
		Stats:            gs.GetStatistics(),
		NextNodeID:       atomic.LoadUint64(&gs.nextNodeID),
		NextEdgeID:       atomic.LoadUint64(&gs.nextEdgeID),
		StickyNodeLabels: labelIndexKeys(gs.nodesByLabel),
		StickyEdgeTypes:  labelIndexKeys(gs.edgesByType),
	}
}

func labelIndexKeys(idx labelIndex) []string {
	keys := make([]string, 0, len(idx))
	for k := range idx {
		keys = append(keys, k)
	}
	return keys
}

func collectNodesSorted(gs *GraphStorage) []*Node {
	nodes := make([]*Node, 0, gs.nodeCount())
	for i := range gs.nodeShards {
		for _, n := range gs.nodeShards[i] {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

func collectEdgesSorted(gs *GraphStorage) []*Edge {
	edges := make([]*Edge, 0, gs.edgeCount())
	for i := range gs.edgeShards {
		for _, e := range gs.edgeShards[i] {
			edges = append(edges, e)
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return edges
}
