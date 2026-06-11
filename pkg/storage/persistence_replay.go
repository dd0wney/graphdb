package storage

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/dd0wney/graphdb/pkg/tenantid"
	"github.com/dd0wney/graphdb/pkg/wal"
)

// replayWAL replays WAL entries to recover state
func (gs *GraphStorage) replayWAL() error {
	if gs.useBatching && gs.batchedWAL != nil {
		return gs.batchedWAL.Replay(func(entry *wal.Entry) error {
			return gs.replayEntry(entry)
		})
	} else if gs.useCompression && gs.compressedWAL != nil {
		// This branch was MISSING: even entries that did reach the
		// compressed WAL (the batch executor's appendToWAL) were never
		// replayed on recovery.
		return gs.compressedWAL.Replay(func(entry *wal.Entry) error {
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
	case wal.OpUpdateEdge:
		return gs.replayUpdateEdge(entry)
	case wal.OpDeleteEdge:
		return gs.replayDeleteEdge(entry)
	case wal.OpDeleteNode:
		return gs.replayDeleteNode(entry)
	case wal.OpCreatePropertyIndex:
		return gs.replayCreatePropertyIndex(entry)
	case wal.OpDropPropertyIndex:
		return gs.replayDropPropertyIndex(entry)
	case wal.OpCreateVectorIndex:
		return gs.replayCreateVectorIndex(entry)
	case wal.OpDropVectorIndex:
		return gs.replayDropVectorIndex(entry)
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
		addToLabelIndex(gs.nodesByLabel, label, node.ID)
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

// replayUpdateEdge replays an edge property/weight update recovered from the WAL.
// UpdateEdge / the upsert-update path enqueue the FULL post-update Edge under
// wal.OpUpdateEdge, so applying it is a straight replace of the stored edge's
// Properties + Weight. Edge updates never change type/from/to, so the type
// index and adjacency lists are untouched (unlike replayCreateEdge). Sibling of
// replayUpdateNode; without this case a post-snapshot edge update was silently
// reverted to its snapshot state on recovery.
func (gs *GraphStorage) replayUpdateEdge(entry *wal.Entry) error {
	var edge Edge
	if err := json.Unmarshal(entry.Data, &edge); err != nil {
		return err
	}

	// Skip if the edge doesn't exist (e.g. deleted later in the WAL).
	existing, exists := gs.lookupEdgeShard(edge.ID)
	if !exists {
		return nil
	}

	existing.Properties = edge.Properties
	existing.Weight = edge.Weight
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
	addToLabelIndex(gs.edgesByType, edge.Type, edge.ID)

	// Rebuild the per-tenant type index too — not just the global one.
	// Sibling of replayCreateNode's addNodeToTenantIndex (H4.3): without
	// this, a crash-restart with WAL-only edges leaves
	// GetEdgesByTypeForTenant returning nil. addEdgeToTenantIndex also
	// restores per-tenant EdgeCount stats.
	gs.addEdgeToTenantIndex(&edge)

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

	// Remove from the per-tenant edge index (type map + enumeration set + tenant
	// EdgeCount), mirroring the live DeleteEdge. loadFromDisk rebuilt the tenant
	// index over the SNAPSHOT edge set before replay, so without this a
	// post-snapshot DeleteEdge recovered here leaves the edge in
	// CountEdgesForTenant / GetEdgesByTypeForTenant forever. Replay is
	// single-threaded during init.
	gs.removeEdgeFromTenantIndex(&edge)

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

	// Remove from the per-tenant node index (label map + enumeration set + tenant
	// NodeCount), mirroring the live DeleteNode. loadFromDisk rebuilt the tenant
	// index over the SNAPSHOT node set before replay, so without this a
	// post-snapshot DeleteNode recovered here leaves the node in
	// CountNodesForTenant / GetNodesByLabelForTenant forever. The WAL delete
	// entry carries the full node (TenantID + Labels), so the removal routes to
	// the right tenant. Replay is single-threaded during init.
	gs.removeNodeFromTenantIndex(&node)

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

// replayCreateVectorIndex restores a vector index DEFINITION recovered from the
// WAL (an index created after the last snapshot). Only the definition is
// recreated here — the HNSW graph is rebuilt from the final post-replay node
// set by rebuildVectorIndexesFromNodes, which runs after replayWAL completes.
// Populating here would be both wasted work and wrong (it would run before the
// WAL-recovered nodes exist). Idempotent: a definition already present from the
// snapshot is left untouched.
func (gs *GraphStorage) replayCreateVectorIndex(entry *wal.Entry) error {
	var def VectorIndexDef
	if err := json.Unmarshal(entry.Data, &def); err != nil {
		return err
	}

	tid := tenantid.TenantID(def.TenantID)
	if tid.IsEmpty() {
		tid = tenantid.Default
	}
	if gs.vectorIndex.HasIndexForTenant(tid, def.PropertyName) {
		return nil
	}
	if err := gs.vectorIndex.CreateIndexForTenant(
		tid, def.PropertyName, def.Dimensions, def.M, def.EfConstruction, def.Metric,
	); err != nil {
		return fmt.Errorf("failed to recreate vector index %s/%s during replay: %w", def.TenantID, def.PropertyName, err)
	}
	return nil
}

// replayDropVectorIndex replays a vector-index drop recovered from the WAL (a
// drop applied after the last snapshot). Without it, the snapshotted definition
// would resurrect on recovery. A missing index is a no-op — the drop may target
// a definition that was never snapshotted.
func (gs *GraphStorage) replayDropVectorIndex(entry *wal.Entry) error {
	var info dropVectorIndexWAL
	if err := json.Unmarshal(entry.Data, &info); err != nil {
		return err
	}

	tid := tenantid.TenantID(info.TenantID)
	if tid.IsEmpty() {
		tid = tenantid.Default
	}
	// Ignore "no such index" — the drop is idempotent on replay.
	_ = gs.vectorIndex.DropIndexForTenant(tid, info.PropertyName)
	return nil
}
