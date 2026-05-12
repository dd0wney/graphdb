package storage

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// TestVectorIndexCreate tests creating vector indexes
func TestVectorIndexCreate(t *testing.T) {
	vi := NewVectorIndex()

	err := vi.CreateIndex("tenant1", "embedding", 128, 16, 200, vector.MetricCosine)
	if err != nil {
		t.Errorf("CreateIndex() error = %v", err)
	}

	if !vi.HasIndex("tenant1", "embedding") {
		t.Error("HasIndex() = false, want true")
	}

	// Duplicate index for same tenant should fail
	err = vi.CreateIndex("tenant1", "embedding", 128, 16, 200, vector.MetricCosine)
	if err == nil {
		t.Error("CreateIndex() should fail for duplicate property in same tenant")
	}

	// Same property name for different tenant should succeed
	err = vi.CreateIndex("tenant2", "embedding", 128, 16, 200, vector.MetricCosine)
	if err != nil {
		t.Errorf("CreateIndex() error = %v for different tenant", err)
	}
}

// TestVectorIndexAddSearch tests adding and searching vectors with isolation
func TestVectorIndexAddSearch(t *testing.T) {
	vi := NewVectorIndex()

	_ = vi.CreateIndex("tenant1", "v1", 3, 16, 200, vector.MetricCosine)
	_ = vi.CreateIndex("tenant2", "v1", 3, 16, 200, vector.MetricCosine)

	// Add to tenant1
	err := vi.AddVector("tenant1", "v1", 1, []float32{1.0, 0.0, 0.0})
	if err != nil {
		t.Errorf("AddVector() error = %v", err)
	}

	// Add different vector to tenant2 with same node ID (legal since they are different tenants)
	err = vi.AddVector("tenant2", "v1", 1, []float32{0.0, 1.0, 0.0})
	if err != nil {
		t.Errorf("AddVector() error = %v", err)
	}

	// Search tenant1
	results, err := vi.Search("tenant1", "v1", []float32{1.0, 0.0, 0.0}, 1, 50)
	if err != nil {
		t.Errorf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != 1 {
		t.Errorf("Search() returned %d results, want 1 with ID 1", len(results))
	}
	if results[0].Distance > 0.001 {
		t.Errorf("Search() distance = %f, want ~0", results[0].Distance)
	}

	// Search tenant2 - should find tenant2's vector
	results2, _ := vi.Search("tenant2", "v1", []float32{0.0, 1.0, 0.0}, 1, 50)
	if len(results2) != 1 || results2[0].ID != 1 {
		t.Error("Search tenant2 should find its own node 1")
	}

	// Search tenant1 for tenant2's vector - should have high distance
	results3, _ := vi.Search("tenant1", "v1", []float32{0.0, 1.0, 0.0}, 1, 50)
	if results3[0].Distance < 0.9 {
		t.Error("Search tenant1 should NOT find tenant2's matching vector")
	}
}

// TestVectorIndexRemove tests removing vectors
func TestVectorIndexRemove(t *testing.T) {
	vi := NewVectorIndex()
	tenantID := "t1"

	_ = vi.CreateIndex(tenantID, "v1", 3, 16, 200, vector.MetricCosine)
	_ = vi.AddVector(tenantID, "v1", 1, []float32{1.0, 0.0, 0.0})

	err := vi.RemoveVector(tenantID, "v1", 1)
	if err != nil {
		t.Errorf("RemoveVector() error = %v", err)
	}

	results, _ := vi.Search(tenantID, "v1", []float32{1.0, 0.0, 0.0}, 1, 50)
	if len(results) != 0 {
		t.Error("Search() returned results after removal")
	}
}

// TestVectorIndexDrop tests dropping indexes
func TestVectorIndexDrop(t *testing.T) {
	vi := NewVectorIndex()
	_ = vi.CreateIndex("t1", "v1", 3, 16, 200, vector.MetricCosine)

	err := vi.DropIndex("t1", "v1")
	if err != nil {
		t.Errorf("DropIndex() error = %v", err)
	}

	if vi.HasIndex("t1", "v1") {
		t.Error("HasIndex() = true after drop")
	}
}

// TestVectorIndexList tests listing indexes
func TestVectorIndexList(t *testing.T) {
	vi := NewVectorIndex()
	_ = vi.CreateIndex("t1", "v1", 3, 16, 200, vector.MetricCosine)
	_ = vi.CreateIndex("t1", "v2", 3, 16, 200, vector.MetricCosine)
	_ = vi.CreateIndex("t2", "v1", 3, 16, 200, vector.MetricCosine)

	indexes := vi.ListIndexes("t1")
	if len(indexes) != 2 {
		t.Errorf("ListIndexes(t1) returned %d indexes, want 2", len(indexes))
	}

	indexes2 := vi.ListIndexes("t2")
	if len(indexes2) != 1 {
		t.Errorf("ListIndexes(t2) returned %d indexes, want 1", len(indexes2))
	}
}
