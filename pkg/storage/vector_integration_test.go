package storage

import (
	"errors"
	"math"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
)

// TestVectorValue tests encoding and decoding of vector values
func TestVectorValue(t *testing.T) {
	tests := []struct {
		name    string
		vector  []float32
		wantErr bool
	}{
		{
			name:    "simple vector",
			vector:  []float32{1.0, 2.0, 3.0},
			wantErr: false,
		},
		{
			name:    "high dimensional vector",
			vector:  []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8},
			wantErr: false,
		},
		{
			name:    "negative values",
			vector:  []float32{-1.5, 2.3, -0.7},
			wantErr: false,
		},
		{
			name:    "zero vector",
			vector:  []float32{0, 0, 0},
			wantErr: false,
		},
		{
			name:    "single element",
			vector:  []float32{42.5},
			wantErr: false,
		},
		{
			name:    "empty vector",
			vector:  []float32{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			val := VectorValue(tt.vector)

			// Verify type
			if val.Type != TypeVector {
				t.Errorf("VectorValue() Type = %v, want %v", val.Type, TypeVector)
			}

			// Decode
			decoded, err := val.AsVector()
			if (err != nil) != tt.wantErr {
				t.Errorf("AsVector() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Verify dimensions
			if len(decoded) != len(tt.vector) {
				t.Errorf("AsVector() length = %v, want %v", len(decoded), len(tt.vector))
				return
			}

			// Verify values
			for i := range decoded {
				if math.Abs(float64(decoded[i]-tt.vector[i])) > 0.0001 {
					t.Errorf("AsVector()[%d] = %v, want %v", i, decoded[i], tt.vector[i])
				}
			}
		})
	}
}

// TestVectorValueTypeError tests type errors when decoding
func TestVectorValueTypeError(t *testing.T) {
	// Create non-vector value
	val := StringValue("not a vector")

	// Try to decode as vector
	_, err := val.AsVector()
	if err == nil {
		t.Error("AsVector() on string value should return error")
	}
}

// TestVectorValueCorruptData tests handling of corrupt data
func TestVectorValueCorruptData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty data",
			data: []byte{},
		},
		{
			name: "insufficient data",
			data: []byte{0, 0, 0},
		},
		{
			name: "mismatched length",
			data: []byte{2, 0, 0, 0, 0, 0, 0, 0}, // Says 2 dimensions but only has space for 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := Value{Type: TypeVector, Data: tt.data}
			_, err := val.AsVector()
			if err == nil {
				t.Error("AsVector() should return error for corrupt data")
			}
		})
	}
}

// TestNodeWithVector tests storing vector properties on nodes
func TestNodeWithVector(t *testing.T) {
	node := &Node{
		ID:         1,
		Labels:     []string{"Document"},
		Properties: make(map[string]Value),
		CreatedAt:  12345,
		UpdatedAt:  12345,
	}

	// Add vector property
	embedding := []float32{0.1, 0.2, 0.3, 0.4}
	node.Properties["embedding"] = VectorValue(embedding)

	// Retrieve and verify
	val, ok := node.GetProperty("embedding")
	if !ok {
		t.Fatal("Failed to get vector property")
	}

	decoded, err := val.AsVector()
	if err != nil {
		t.Fatalf("Failed to decode vector: %v", err)
	}

	if len(decoded) != len(embedding) {
		t.Errorf("Vector length = %d, want %d", len(decoded), len(embedding))
	}

	for i := range decoded {
		if math.Abs(float64(decoded[i]-embedding[i])) > 0.0001 {
			t.Errorf("Vector[%d] = %v, want %v", i, decoded[i], embedding[i])
		}
	}
}

// TestStorageVectorSearchIntegration tests end-to-end vector search integration
func TestStorageVectorSearchIntegration(t *testing.T) {
	// Create storage
	tmpDir := t.TempDir()
	config := StorageConfig{
		DataDir:            tmpDir,
		EnableBatching:     false,
		EnableCompression:  false,
		UseDiskBackedEdges: false,
		BulkImportMode:     true, // Skip WAL for test
	}

	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Create vector index for "embedding" property
	err = gs.CreateVectorIndex("embedding", 3, 16, 200, vector.MetricCosine)
	if err != nil {
		t.Fatalf("Failed to create vector index: %v", err)
	}

	// Add nodes with vector embeddings
	documents := []struct {
		id    uint64
		label string
		text  string
		vec   []float32
	}{
		{1, "Document", "AI and machine learning", []float32{1.0, 0.0, 0.0}},
		{2, "Document", "Database systems", []float32{0.0, 1.0, 0.0}},
		{3, "Document", "Graph theory", []float32{0.0, 0.0, 1.0}},
		{4, "Document", "AI applications", []float32{0.9, 0.1, 0.0}},
	}

	for _, doc := range documents {
		node := &Node{
			ID:     doc.id,
			Labels: []string{doc.label},
			Properties: map[string]Value{
				"text":      StringValue(doc.text),
				"embedding": VectorValue(doc.vec),
			},
			CreatedAt: 12345,
			UpdatedAt: 12345,
		}

		// Add node to storage
		gs.storeNodeInShard(node)

		// Update vector indexes
		err := gs.UpdateNodeVectorIndexes(node)
		if err != nil {
			t.Fatalf("Failed to update vector indexes for node %d: %v", doc.id, err)
		}
	}

	// Search for documents similar to "AI and machine learning"
	query := []float32{1.0, 0.0, 0.0}
	results, err := gs.VectorSearch("embedding", query, 2, 50)
	if err != nil {
		t.Fatalf("VectorSearch() error = %v", err)
	}

	// Verify we got results
	if len(results) != 2 {
		t.Errorf("VectorSearch() returned %d results, want 2", len(results))
	}

	// Verify results contain valid node IDs
	for _, r := range results {
		if r.ID < 1 || r.ID > 4 {
			t.Errorf("VectorSearch() returned invalid ID %d", r.ID)
		}
	}

	// HNSW is approximate, so just verify we got reasonable results
	// The closest vectors should have small distances
	for _, r := range results {
		if r.Distance > 1.5 {
			t.Errorf("VectorSearch() returned result with large distance %f (ID %d)", r.Distance, r.ID)
		}
	}
}

// TestStorageVectorIndexManagement tests vector index lifecycle
func TestStorageVectorIndexManagement(t *testing.T) {
	tmpDir := t.TempDir()
	config := StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Create index
	err = gs.CreateVectorIndex("embedding", 128, 16, 200, vector.MetricCosine)
	if err != nil {
		t.Errorf("CreateVectorIndex() error = %v", err)
	}

	// Verify index exists
	if !gs.HasVectorIndex("embedding") {
		t.Error("HasVectorIndex() = false, want true")
	}

	// List indexes
	indexes := gs.ListVectorIndexes()
	if len(indexes) != 1 {
		t.Errorf("ListVectorIndexes() returned %d indexes, want 1", len(indexes))
	}

	// Drop index
	err = gs.DropVectorIndex("embedding")
	if err != nil {
		t.Errorf("DropVectorIndex() error = %v", err)
	}

	// Verify index no longer exists
	if gs.HasVectorIndex("embedding") {
		t.Error("HasVectorIndex() = true after drop, want false")
	}
}

// TestStorageVectorUpdateDelete tests updating and deleting nodes with vectors
func TestStorageVectorUpdateDelete(t *testing.T) {
	tmpDir := t.TempDir()
	config := StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Create index
	_ = gs.CreateVectorIndex("embedding", 3, 16, 200, vector.MetricCosine)

	// Add node with vector
	node := &Node{
		ID:     1,
		Labels: []string{"Test"},
		Properties: map[string]Value{
			"embedding": VectorValue([]float32{1, 0, 0}),
		},
		CreatedAt: 12345,
		UpdatedAt: 12345,
	}

	gs.storeNodeInShard(node)
	_ = gs.UpdateNodeVectorIndexes(node)

	// Search for the node
	results, _ := gs.VectorSearch("embedding", []float32{1, 0, 0}, 1, 50)
	if len(results) != 1 || results[0].ID != 1 {
		t.Error("Should find node 1 before delete")
	}

	// Delete node from vector indexes. R1.2 signature: empty tenantID
	// preserves this test's tenant-blind intent — RemoveNodeFromVectorIndexes
	// falls back to tenantid.Default, which is where the index lives.
	err = gs.RemoveNodeFromVectorIndexes(1, "")
	if err != nil {
		t.Errorf("RemoveNodeFromVectorIndexes() error = %v", err)
	}

	// Search should not find deleted node
	results, _ = gs.VectorSearch("embedding", []float32{1, 0, 0}, 1, 50)
	for _, r := range results {
		if r.ID == 1 {
			t.Error("VectorSearch() should not return deleted node")
		}
	}
}

// TestStorageVectorCrossTenantIsolation pins the existence-leak channel
// closed: a tenant with no vector index cannot observe another tenant's
// indexes or contents via any *VectorIndexForTenant method. F4 spike §1.2.
func TestStorageVectorCrossTenantIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	config := StorageConfig{DataDir: tmpDir, BulkImportMode: true}
	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Tenant A creates an index and indexes a vector.
	if err := gs.CreateVectorIndexForTenant("tenantA", "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant(tenantA) error = %v", err)
	}
	nodeA := &Node{
		ID:       1,
		Labels:   []string{"Doc"},
		TenantID: "tenantA",
		Properties: map[string]Value{
			"embedding": VectorValue([]float32{1, 0, 0}),
		},
	}
	gs.storeNodeInShard(nodeA)
	if err := gs.UpdateNodeVectorIndexes(nodeA); err != nil {
		t.Fatalf("UpdateNodeVectorIndexes(nodeA) error = %v", err)
	}

	// (1) HasVectorIndexForTenant(B) returns false even though A has it.
	if gs.HasVectorIndexForTenant("tenantB", "embedding") {
		t.Error("HasVectorIndexForTenant(tenantB) = true, want false (cross-tenant existence leak)")
	}

	// (2) VectorSearchForTenant(B) returns ErrNodeNotFound — unified
	// error prevents existence-leak via error-shape inference.
	if _, err := gs.VectorSearchForTenant("tenantB", "embedding", []float32{1, 0, 0}, 1, 50); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("VectorSearchForTenant(tenantB) error = %v, want ErrNodeNotFound", err)
	}

	// (3) GetVectorIndexMetricForTenant(B) returns ErrNodeNotFound.
	if _, err := gs.GetVectorIndexMetricForTenant("tenantB", "embedding"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("GetVectorIndexMetricForTenant(tenantB) error = %v, want ErrNodeNotFound", err)
	}

	// (4) ListVectorIndexesForTenant(B) returns []string{}.
	if indexes := gs.ListVectorIndexesForTenant("tenantB"); len(indexes) != 0 {
		t.Errorf("ListVectorIndexesForTenant(tenantB) = %v, want []", indexes)
	}

	// (5) Tenant A still sees its index — data is not lost.
	if !gs.HasVectorIndexForTenant("tenantA", "embedding") {
		t.Error("HasVectorIndexForTenant(tenantA) = false, want true (data lost)")
	}
	resultsA, err := gs.VectorSearchForTenant("tenantA", "embedding", []float32{1, 0, 0}, 1, 50)
	if err != nil {
		t.Fatalf("VectorSearchForTenant(tenantA) error = %v", err)
	}
	if len(resultsA) != 1 || resultsA[0].ID != 1 {
		t.Errorf("VectorSearchForTenant(tenantA) = %v, want [{ID: 1, ...}]", resultsA)
	}
}

// TestStorageVectorEmptyTenantID pins that empty tenantID is treated as
// rejection / unified-not-found / unified-false per F4 spike §1.3 — the
// no-silent-default-routing rationale. Empty must not silently route to
// tenantid.Default for the public *VectorIndexForTenant surface.
func TestStorageVectorEmptyTenantID(t *testing.T) {
	tmpDir := t.TempDir()
	config := StorageConfig{DataDir: tmpDir, BulkImportMode: true}
	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Pre-seed an index in the default tenant — empty-tenantID probes
	// must NOT see it via the public *ForTenant surface.
	if err := gs.CreateVectorIndex("embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndex(seed) error = %v", err)
	}

	if err := gs.CreateVectorIndexForTenant("", "embedding2", 3, 16, 200, vector.MetricCosine); err == nil {
		t.Error("CreateVectorIndexForTenant(\"\") = nil, want error")
	}
	if gs.HasVectorIndexForTenant("", "embedding") {
		t.Error("HasVectorIndexForTenant(\"\", existing) = true, want false")
	}
	if _, err := gs.VectorSearchForTenant("", "embedding", []float32{1, 0, 0}, 1, 50); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("VectorSearchForTenant(\"\") error = %v, want ErrNodeNotFound", err)
	}
	if _, err := gs.GetVectorIndexMetricForTenant("", "embedding"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("GetVectorIndexMetricForTenant(\"\") error = %v, want ErrNodeNotFound", err)
	}
	if indexes := gs.ListVectorIndexesForTenant(""); len(indexes) != 0 {
		t.Errorf("ListVectorIndexesForTenant(\"\") = %v, want []", indexes)
	}
	if err := gs.DropVectorIndexForTenant("", "embedding"); err == nil {
		t.Error("DropVectorIndexForTenant(\"\") = nil, want error")
	}
}

// TestStorageVectorPerTenantRouting pins that UpdateNodeVectorIndexes
// routes vectors into the per-tenant index keyed by node.TenantID. This
// is the R1.2 behavior change — previously all vectors landed in
// tenantid.Default's namespace and isolation was post-filter.
func TestStorageVectorPerTenantRouting(t *testing.T) {
	tmpDir := t.TempDir()
	config := StorageConfig{DataDir: tmpDir, BulkImportMode: true}
	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Both tenants register the same property name.
	if err := gs.CreateVectorIndexForTenant("tenantA", "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant(tenantA) error = %v", err)
	}
	if err := gs.CreateVectorIndexForTenant("tenantB", "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("CreateVectorIndexForTenant(tenantB) error = %v", err)
	}

	// Each tenant has a distinct node with a different vector.
	nodeA := &Node{ID: 1, TenantID: "tenantA",
		Properties: map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})}}
	nodeB := &Node{ID: 2, TenantID: "tenantB",
		Properties: map[string]Value{"embedding": VectorValue([]float32{0, 1, 0})}}
	gs.storeNodeInShard(nodeA)
	gs.storeNodeInShard(nodeB)
	if err := gs.UpdateNodeVectorIndexes(nodeA); err != nil {
		t.Fatalf("UpdateNodeVectorIndexes(nodeA) error = %v", err)
	}
	if err := gs.UpdateNodeVectorIndexes(nodeB); err != nil {
		t.Fatalf("UpdateNodeVectorIndexes(nodeB) error = %v", err)
	}

	// Tenant A's search finds only A's vector (not B's, even though B's
	// vector is identical to A's query direction would have ranked it).
	resultsA, err := gs.VectorSearchForTenant("tenantA", "embedding", []float32{1, 0, 0}, 5, 50)
	if err != nil {
		t.Fatalf("VectorSearchForTenant(tenantA) error = %v", err)
	}
	if len(resultsA) != 1 || resultsA[0].ID != 1 {
		t.Errorf("VectorSearchForTenant(tenantA) = %v, want exactly [{ID: 1}]", resultsA)
	}

	// Tenant B's search finds only B's vector.
	resultsB, err := gs.VectorSearchForTenant("tenantB", "embedding", []float32{0, 1, 0}, 5, 50)
	if err != nil {
		t.Fatalf("VectorSearchForTenant(tenantB) error = %v", err)
	}
	if len(resultsB) != 1 || resultsB[0].ID != 2 {
		t.Errorf("VectorSearchForTenant(tenantB) = %v, want exactly [{ID: 2}]", resultsB)
	}
}

// TestStorageVectorRemoveRoutesByTenant pins that RemoveNodeFromVectorIndexes
// with an explicit tenantID removes only from that tenant's indexes, not
// any other tenant's. Empty tenantID falls back to tenantid.Default.
func TestStorageVectorRemoveRoutesByTenant(t *testing.T) {
	tmpDir := t.TempDir()
	config := StorageConfig{DataDir: tmpDir, BulkImportMode: true}
	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	_ = gs.CreateVectorIndexForTenant("tenantA", "embedding", 3, 16, 200, vector.MetricCosine)
	_ = gs.CreateVectorIndexForTenant("tenantB", "embedding", 3, 16, 200, vector.MetricCosine)

	// Both tenants index a node with id=1 (allowed because indexes are
	// per-tenant; collision is only inside one tenant's namespace).
	nodeA := &Node{ID: 1, TenantID: "tenantA",
		Properties: map[string]Value{"embedding": VectorValue([]float32{1, 0, 0})}}
	nodeB := &Node{ID: 1, TenantID: "tenantB",
		Properties: map[string]Value{"embedding": VectorValue([]float32{0, 1, 0})}}
	_ = gs.UpdateNodeVectorIndexes(nodeA)
	_ = gs.UpdateNodeVectorIndexes(nodeB)

	// Remove from A only.
	if err := gs.RemoveNodeFromVectorIndexes(1, "tenantA"); err != nil {
		t.Fatalf("RemoveNodeFromVectorIndexes(1, tenantA) error = %v", err)
	}

	// A's search no longer finds it.
	resultsA, _ := gs.VectorSearchForTenant("tenantA", "embedding", []float32{1, 0, 0}, 5, 50)
	for _, r := range resultsA {
		if r.ID == 1 {
			t.Error("VectorSearchForTenant(tenantA) returned removed node")
		}
	}

	// B's search still finds its node — the remove did not cross tenants.
	resultsB, _ := gs.VectorSearchForTenant("tenantB", "embedding", []float32{0, 1, 0}, 5, 50)
	foundInB := false
	for _, r := range resultsB {
		if r.ID == 1 {
			foundInB = true
			break
		}
	}
	if !foundInB {
		t.Error("VectorSearchForTenant(tenantB) failed to find tenantB's node — remove leaked across tenants")
	}
}

// TestStorageVectorSearchFromFloatArrayProperty verifies that a numeric-array
// (TypeFloatArray) property — the shape a REST/GraphQL client produces from a
// JSON number array, which cannot express TypeVector — is indexed as a vector
// when a vector index exists for that property. This is what lets pure-REST
// consumers populate HNSW vector indexes (previously these were silently ignored).
func TestStorageVectorSearchFromFloatArrayProperty(t *testing.T) {
	tmpDir := t.TempDir()
	config := StorageConfig{
		DataDir:            tmpDir,
		EnableBatching:     false,
		EnableCompression:  false,
		UseDiskBackedEdges: false,
		BulkImportMode:     true,
	}
	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreateVectorIndex("embedding", 3, 16, 200, vector.MetricCosine); err != nil {
		t.Fatalf("Failed to create vector index: %v", err)
	}

	// Store nodes whose "embedding" is a FLOAT ARRAY (not VectorValue) — exactly
	// what the REST node-create path produces from a JSON array.
	docs := []struct {
		id  uint64
		vec []float64
	}{
		{1, []float64{1.0, 0.0, 0.0}},
		{2, []float64{0.0, 1.0, 0.0}},
		{3, []float64{0.9, 0.1, 0.0}},
	}
	for _, d := range docs {
		node := &Node{
			ID:         d.id,
			Labels:     []string{"Document"},
			Properties: map[string]Value{"embedding": FloatArrayValue(d.vec)},
			CreatedAt:  12345,
			UpdatedAt:  12345,
		}
		gs.storeNodeInShard(node)
		if err := gs.UpdateNodeVectorIndexes(node); err != nil {
			t.Fatalf("UpdateNodeVectorIndexes(%d) error = %v", d.id, err)
		}
	}

	// A query closest to doc 1 should return results — proving the float-array
	// vectors were actually indexed, not silently skipped.
	results, err := gs.VectorSearch("embedding", []float32{1.0, 0.0, 0.0}, 2, 50)
	if err != nil {
		t.Fatalf("VectorSearch() error = %v", err)
	}
	// The key assertion: results are non-empty (previously 0 — the float-array
	// vectors were silently never indexed). HNSW is approximate at tiny scale, so
	// we don't assert exact recall — only that the vectors were indexed (results
	// returned), IDs are valid, and at least one is a genuine near-neighbor
	// (small distance), which proves the float→vector conversion is sound.
	if len(results) == 0 {
		t.Fatalf("VectorSearch returned 0 results — float-array property was not indexed")
	}
	minDist := float32(math.MaxFloat32)
	for _, r := range results {
		if r.ID < 1 || r.ID > 3 {
			t.Errorf("VectorSearch returned invalid ID %d", r.ID)
		}
		if r.Distance < minDist {
			minDist = r.Distance
		}
	}
	if minDist > 0.5 {
		t.Errorf("nearest distance %v too large — float-array vectors not meaningfully indexed", minDist)
	}
}
