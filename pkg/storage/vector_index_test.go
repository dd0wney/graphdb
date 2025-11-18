package storage

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// TestVectorIndexCreate tests creating vector indexes
func TestVectorIndexCreate(t *testing.T) {
	vi := NewVectorIndex()

	// Create index
	err := vi.CreateIndex("embedding", 128, 16, 200, vector.MetricCosine)
	if err != nil {
		t.Fatalf("CreateIndex() error = %v", err)
	}

	// Verify index exists
	if !vi.HasIndex("embedding") {
		t.Error("HasIndex() = false, want true")
	}

	// Try to create duplicate index
	err = vi.CreateIndex("embedding", 128, 16, 200, vector.MetricCosine)
	if err == nil {
		t.Error("CreateIndex() for duplicate should return error")
	}
}

// TestVectorIndexAddSearch tests adding and searching vectors
func TestVectorIndexAddSearch(t *testing.T) {
	vi := NewVectorIndex()

	// Create index
	err := vi.CreateIndex("embedding", 3, 16, 200, vector.MetricCosine)
	if err != nil {
		t.Fatalf("CreateIndex() error = %v", err)
	}

	// Add vectors
	vectors := []struct {
		id  uint64
		vec []float32
	}{
		{1, []float32{1, 0, 0}},
		{2, []float32{0, 1, 0}},
		{3, []float32{0, 0, 1}},
		{4, []float32{1, 1, 0}},
	}

	for _, v := range vectors {
		err := vi.AddVector("embedding", v.id, v.vec)
		if err != nil {
			t.Errorf("AddVector(%d) error = %v", v.id, err)
		}
	}

	// Search for nearest neighbor
	query := []float32{1, 0, 0}
	results, err := vi.Search("embedding", query, 2, 50)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Search() returned %d results, want 2", len(results))
	}

	// Verify results contain valid node IDs
	validIDs := map[uint64]bool{1: true, 2: true, 3: true, 4: true}
	for _, r := range results {
		if !validIDs[r.ID] {
			t.Errorf("Search() returned invalid ID %d", r.ID)
		}
	}
}

// TestVectorIndexRemove tests removing vectors
func TestVectorIndexRemove(t *testing.T) {
	vi := NewVectorIndex()

	// Create index and add vectors
	vi.CreateIndex("embedding", 3, 16, 200, vector.MetricCosine)
	vi.AddVector("embedding", 1, []float32{1, 0, 0})
	vi.AddVector("embedding", 2, []float32{0, 1, 0})

	// Remove vector
	err := vi.RemoveVector("embedding", 1)
	if err != nil {
		t.Errorf("RemoveVector() error = %v", err)
	}

	// Search should not return removed vector
	query := []float32{1, 0, 0}
	results, _ := vi.Search("embedding", query, 2, 50)
	for _, r := range results {
		if r.ID == 1 {
			t.Error("Search() returned removed vector ID 1")
		}
	}
}

// TestVectorIndexDrop tests dropping indexes
func TestVectorIndexDrop(t *testing.T) {
	vi := NewVectorIndex()

	// Create index
	vi.CreateIndex("embedding", 128, 16, 200, vector.MetricCosine)

	// Drop index
	err := vi.DropIndex("embedding")
	if err != nil {
		t.Errorf("DropIndex() error = %v", err)
	}

	// Verify index no longer exists
	if vi.HasIndex("embedding") {
		t.Error("HasIndex() = true after drop, want false")
	}

	// Try to drop non-existent index
	err = vi.DropIndex("nonexistent")
	if err == nil {
		t.Error("DropIndex() for non-existent index should return error")
	}
}

// TestVectorIndexList tests listing indexes
func TestVectorIndexList(t *testing.T) {
	vi := NewVectorIndex()

	// Create multiple indexes
	vi.CreateIndex("embedding1", 128, 16, 200, vector.MetricCosine)
	vi.CreateIndex("embedding2", 256, 16, 200, vector.MetricEuclidean)

	// List indexes
	indexes := vi.ListIndexes()
	if len(indexes) != 2 {
		t.Errorf("ListIndexes() returned %d indexes, want 2", len(indexes))
	}

	// Verify both indexes are in the list
	found := make(map[string]bool)
	for _, name := range indexes {
		found[name] = true
	}

	if !found["embedding1"] || !found["embedding2"] {
		t.Error("ListIndexes() missing expected index names")
	}
}

// TestVectorIndexErrors tests error handling
func TestVectorIndexErrors(t *testing.T) {
	vi := NewVectorIndex()

	// Try to add vector without index
	err := vi.AddVector("nonexistent", 1, []float32{1, 2, 3})
	if err == nil {
		t.Error("AddVector() without index should return error")
	}

	// Try to search without index
	_, err = vi.Search("nonexistent", []float32{1, 2, 3}, 5, 50)
	if err == nil {
		t.Error("Search() without index should return error")
	}

	// Try to remove vector without index
	err = vi.RemoveVector("nonexistent", 1)
	if err == nil {
		t.Error("RemoveVector() without index should return error")
	}
}

// TestVectorIndexConcurrent tests concurrent operations
func TestVectorIndexConcurrent(t *testing.T) {
	vi := NewVectorIndex()

	// Create index
	vi.CreateIndex("embedding", 10, 16, 200, vector.MetricCosine)

	// Add vectors concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			vec := make([]float32, 10)
			for j := 0; j < 10; j++ {
				vec[j] = float32(id*10 + j)
			}
			err := vi.AddVector("embedding", uint64(id), vec)
			if err != nil {
				t.Errorf("Concurrent AddVector(%d) error = %v", id, err)
			}
			done <- true
		}(i)
	}

	// Wait for all insertions
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify we can search
	query := make([]float32, 10)
	for i := 0; i < 10; i++ {
		query[i] = 0.5
	}

	results, err := vi.Search("embedding", query, 5, 50)
	if err != nil {
		t.Errorf("Search() after concurrent inserts error = %v", err)
	}

	if len(results) == 0 {
		t.Error("Search() returned no results after concurrent inserts")
	}
}
