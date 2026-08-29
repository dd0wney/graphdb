package storage

// Snapshot write path for the mmap reopen mode (Phase 1d). Writes the merged live
// state (shard overlay ∪ mmap base − tombstones) to snapshot.mmap.

import (
	"fmt"
	"sort"
	"time"
)

// snapshotMmapLocked is the mmap-mode branch of snapshotWithBoundary. Caller holds
// gs.mu.RLock (taken in snapshotWithBoundary); this collects cloned live state under
// that lock, releases it (so the file encode doesn't stall writers — matching the
// JSON path), writes snapshot.mmap atomically, and returns the WAL boundary LSN.
func (gs *GraphStorage) snapshotMmapLocked(boundary uint64) (uint64, error) {
	// Live nodes: shard overlay + non-shadowed, non-tombstoned base, all cloned
	// for isolation (the encode runs after RUnlock).
	nodes := make([]*Node, 0, gs.nodeCount())
	gs.forEachNodeUnlocked(func(n *Node) bool {
		nodes = append(nodes, n.Clone())
		return true
	})

	// Live edges: shard overlay + non-shadowed, non-tombstoned base.
	edges := make([]*Edge, 0, gs.edgeCount())
	for i := range gs.edgeShards {
		for _, e := range gs.edgeShards[i] {
			edges = append(edges, e.Clone())
		}
	}
	var damaged uint64
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
				damaged = id
				return
			}
			edges = append(edges, e)
		})
	}
	if damaged != 0 {
		gs.mu.RUnlock()
		return 0, fmt.Errorf("refusing to write a snapshot: edge record %d in the current snapshot is damaged", damaged)
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
	gs.stats.LastSnapshot = time.Now()
	return boundary, nil
}
