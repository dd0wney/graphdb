package storage

// Snapshot write path for the mmap reopen mode (Phase 1d). Writes the merged live
// state (shard overlay ∪ mmap base − tombstones) to snapshot.mmap.

import (
	"fmt"
	"sort"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// snapshotMmapLocked is the mmap-mode branch of snapshotWithBoundary. Caller holds
// gs.mu.RLock (taken in snapshotWithBoundary); this collects cloned live state under
// that lock, releases it (so the file encode doesn't stall writers — matching the
// JSON path), writes snapshot.mmap atomically, and returns the WAL boundary LSN.
func (gs *GraphStorage) snapshotMmapLocked(boundary uint64) (uint64, error) {
	// Live nodes: shard overlay + non-shadowed, non-tombstoned base. The shard
	// nodes are cloned for isolation (the encode runs after RUnlock); a decoded
	// base record is already a fresh heap object, so it needs no second copy.
	//
	// This walks the overlay and the base inline instead of calling
	// gs.forEachNodeUnlocked. That helper cannot report a decode failure — its
	// callback returns bool, not error — so it SKIPS a damaged base record.
	// A skip costs a reader one absent node. It costs this function the record
	// itself: the write would complete without it, and the next open would
	// find neither the record nor its directory entry. The edge branch below
	// refuses for that reason, and this branch now matches it.
	nodes := make([]*Node, 0, gs.nodeCount())
	for i := range gs.nodeShards {
		for _, n := range gs.nodeShards[i] {
			nodes = append(nodes, n.Clone())
		}
	}
	var damagedNode uint64
	if gs.mmapSnap != nil {
		gs.mmapSnap.forEachNodeID(func(id uint64, off int64) {
			if _, shadowed := gs.lookupNodeShard(id); shadowed || gs.isNodeDeletedLocked(id) {
				return
			}
			n, err := decodeNodeRecordAt(gs.mmapSnap.data, off)
			if err != nil {
				// Refuse rather than drop, as the edge branch does.
				damagedNode = id
				return
			}
			nodes = append(nodes, n)
		})
	}
	if damagedNode != 0 {
		gs.mu.RUnlock()
		return 0, fmt.Errorf("refusing to write a snapshot: node record %d in the current snapshot is damaged", damagedNode)
	}

	// Live edges: shard overlay + non-shadowed, non-tombstoned base.
	edges := make([]*Edge, 0, gs.edgeCount())
	for i := range gs.edgeShards {
		for _, e := range gs.edgeShards[i] {
			edges = append(edges, e.Clone())
		}
	}
	var damagedEdge uint64
	if gs.mmapSnap != nil {
		gs.mmapSnap.forEachEdgeID(func(id uint64, off int64) {
			if _, shadowed := gs.lookupEdgeShard(id); shadowed || gs.isEdgeDeletedLocked(id) {
				return
			}
			e, err := decodeEdgeRecordAt(gs.mmapSnap.data, off)
			if err != nil {
				// Refuse rather than drop. Skipping the record here would turn
				// a damaged byte into permanent data loss at the next
				// snapshot, and the operator would never see it happen.
				damagedEdge = id
				return
			}
			edges = append(edges, e)
		})
	}
	if damagedEdge != 0 {
		gs.mu.RUnlock()
		return 0, fmt.Errorf("refusing to write a snapshot: edge record %d in the current snapshot is damaged", damagedEdge)
	}

	meta := buildMmapMetadata(gs)
	gs.mu.RUnlock()

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })

	finalPath := mmapSnapshotPath(gs.dataDir)
	tmpPath := finalPath + ".tmp"
	if err := writeMmapSnapshotDataWithFS(gs.fs, tmpPath, nodes, edges, meta); err != nil {
		return 0, fmt.Errorf("failed to write mmap snapshot: %w", err)
	}
	if err := gs.fs.Rename(tmpPath, finalPath); err != nil {
		return 0, fmt.Errorf("failed to rename mmap snapshot: %w", err)
	}
	// The rename is atomic, and that is not the same as durable. The new name
	// is an entry in the data directory, and syncing the temporary file wrote
	// the file, not the directory that holds its name. A power cut here can
	// lose the entry: the snapshot's bytes are on the disk and nothing points
	// at them. Reporting the error matters as much as making the call — a
	// caller that checkpoints the WAL on the strength of this snapshot must
	// not be told the snapshot is durable when the name is not.
	//
	// MEASURED, not argued. A crash sweep by the github.com/dd0wney/fault
	// session (v0.1.0, harness fault-graphdb-sweep, beside this repository and
	// not in it) generated the on-disk states a power cut can leave here:
	//
	//	e418768 (#529), before #530 added this call   204 states,  45 violations
	//	2df539b (#535) and main 6686def                74 states,   0 violations
	//
	// One failing state, named structurally:
	//
	//	after=marker|first-publish-returned:create1/lost=data|snapshot.mmap.tmp:rename1
	//
	// Conditions, because a figure without them is the defect this comment
	// exists to prevent. crash.Model{}, so a unit is a whole Write call and no
	// sector splitting applies. Two publishes, with a marker written between
	// them in its OWN directory; the harness syncs the marker's directory and
	// deliberately not this one, because syncing this one would make the rename
	// durable and hide the defect. The rule is: if the marker is durable then
	// the first publish returned before the crash, so the store must hold at
	// least the first batch.
	//
	// What the numbers do NOT say. Two publishes alone do not discriminate. The
	// same session measured that with the marker rule off and everything else
	// unchanged, e418768 PASSES. The rule does the work; the second publish
	// only makes the rule expressible. Copying this shape without the marker
	// rule produces a sweep that cannot fail.
	if err := vfs.SyncParentDir(gs.fs, finalPath); err != nil {
		return 0, fmt.Errorf("failed to sync the data directory after publishing the mmap snapshot: %w", err)
	}
	gs.stats.LastSnapshot = time.Now()
	return boundary, nil
}
