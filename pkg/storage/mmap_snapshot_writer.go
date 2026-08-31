package storage

// Writer for the mmap-able snapshot format (see mmap_snapshot_format.go).
// writeMmapSnapshotData is the pure-format writer (testable without a store);
// writeMmapSnapshot + buildMmapMetadata adapt a quiescent GraphStorage to it.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"sync/atomic"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// writeMmapSnapshotData writes nodes (sorted ascending by ID), edges, and metadata to
// path in the mmap-able v4 format with a CRC over the structural sections.
func writeMmapSnapshotData(path string, nodes []*Node, edges []*Edge, meta *mmapMetadata) error {
	return writeMmapSnapshotDataWithFS(vfs.Default(), path, nodes, edges, meta)
}

func writeMmapSnapshotDataWithFS(fs vfs.FileSystem, path string, nodes []*Node, edges []*Edge, meta *mmapMetadata) error {
	metaBytes, err := meta.marshal()
	if err != nil {
		return err
	}

	f, err := fs.Open(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePermissions)
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
		if err := checkDirectorySpan(uint64(len(nodes)), hdr.minNodeID, hdr.maxNodeID, "node"); err != nil {
			return err
		}
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
		if err := checkDirectorySpan(uint64(len(edges)), hdr.minEdgeID, hdr.maxEdgeID, "edge"); err != nil {
			return err
		}
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

	// CSR adjacency: bucket edge IDs per endpoint, then emit outgoing data,
	// incoming data, and a dense combined directory indexed by nodeID-minNodeID.
	var adjDirBytes []byte
	if len(nodes) > 0 {
		out := make(map[uint64][]uint64, len(nodes))
		in := make(map[uint64][]uint64, len(nodes))
		for _, e := range edges {
			out[e.FromNodeID] = append(out[e.FromNodeID], e.ID)
			in[e.ToNodeID] = append(in[e.ToNodeID], e.ID)
		}
		type adjEntry struct{ outOff, outLen, inOff, inLen int64 }
		entries := make([]adjEntry, hdr.maxNodeID-hdr.minNodeID+1)

		hdr.outCSROffset = uint64(offset)
		for _, n := range nodes {
			ids := out[n.ID]
			entries[n.ID-hdr.minNodeID].outOff = offset
			entries[n.ID-hdr.minNodeID].outLen = int64(len(ids))
			rec := appendCSRRun(nil, ids)
			if _, err := w.Write(rec); err != nil {
				return err
			}
			offset += int64(len(rec))
		}
		hdr.inCSROffset = uint64(offset)
		for _, n := range nodes {
			ids := in[n.ID]
			entries[n.ID-hdr.minNodeID].inOff = offset
			entries[n.ID-hdr.minNodeID].inLen = int64(len(ids))
			rec := appendCSRRun(nil, ids)
			if _, err := w.Write(rec); err != nil {
				return err
			}
			offset += int64(len(rec))
		}
		hdr.adjDirOffset = uint64(offset)
		adjDirBytes = make([]byte, len(entries)*adjDirEntrySize)
		for i, e := range entries {
			b := adjDirBytes[i*adjDirEntrySize:]
			binary.LittleEndian.PutUint64(b[0:], uint64(e.outOff))
			binary.LittleEndian.PutUint64(b[8:], uint64(e.outLen))
			binary.LittleEndian.PutUint64(b[16:], uint64(e.inOff))
			binary.LittleEndian.PutUint64(b[24:], uint64(e.inLen))
		}
		if _, err := w.Write(adjDirBytes); err != nil {
			return err
		}
		offset += int64(len(adjDirBytes))
	}

	// Membership inverted indexes (Stage 2b): per-tenant node/edge enumeration +
	// by-label/by-type. IDs are appended in the (already ascending) node/edge
	// iteration order, so each run is sorted without an extra sort.
	var membRunData, membDirBytes []byte
	{
		mb := newMembershipBuilder()
		for _, n := range nodes {
			tn := string(effectiveTenantID(n.TenantID))
			mb.add(membKindNodeTenant, tn, "", n.ID)
			// Use a seen set to deduplicate labels: a node with repeated labels
			// (e.g. ["Person","Person"]) must only contribute one membership entry
			// per label, matching the in-memory map[uint64]struct{} index which
			// deduplicates naturally.
			seen := make(map[string]struct{}, len(n.Labels))
			for _, label := range n.Labels {
				if _, dup := seen[label]; dup {
					continue
				}
				seen[label] = struct{}{}
				mb.add(membKindNodeLabel, tn, label, n.ID)
			}
		}
		for _, e := range edges {
			te := string(effectiveTenantID(e.TenantID))
			mb.add(membKindEdgeTenant, te, "", e.ID)
			mb.add(membKindEdgeType, te, e.Type, e.ID)
		}
		membRunData, membDirBytes = mb.encode(offset)
	}
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

	hdr.metaOffset = uint64(offset)
	hdr.metaLen = uint64(len(metaBytes))
	if _, err := w.Write(metaBytes); err != nil {
		return err
	}

	hdr.crc = computeCRC(hdr.marshal()[:hCRC], nodeDirBytes, edgeDirBytes, adjDirBytes, membDirBytes, metaBytes)

	// The header goes in last, out of order, because its CRC and its section
	// offsets are only known once the body is written. Flush, back-patch the
	// reserved header at offset 0, then one Sync covers both.
	//
	// THIS FUNCTION DOES NOT MAKE THAT SAFE, AND IT CANNOT. The safety comes
	// from the caller: snapshotMmapLocked passes a .tmp path and renames only
	// after this returns. A power cut anywhere in the window above leaves a
	// torn temporary file that nothing ever publishes. Hand this function a
	// FINAL path and the protection is gone whole — writeMmapSnapshot below
	// does exactly that, which is why its only callers are benchmarks.
	//
	// MEASURED. The github.com/dd0wney/fault session (v0.1.0) went looking for
	// a torn published header on this path and could not build one, for the
	// reason above. crash.Model{Sector: 4096, Cover: crash.Prefixes}, 400 nodes
	// so the body spans more than one sector, 107 states:
	//
	//	states: map[0:21 200:47 400:39]   0 violations, 0 reopen failures
	//
	// Every state holds nothing, the first batch, or both. No published
	// snapshot failed to decode.
	//
	// Two limits it stated. Prefixes was necessary: 14 pending units give 16384
	// states against a cap of 4096, so the sweep does not visit every legal
	// subset. And the check reopens and counts nodes, so a torn snapshot that
	// reopens with the right count is invisible to it.
	//
	// STILL UNMEASURED: whether hdr.crc DETECTS a torn header. No crash on this
	// path can produce one, so a crash sweep is the wrong instrument. vfs.Mapper
	// is the right one — a driver that implements Map hands the reader bytes it
	// chose, so a bad CRC reaches the production read path with no corrupt file
	// on any disk. Nobody has run that yet.
	if err := w.Flush(); err != nil {
		return err
	}
	if _, err := f.WriteAt(hdr.marshal(), 0); err != nil {
		return err
	}
	return f.Sync()
}

// maxDenseDirectorySpan and denseDirectoryFillRatio bound the dense directory
// the writer builds. See checkDirectorySpan.
const (
	maxDenseDirectorySpan   uint64 = 1 << 26 // 67,108,864 entries, a 512 MiB directory
	denseDirectoryFillRatio uint64 = 64      // refuse below about 1.6% occupancy
)

// checkDirectorySpan refuses a snapshot whose dense directory would be both
// enormous and almost entirely holes.
//
// This is the write-side counterpart to checkDirRange in the reader, which
// refuses a snapshot whose declared span disagrees with the directory the file
// carries. Until this existed the span was validated coming OFF the disk and
// not going back on: the writer took nodes[len-1].ID on trust, and newDirectory
// allocated and filled maxID-minID+1 entries from it.
//
// A crash sweep reached that. The record area lies outside computeCRC, so one
// flipped byte in an ID field passes the checksum, loads, sorts last, and
// becomes maxID. The next ordinary Close re-publishes and fills a directory
// sized by the garbage.
//
// The measured case: one flipped byte gave minNodeID 2 and maxNodeID
// 4278190081, a span of 4,278,190,080. newDirectory fills at 3.8 ns/entry, so
// that span is 16.3 seconds of CPU. It allocates make([]int64, span) at 34 GB,
// and directoryBytes then allocates a SECOND buffer of len(dir)*8 — peak demand
// is about 69 GB, not 34. Both runs ended in the OOM killer, one of them in
// this repository's own test suite before the guard existed.
//
// TWO conditions, and both are required. An absolute cap alone would refuse a
// large healthy store for being large, which trades a rare hang for a routine
// outage. The occupancy test is what separates "big" from "wrong": a dense
// directory earns its shape only while it is mostly full, so a store whose IDs
// are dense is never refused at any size.
//
// Refusing is safe. persistence.go:408 truncates the WAL ONLY after a snapshot
// that succeeded, so on a refusal the old snapshot and the WAL remain the
// recovery pair. The damaged-record refusal (#521) already relies on that
// invariant, and this reuses its wording so the two read alike in a log.
func checkDirectorySpan(count, minID, maxID uint64, what string) error {
	if count == 0 {
		return nil
	}
	if maxID < minID {
		return fmt.Errorf("refusing to write a snapshot: %s ID range inverted, min %d > max %d",
			what, minID, maxID)
	}
	span := maxID - minID + 1
	if span <= maxDenseDirectorySpan || span/count <= denseDirectoryFillRatio {
		return nil
	}
	return fmt.Errorf("refusing to write a snapshot: %d %s records span IDs %d..%d, so a dense "+
		"directory needs %d entries and would be %d%% empty; the snapshot on disk and the WAL "+
		"remain the recovery pair", count, what, minID, maxID, span, 100-(count*100/span))
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
	return writeMmapSnapshotDataWithFS(gs.fs, path, collectNodesSorted(gs), collectEdgesSorted(gs), buildMmapMetadata(gs))
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
	tenantStats := make(map[string]TenantStats, len(gs.tenantStats))
	for tid, st := range gs.tenantStats {
		if st != nil {
			tenantStats[string(tid)] = *st
		}
	}
	return &mmapMetadata{
		PropertyIndexes:  propIdx,
		VectorIndexes:    gs.vectorIndex.IndexDefinitions(),
		Stats:            gs.GetStatistics(),
		NextNodeID:       atomic.LoadUint64(&gs.nextNodeID),
		NextEdgeID:       atomic.LoadUint64(&gs.nextEdgeID),
		StickyNodeLabels: labelIndexKeys(gs.nodesByLabel),
		StickyEdgeTypes:  labelIndexKeys(gs.edgesByType),
		TenantStats:      tenantStats,
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
