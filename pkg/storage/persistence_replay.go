package storage

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// replayWAL replays WAL entries to recover state
func (gs *GraphStorage) replayWAL() error {
	if gs.useBatching && gs.batchedWAL != nil {
		return gs.batchedWAL.Replay(func(entry *wal.Entry) error {
			return gs.replayEntry(entry)
		})
	} else if gs.wal != nil {
		return gs.wal.Replay(func(entry *wal.Entry) error {
			return gs.replayEntry(entry)
		})
	}
	return nil
}

// replayEntry replays a single WAL entry
func (gs *GraphStorage) replayEntry(entry *wal.Entry) error {
	switch entry.OpType {
	case wal.OpCreateNode:
		return gs.replayCreateNode(entry)
	case wal.OpUpdateNode:
		return gs.replayUpdateNode(entry)
	case wal.OpCreateEdge:
		return gs.replayCreateEdge(entry)
	case wal.OpDeleteEdge:
		return gs.replayDeleteEdge(entry)
	case wal.OpDeleteNode:
		return gs.replayDeleteNode(entry)
	case wal.OpCreatePropertyIndex:
		return gs.replayCreatePropertyIndex(entry)
	case wal.OpDropPropertyIndex:
		return gs.replayDropPropertyIndex(entry)
	case wal.OpAddNodeLabels:
		return gs.replayAddNodeLabels(entry)
	case wal.OpRemoveNodeLabel:
		return gs.replayRemoveNodeLabel(entry)
	}
	return nil
}

func (gs *GraphStorage) replayCreateNode(entry *wal.Entry) error {
	var node Node
	if err := json.Unmarshal(entry.Data, &node); err != nil {
		return err
	}

	// Skip if node already exists (already in snapshot)
	if _, exists := gs.lookupNodeShard(node.ID); exists {
		return nil
	}

	// Replay node creation
	gs.storeNodeInShard(&node)
	for _, label := range node.Labels {
		gs.nodesByLabel[label] = append(gs.nodesByLabel[label], node.ID)
	}
	// H4.3: mirror the tenant-scoped label index. Without this, the
	// per-tenant GraphQL schema generator (which lists labels from
	// tenantNodesByLabel, not the global nodesByLabel) sees the tenant
	// as labelless after a restart, so `{ tasks { id } }` 400s with
	// "Cannot query field" until the tenant takes its next write.
	gs.addNodeToTenantIndex(&node)
	if gs.outgoingEdges[node.ID] == nil {
		gs.outgoingEdges[node.ID] = make([]uint64, 0)
	}
	if gs.incomingEdges[node.ID] == nil {
		gs.incomingEdges[node.ID] = make([]uint64, 0)
	}

	// Insert into property indexes if they exist
	if err := gs.insertNodeIntoPropertyIndexes(node.ID, node.Properties); err != nil {
		return fmt.Errorf("failed to insert node %d into property indexes: %w", node.ID, err)
	}

	// Update stats atomically
	atomic.AddUint64(&gs.stats.NodeCount, 1)

	// Update next ID if necessary
	if node.ID >= gs.nextNodeID {
		gs.nextNodeID = node.ID + 1
	}

	return nil
}

func (gs *GraphStorage) replayUpdateNode(entry *wal.Entry) error {
	var updateInfo struct {
		NodeID     uint64
		Properties map[string]Value
	}
	if err := json.Unmarshal(entry.Data, &updateInfo); err != nil {
		return err
	}

	// Skip if node doesn't exist
	node, exists := gs.lookupNodeShard(updateInfo.NodeID)
	if !exists {
		return nil
	}

	// Update property indexes
	if err := gs.updatePropertyIndexes(updateInfo.NodeID, node, updateInfo.Properties); err != nil {
		return fmt.Errorf("failed to update property indexes for node %d: %w", updateInfo.NodeID, err)
	}

	// Apply property updates
	for key, value := range updateInfo.Properties {
		node.Properties[key] = value
	}

	return nil
}

func (gs *GraphStorage) replayCreateEdge(entry *wal.Entry) error {
	var edge Edge
	if err := json.Unmarshal(entry.Data, &edge); err != nil {
		return err
	}

	// Skip if edge already exists (already in snapshot)
	if _, exists := gs.lookupEdgeShard(edge.ID); exists {
		return nil
	}

	// Replay edge creation
	gs.storeEdgeInShard(&edge)
	gs.edgesByType[edge.Type] = append(gs.edgesByType[edge.Type], edge.ID)

	// Rebuild adjacency lists (disk-backed or in-memory)
	if err := gs.storeOutgoingEdge(edge.FromNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to store outgoing edge during replay: %w", err)
	}
	if err := gs.storeIncomingEdge(edge.ToNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to store incoming edge during replay: %w", err)
	}

	// Update stats atomically
	atomic.AddUint64(&gs.stats.EdgeCount, 1)

	// Update next ID if necessary
	if edge.ID >= gs.nextEdgeID {
		gs.nextEdgeID = edge.ID + 1
	}

	return nil
}

func (gs *GraphStorage) replayDeleteEdge(entry *wal.Entry) error {
	var edge Edge
	if err := json.Unmarshal(entry.Data, &edge); err != nil {
		return err
	}

	// Skip if edge doesn't exist (already deleted or never existed)
	if _, exists := gs.lookupEdgeShard(edge.ID); !exists {
		return nil
	}

	// Replay edge deletion
	gs.deleteEdgeShardEntry(edge.ID)

	// Remove from type index
	gs.removeEdgeFromTypeIndex(edge.Type, edge.ID)

	// Remove from adjacency lists (disk-backed or in-memory)
	if err := gs.removeOutgoingEdge(edge.FromNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to remove outgoing edge during replay: %w", err)
	}
	if err := gs.removeIncomingEdge(edge.ToNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to remove incoming edge during replay: %w", err)
	}

	// Decrement stats with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)

	return nil
}

func (gs *GraphStorage) replayDeleteNode(entry *wal.Entry) error {
	var node Node
	if err := json.Unmarshal(entry.Data, &node); err != nil {
		return err
	}

	// Skip if node doesn't exist (already deleted or never existed)
	if _, exists := gs.lookupNodeShard(node.ID); !exists {
		return nil
	}

	// Get edges to delete (disk-backed or in-memory)
	var outgoingEdgeIDs, incomingEdgeIDs []uint64
	if gs.useDiskBackedEdges {
		var err error
		outgoingEdgeIDs, err = gs.edgeStore.GetOutgoingEdges(node.ID)
		if err != nil {
			return fmt.Errorf("failed to get outgoing edges for node %d during replay: %w", node.ID, err)
		}
		incomingEdgeIDs, err = gs.edgeStore.GetIncomingEdges(node.ID)
		if err != nil {
			return fmt.Errorf("failed to get incoming edges for node %d during replay: %w", node.ID, err)
		}
	} else {
		outgoingEdgeIDs = gs.outgoingEdges[node.ID]
		incomingEdgeIDs = gs.incomingEdges[node.ID]
	}

	// Cascade delete all outgoing edges during replay
	for _, edgeID := range outgoingEdgeIDs {
		if err := gs.cascadeDeleteOutgoingEdge(edgeID); err != nil {
			return fmt.Errorf("failed to cascade delete outgoing edge %d during replay: %w", edgeID, err)
		}
	}

	// Cascade delete all incoming edges during replay
	for _, edgeID := range incomingEdgeIDs {
		if err := gs.cascadeDeleteIncomingEdge(edgeID); err != nil {
			return fmt.Errorf("failed to cascade delete incoming edge %d during replay: %w", edgeID, err)
		}
	}

	// Remove from label indexes
	for _, label := range node.Labels {
		gs.removeFromLabelIndex(label, node.ID)
	}

	// Remove from property indexes
	if err := gs.removeNodeFromPropertyIndexes(node.ID, node.Properties); err != nil {
		return fmt.Errorf("failed to remove node %d from property indexes: %w", node.ID, err)
	}

	// Delete node
	gs.deleteNodeShardEntry(node.ID)

	// Delete adjacency lists (disk-backed or in-memory)
	if err := gs.clearNodeAdjacency(node.ID); err != nil {
		return fmt.Errorf("failed to clear adjacency for node %d during replay: %w", node.ID, err)
	}

	// Decrement stats with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.NodeCount)

	return nil
}

func (gs *GraphStorage) replayCreatePropertyIndex(entry *wal.Entry) error {
	var indexInfo struct {
		PropertyKey string
		ValueType   ValueType
	}
	if err := json.Unmarshal(entry.Data, &indexInfo); err != nil {
		return err
	}

	// Skip if index already exists
	if _, exists := gs.propertyIndexes[indexInfo.PropertyKey]; exists {
		return nil
	}

	// Create index and populate with existing nodes
	idx := NewPropertyIndex(indexInfo.PropertyKey, indexInfo.ValueType)
	var insertErr error
	gs.forEachNodeUnlocked(func(node *Node) bool {
		if prop, exists := node.Properties[indexInfo.PropertyKey]; exists {
			if prop.Type == indexInfo.ValueType {
				if err := idx.Insert(node.ID, prop); err != nil {
					insertErr = fmt.Errorf("failed to insert node %d into property index %s during replay: %w", node.ID, indexInfo.PropertyKey, err)
					return false
				}
			}
		}
		return true
	})
	if insertErr != nil {
		return insertErr
	}
	gs.propertyIndexes[indexInfo.PropertyKey] = idx

	return nil
}

func (gs *GraphStorage) replayDropPropertyIndex(entry *wal.Entry) error {
	var indexInfo struct {
		PropertyKey string
	}
	if err := json.Unmarshal(entry.Data, &indexInfo); err != nil {
		return err
	}

	// Remove index
	delete(gs.propertyIndexes, indexInfo.PropertyKey)

	return nil
}

// replayAddNodeLabels re-applies an OpAddNodeLabels entry during WAL
// recovery. The payload only carries newly-added labels (the storage
// op dedupes against the node's current set before writing the WAL),
// so during replay we can append unconditionally without a second
// dedup pass — provided the snapshot the WAL replays on top of
// matches the WAL's view of the world at the time it was written.
// If a snapshot already includes one of these labels (e.g., the
// snapshot was taken after the add), we still need to dedup here
// because snapshot+WAL replay isn't write-causal. Be defensive.
func (gs *GraphStorage) replayAddNodeLabels(entry *wal.Entry) error {
	var info struct {
		NodeID uint64
		Labels []string
	}
	if err := json.Unmarshal(entry.Data, &info); err != nil {
		return err
	}

	// Skip if node doesn't exist (already deleted by a later WAL entry,
	// or never made it into the snapshot). Mirrors replayUpdateNode.
	node, exists := gs.lookupNodeShard(info.NodeID)
	if !exists {
		return nil
	}

	// Dedup against the live label set to keep replay idempotent under
	// snapshot-overlap. Same set semantics as the live path.
	present := make(map[string]struct{}, len(node.Labels))
	for _, l := range node.Labels {
		present[l] = struct{}{}
	}

	tid := effectiveTenantID(node.TenantID)
	if gs.tenantNodesByLabel[tid] == nil {
		gs.tenantNodesByLabel[tid] = make(map[string][]uint64)
	}

	for _, label := range info.Labels {
		if _, alreadyOn := present[label]; alreadyOn {
			continue
		}
		node.Labels = append(node.Labels, label)
		present[label] = struct{}{}
		gs.nodesByLabel[label] = append(gs.nodesByLabel[label], info.NodeID)
		gs.tenantNodesByLabel[tid][label] = append(gs.tenantNodesByLabel[tid][label], info.NodeID)
	}

	return nil
}

// replayRemoveNodeLabel re-applies an OpRemoveNodeLabel entry during
// WAL recovery. Safe to no-op if the node is gone (deleted by a later
// entry) or the label is already absent (the snapshot was taken after
// the removal).
func (gs *GraphStorage) replayRemoveNodeLabel(entry *wal.Entry) error {
	var info struct {
		NodeID uint64
		Label  string
	}
	if err := json.Unmarshal(entry.Data, &info); err != nil {
		return err
	}

	node, exists := gs.lookupNodeShard(info.NodeID)
	if !exists {
		return nil
	}

	idx := -1
	for i, l := range node.Labels {
		if l == info.Label {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Already absent (snapshot includes the removal, or a prior
		// replay entry already removed it). No-op.
		return nil
	}

	node.Labels = append(node.Labels[:idx], node.Labels[idx+1:]...)
	gs.removeFromLabelIndex(info.Label, info.NodeID)

	// Tenant-scoped label index — same shape as removeNodeFromTenantIndex
	// but for a single label.
	tid := effectiveTenantID(node.TenantID)
	if labelMap := gs.tenantNodesByLabel[tid]; labelMap != nil {
		ids := labelMap[info.Label]
		for i, id := range ids {
			if id == info.NodeID {
				labelMap[info.Label] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		if len(labelMap[info.Label]) == 0 {
			delete(labelMap, info.Label)
		}
	}

	return nil
}
