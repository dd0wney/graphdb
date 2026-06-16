package storage

// PROTOTYPE writer for the mmap-able snapshot format (see mmap_proto_format.go).
// Serializes a quiescent GraphStorage's nodes and edges into the binary layout.
// Not wired into Snapshot()/Close(); intended to be called on a just-built store
// in the benchmark.

import (
	"bufio"
	"encoding/binary"
	"os"
	"sort"
)

// writeMmapSnapshot writes gs's nodes and edges to path in the mmap-able format.
// The caller must ensure no concurrent writers (the benchmark builds then writes).
func writeMmapSnapshot(path string, gs *GraphStorage) error {
	nodes := collectNodesSorted(gs)
	edges := collectEdgesSorted(gs)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 1<<20)
	hdr := &mmapProtoHeader{nodeCount: uint64(len(nodes)), edgeCount: uint64(len(edges))}

	// Header placeholder; real offsets backfilled via WriteAt after the body.
	offset := int64(mmapHeaderSize)
	if _, err := w.Write(make([]byte, mmapHeaderSize)); err != nil {
		return err
	}

	// Node records + dense directory.
	if len(nodes) > 0 {
		hdr.minNodeID = nodes[0].ID
		hdr.maxNodeID = nodes[len(nodes)-1].ID
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
		offset += writeDirectory(w, dir)
	}

	// Edge records + dense directory.
	if len(edges) > 0 {
		hdr.minEdgeID = edges[0].ID
		hdr.maxEdgeID = edges[len(edges)-1].ID
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
		offset += writeDirectory(w, dir)
	}

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

// writeDirectory writes dir as little-endian int64s and returns bytes written.
func writeDirectory(w *bufio.Writer, dir []int64) int64 {
	var b [8]byte
	for _, off := range dir {
		binary.LittleEndian.PutUint64(b[:], uint64(off))
		_, _ = w.Write(b[:])
	}
	return int64(len(dir)) * 8
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
