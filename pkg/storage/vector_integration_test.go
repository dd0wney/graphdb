package storage

import (
	"math"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
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
	defer gs.Close()

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
			ID:         doc.id,
			Labels:     []string{doc.label},
			Properties: map[string]Value{
				"text":      StringValue(doc.text),
				"embedding": VectorValue(doc.vec),
			},
			CreatedAt: 12345,
			UpdatedAt: 12345,
		}

		// Add node to storage
		gs.nodes[node.ID] = node

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
	defer gs.Close()

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
	defer gs.Close()

	// Create index
	gs.CreateVectorIndex("embedding", 3, 16, 200, vector.MetricCosine)

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

	gs.nodes[node.ID] = node
	gs.UpdateNodeVectorIndexes(node)

	// Search for the node
	results, _ := gs.VectorSearch("embedding", []float32{1, 0, 0}, 1, 50)
	if len(results) != 1 || results[0].ID != 1 {
		t.Error("Should find node 1 before delete")
	}

	// Delete node from vector indexes
	err = gs.RemoveNodeFromVectorIndexes(1)
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
