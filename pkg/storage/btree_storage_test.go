package storage

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// setupBTreeTestGraph builds an isolated BTreeGraphStorage rooted at a
// temp dir; the returned cleanup closes it and removes the dir.
func setupBTreeTestGraph(t *testing.T) (*BTreeGraphStorage, func()) {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "btree-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	gs, err := NewBTreeGraphStorage(dataDir)
	if err != nil {
		os.RemoveAll(dataDir)
		t.Fatalf("failed to create btree storage: %v", err)
	}

	return gs, func() {
		_ = gs.Close()
		_ = os.RemoveAll(dataDir)
	}
}

func TestBTreeGraphStorage_NodeOperations(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	tenantID := "tenant1"
	labels := []string{"Person"}
	props := map[string]Value{
		"name": StringValue("Alice"),
	}

	node, err := gs.CreateNodeWithTenant(tenantID, labels, props)
	assert.NoError(t, err)
	assert.NotNil(t, node)
	assert.Equal(t, tenantID, node.TenantID)

	retrieved, err := gs.GetNodeForTenant(node.ID, tenantID)
	assert.NoError(t, err)
	assert.Equal(t, node.ID, retrieved.ID)
	name, _ := retrieved.Properties["name"].AsString()
	assert.Equal(t, "Alice", name)

	nodes := gs.GetNodesByLabelForTenant(tenantID, "Person")
	assert.Len(t, nodes, 1)
	assert.Equal(t, node.ID, nodes[0].ID)

	// Cross-tenant isolation: per CLAUDE.md tenant-strict rule, a lookup
	// from a different tenant must return ErrNodeNotFound (not a distinct
	// error) to avoid existence-leak side channels.
	_, err = gs.GetNodeForTenant(node.ID, "tenant2")
	assert.ErrorIs(t, err, ErrNodeNotFound)
}

func TestBTreeGraphStorage_EdgeOperations(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	tenantID := "tenant1"
	n1, err := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
	assert.NoError(t, err)
	n2, err := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
	assert.NoError(t, err)

	edge, err := gs.CreateEdgeWithTenant(tenantID, n1.ID, n2.ID, "FOLLOWS", nil, 1.0)
	assert.NoError(t, err)
	assert.NotNil(t, edge)

	outgoing, err := gs.GetOutgoingEdgesForTenant(n1.ID, tenantID)
	assert.NoError(t, err)
	assert.Len(t, outgoing, 1)
	assert.Equal(t, edge.ID, outgoing[0].ID)

	incoming, err := gs.GetIncomingEdgesForTenant(n2.ID, tenantID)
	assert.NoError(t, err)
	assert.Len(t, incoming, 1)
	assert.Equal(t, edge.ID, incoming[0].ID)

	got, err := gs.GetEdgeForTenant(edge.ID, tenantID)
	assert.NoError(t, err)
	assert.Equal(t, edge.ID, got.ID)
	assert.Equal(t, "FOLLOWS", got.Type)

	// Cross-tenant edge lookup must miss the same way node lookup does.
	_, err = gs.GetEdgeForTenant(edge.ID, "tenant2")
	assert.ErrorIs(t, err, ErrEdgeNotFound)
}

func TestBTreeGraphStorage_MetadataOperations(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	tenantID := "tenant1"
	_, err := gs.CreateNodeWithTenant(tenantID, []string{"Person"}, nil)
	assert.NoError(t, err)
	_, err = gs.CreateNodeWithTenant(tenantID, []string{"Company"}, nil)
	assert.NoError(t, err)

	persons := gs.GetNodesByLabelForTenant(tenantID, "Person")
	companies := gs.GetNodesByLabelForTenant(tenantID, "Company")
	_, err = gs.CreateEdgeWithTenant(tenantID, persons[0].ID, companies[0].ID, "WORKS_AT", nil, 1.0)
	assert.NoError(t, err)

	labels := gs.GetLabelsForTenant(tenantID)
	assert.ElementsMatch(t, []string{"Person", "Company"}, labels)

	types := gs.GetEdgeTypesForTenant(tenantID)
	assert.ElementsMatch(t, []string{"WORKS_AT"}, types)

	stats := gs.GetStatistics()
	assert.Equal(t, uint64(2), stats.NodeCount)
	assert.Equal(t, uint64(1), stats.EdgeCount)
}

func TestBTreeGraphStorage_MultiTenancy(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	_, err := gs.CreateNodeWithTenant("tenantA", []string{"User"}, map[string]Value{"name": StringValue("Alice")})
	assert.NoError(t, err)
	_, err = gs.CreateNodeWithTenant("tenantB", []string{"User"}, map[string]Value{"name": StringValue("Bob")})
	assert.NoError(t, err)

	nodesA := gs.GetAllNodesForTenant("tenantA")
	assert.Len(t, nodesA, 1)
	nameA, _ := nodesA[0].Properties["name"].AsString()
	assert.Equal(t, "Alice", nameA)

	nodesB := gs.GetAllNodesForTenant("tenantB")
	assert.Len(t, nodesB, 1)
	nameB, _ := nodesB[0].Properties["name"].AsString()
	assert.Equal(t, "Bob", nameB)

	allNodes := gs.GetAllNodesAcrossTenants()
	assert.Len(t, allNodes, 2)
}

// TestBTreeGraphStorage_StubsReturnTypedError pins the stub policy: every
// unimplemented write method routes through errBTreeBackendUnsupported,
// not silent (nil, nil). This is the load-bearing safety net for callers
// who might wire this backend in before C2.1 / R1 land.
func TestBTreeGraphStorage_StubsReturnTypedError(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	tenantID := "tenantS"

	_, err := gs.CreateNodeWithUniquePropertyForTenant(tenantID, []string{"Claim"}, nil, "Claim", "id")
	assert.ErrorIs(t, err, errBTreeBackendUnsupported,
		"B-lite uniqueness primitive must not silently succeed — graphdb-coord depends on it")

	assert.ErrorIs(t, gs.UpdateNodeForTenant(0, nil, tenantID), errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.RemoveNodePropertiesForTenant(0, nil, tenantID), errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.UpdateEdgeForTenant(0, nil, nil, tenantID), errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.DeleteEdgeForTenant(0, tenantID), errBTreeBackendUnsupported)

	_, _, err = gs.UpsertEdgeWithTenant(tenantID, 0, 0, "T", nil, 0)
	assert.ErrorIs(t, err, errBTreeBackendUnsupported)

	_, err = gs.CreateNode(nil, nil)
	assert.ErrorIs(t, err, errBTreeBackendUnsupported)
	_, err = gs.CreateEdge(0, 0, "T", nil, 0)
	assert.ErrorIs(t, err, errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.UpdateNode(0, nil), errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.DeleteNode(0), errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.RemoveNodeProperties(0, nil), errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.UpdateEdge(0, nil, nil), errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.DeleteEdge(0), errBTreeBackendUnsupported)

	// Vector ops are likewise stubbed pending R1.
	_, err = gs.VectorSearch("p", nil, 0, 0)
	assert.ErrorIs(t, err, errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.CreateVectorIndex("p", 0, 0, 0, ""), errBTreeBackendUnsupported)
	assert.ErrorIs(t, gs.DropVectorIndex("p"), errBTreeBackendUnsupported)
	_, err = gs.GetVectorIndexMetric("p")
	assert.ErrorIs(t, err, errBTreeBackendUnsupported)

	// FindNodesBy* property paths likewise.
	_, err = gs.FindNodesByPropertyForTenant("k", StringValue("v"), tenantID)
	assert.ErrorIs(t, err, errBTreeBackendUnsupported)
	_, err = gs.FindNodesByPropertyIndexedForTenant("k", StringValue("v"), tenantID)
	assert.ErrorIs(t, err, errBTreeBackendUnsupported)
}

// TestBTreeGraphStorage_TenantBlindReadsReturnNotFound documents that the
// tenant-blind read methods (admin / cross-tenant aggregate hatch) return
// ErrNodeNotFound / ErrEdgeNotFound rather than silently succeeding with
// an empty result — see the comment block above their definitions.
func TestBTreeGraphStorage_TenantBlindReadsReturnNotFound(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	_, err := gs.GetNode(1)
	assert.ErrorIs(t, err, ErrNodeNotFound)

	_, err = gs.GetEdge(1)
	assert.ErrorIs(t, err, ErrEdgeNotFound)

	// FindNodesByLabel / GetOutgoingEdges / GetIncomingEdges are declared
	// returning ([]*X, error) — they yield (nil, nil) ("nothing matched")
	// which is contractually distinct from "not implemented." Pin that.
	out, err := gs.FindNodesByLabel("AnyLabel")
	assert.NoError(t, err)
	assert.Nil(t, out)
}

// TestBTreeGraphStorage_BeginBatchPanics pins the BeginBatch contract: it
// panics rather than returning a nil *Batch (which would nil-deref on the
// first op). Callers that need batches must select *GraphStorage.
func TestBTreeGraphStorage_BeginBatchPanics(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	defer func() {
		r := recover()
		assert.NotNil(t, r, "BeginBatch must panic in C2 partial backend")
	}()
	_ = gs.BeginBatch()
}

// TestBTreeGraphStorage_DeleteNodeForTenantReal pins the one real write
// path beyond Create — node + label-index removal, with the documented
// gap that outgoing/incoming edges are NOT cascaded (see TODO(C2.1)).
func TestBTreeGraphStorage_DeleteNodeForTenantReal(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	tenantID := "tenantD"
	n, err := gs.CreateNodeWithTenant(tenantID, []string{"Doomed"}, nil)
	assert.NoError(t, err)

	got, err := gs.GetNodeForTenant(n.ID, tenantID)
	assert.NoError(t, err)
	assert.Equal(t, n.ID, got.ID)

	assert.NoError(t, gs.DeleteNodeForTenant(n.ID, tenantID))

	_, err = gs.GetNodeForTenant(n.ID, tenantID)
	assert.True(t, errors.Is(err, ErrNodeNotFound))

	// Label index must be cleaned too.
	stillByLabel := gs.GetNodesByLabelForTenant(tenantID, "Doomed")
	assert.Empty(t, stillByLabel)

	// Deleting again is ErrNodeNotFound (idempotent-error semantics).
	err = gs.DeleteNodeForTenant(n.ID, tenantID)
	assert.ErrorIs(t, err, ErrNodeNotFound)
}

// TestBTreeGraphStorage_PersistAcrossReopen pins ID-counter persistence
// via Close + reopen, plus that node records survive process restart.
func TestBTreeGraphStorage_PersistAcrossReopen(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "btree-persist-*")
	assert.NoError(t, err)
	defer os.RemoveAll(dataDir)

	tenantID := "tenantP"
	var firstID uint64

	{
		gs, err := NewBTreeGraphStorage(dataDir)
		assert.NoError(t, err)
		n, err := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
		assert.NoError(t, err)
		firstID = n.ID
		assert.NoError(t, gs.Close())
	}

	{
		gs, err := NewBTreeGraphStorage(dataDir)
		assert.NoError(t, err)
		defer gs.Close()

		// The previously created node survives.
		got, err := gs.GetNodeForTenant(firstID, tenantID)
		assert.NoError(t, err)
		assert.Equal(t, firstID, got.ID)

		// New IDs must not collide with persisted ones.
		n2, err := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
		assert.NoError(t, err)
		assert.Greater(t, n2.ID, firstID)
	}
}

// TestBTreeGraphStorage_SnapshotFlushes verifies Snapshot persists counters
// and flushes the tree without erroring; explicit close still required.
func TestBTreeGraphStorage_SnapshotFlushes(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	_, err := gs.CreateNodeWithTenant("tenantX", []string{"X"}, nil)
	assert.NoError(t, err)
	assert.NoError(t, gs.Snapshot())
}

// Compile-time guard: the test file exercises the Storage interface via
// the BTreeGraphStorage receiver. If the method set drifts, this would
// fail to compile here in addition to the assertion in btree_storage.go.
var _ Storage = (*BTreeGraphStorage)(nil)
