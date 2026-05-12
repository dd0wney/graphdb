package storage

import (
	"os"
	"testing"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
	"github.com/stretchr/testify/assert"
)

func setupBTreeTestGraph(t *testing.T) (*BTreeGraphStorage, func()) {
	dataDir, err := os.MkdirTemp("", "btree-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	gs, err := NewBTreeGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("failed to create btree storage: %v", err)
	}

	return gs, func() {
		gs.Close()
		os.RemoveAll(dataDir)
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

	// Create node
	node, err := gs.CreateNodeWithTenant(tenantID, labels, props)
	assert.NoError(t, err)
	assert.NotNil(t, node)
	assert.Equal(t, tenantID, node.TenantID)

	// Get node
	retrieved, err := gs.GetNodeForTenant(node.ID, tenantID)
	assert.NoError(t, err)
	assert.Equal(t, node.ID, retrieved.ID)
	name, _ := retrieved.Properties["name"].AsString()
	assert.Equal(t, "Alice", name)

	// Get by label
	nodes := gs.GetNodesByLabelForTenant(tenantID, "Person")
	assert.Len(t, nodes, 1)
	assert.Equal(t, node.ID, nodes[0].ID)

	// Cross-tenant isolation (stub check)
	_, err = gs.GetNodeForTenant(node.ID, "tenant2")
	assert.Error(t, err)
}

func TestBTreeGraphStorage_EdgeOperations(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	tenantID := "tenant1"
	n1, _ := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
	n2, _ := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)

	// Create edge
	edge, err := gs.CreateEdgeWithTenant(tenantID, n1.ID, n2.ID, "FOLLOWS", nil, 1.0)
	assert.NoError(t, err)
	assert.NotNil(t, edge)

	// Get outgoing
	outgoing, err := gs.GetOutgoingEdgesForTenant(n1.ID, tenantID)
	assert.NoError(t, err)
	assert.Len(t, outgoing, 1)
	assert.Equal(t, edge.ID, outgoing[0].ID)

	// Get incoming
	incoming, err := gs.GetIncomingEdgesForTenant(n2.ID, tenantID)
	assert.NoError(t, err)
	assert.Len(t, incoming, 1)
	assert.Equal(t, edge.ID, incoming[0].ID)
}

func TestBTreeGraphStorage_MetadataOperations(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	tenantID := "tenant1"
	gs.CreateNodeWithTenant(tenantID, []string{"Person"}, nil)
	gs.CreateNodeWithTenant(tenantID, []string{"Company"}, nil)
	
	n1 := gs.GetNodesByLabelForTenant(tenantID, "Person")
	n2 := gs.GetNodesByLabelForTenant(tenantID, "Company")
	gs.CreateEdgeWithTenant(tenantID, n1[0].ID, n2[0].ID, "WORKS_AT", nil, 1.0)

	// Get labels
	labels := gs.GetLabelsForTenant(tenantID)
	assert.ElementsMatch(t, []string{"Person", "Company"}, labels)

	// Get edge types
	types := gs.GetEdgeTypesForTenant(tenantID)
	assert.ElementsMatch(t, []string{"WORKS_AT"}, types)
	
	// Statistics
	stats := gs.GetStatistics()
	assert.Equal(t, uint64(2), stats.NodeCount)
	assert.Equal(t, uint64(1), stats.EdgeCount)
}

func TestBTreeGraphStorage_MultiTenancy(t *testing.T) {
	gs, cleanup := setupBTreeTestGraph(t)
	defer cleanup()

	gs.CreateNodeWithTenant("tenantA", []string{"User"}, map[string]Value{"name": StringValue("Alice")})
	gs.CreateNodeWithTenant("tenantB", []string{"User"}, map[string]Value{"name": StringValue("Bob")})

	// tenantA view
	nodesA := gs.GetAllNodesForTenant("tenantA")
	assert.Len(t, nodesA, 1)
	nameA, _ := nodesA[0].Properties["name"].AsString()
	assert.Equal(t, "Alice", nameA)

	// tenantB view
	nodesB := gs.GetAllNodesForTenant("tenantB")
	assert.Len(t, nodesB, 1)
	nameB, _ := nodesB[0].Properties["name"].AsString()
	assert.Equal(t, "Bob", nameB)
	
	// Global view
	allNodes := gs.GetAllNodesAcrossTenants()
	assert.Len(t, allNodes, 2)
}

func TestBTreeGraphStorage_PersistentVectorIndex(t *testing.T) {
	dataDir, _ := os.MkdirTemp("", "btree-vector-persist-*")
	defer os.RemoveAll(dataDir)

	tenantID := "tenantV"
	propertyName := "embedding"
	vec := []float32{0.1, 0.2, 0.3}

	var nodeID uint64

	// First session: create index and node
	{
		gs, _ := NewBTreeGraphStorage(dataDir)
		_ = gs.CreateVectorIndexForTenant(tenantID, propertyName, 3, 16, 200, vector.MetricCosine)
		
		n, err := gs.CreateNodeWithTenant(tenantID, []string{"Doc"}, map[string]Value{
			propertyName: VectorValue(vec),
		})
		assert.NoError(t, err)
		nodeID = n.ID
		
		// Initial search
		res, err := gs.VectorSearchForTenant(tenantID, propertyName, vec, 1, 50)
		assert.NoError(t, err)
		assert.Len(t, res, 1)
		assert.Equal(t, nodeID, res[0].ID)
		
		gs.Close()
	}

	// Second session: re-open and search without re-indexing
	{
		gs, _ := NewBTreeGraphStorage(dataDir)
		defer gs.Close()
		
		// Search immediately after re-opening
		res, err := gs.VectorSearchForTenant(tenantID, propertyName, vec, 1, 50)
		assert.NoError(t, err, "Vector search should succeed after restart")
		assert.Len(t, res, 1)
		assert.Equal(t, nodeID, res[0].ID)
		
		// Verify persistence of the index itself
		assert.True(t, gs.HasVectorIndexForTenant(tenantID, propertyName))
	}
}
