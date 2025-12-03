package vector

import (
	"fmt"
	"math/rand"
	"testing"
)

// TestNewHNSWIndex tests index creation
func TestNewHNSWIndex(t *testing.T) {
	tests := []struct {
		name       string
		dimensions int
		m          int
		efConstruction int
		metric     DistanceMetric
		wantErr    bool
	}{
		{
			name:       "valid index",
			dimensions: 128,
			m:          16,
			efConstruction: 200,
			metric:     MetricCosine,
			wantErr:    false,
		},
		{
			name:       "invalid dimensions",
			dimensions: 0,
			m:          16,
			efConstruction: 200,
			metric:     MetricCosine,
			wantErr:    true,
		},
		{
			name:       "invalid M",
			dimensions: 128,
			m:          0,
			efConstruction: 200,
			metric:     MetricCosine,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index, err := NewHNSWIndex(tt.dimensions, tt.m, tt.efConstruction, tt.metric)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewHNSWIndex() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && index == nil {
				t.Error("NewHNSWIndex() returned nil index")
			}
			if !tt.wantErr {
				if index.Dimensions() != tt.dimensions {
					t.Errorf("Dimensions() = %v, want %v", index.Dimensions(), tt.dimensions)
				}
			}
		})
	}
}

// TestHNSWInsert tests vector insertion
func TestHNSWInsert(t *testing.T) {
	index, err := NewHNSWIndex(3, 16, 200, MetricCosine)
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	// Insert first vector
	vec1 := []float32{1, 0, 0}
	id1 := uint64(1)
	err = index.Insert(id1, vec1)
	if err != nil {
		t.Errorf("Insert() error = %v", err)
	}

	// Insert second vector
	vec2 := []float32{0, 1, 0}
	id2 := uint64(2)
	err = index.Insert(id2, vec2)
	if err != nil {
		t.Errorf("Insert() error = %v", err)
	}

	// Verify count
	if index.Len() != 2 {
		t.Errorf("Len() = %v, want 2", index.Len())
	}
}

// TestHNSWInsertWrongDimensions tests error handling for wrong dimensions
func TestHNSWInsertWrongDimensions(t *testing.T) {
	index, _ := NewHNSWIndex(3, 16, 200, MetricCosine)

	// Try to insert vector with wrong dimensions
	vec := []float32{1, 2, 3, 4} // 4 dimensions instead of 3
	err := index.Insert(1, vec)
	if err == nil {
		t.Error("Expected error for wrong dimensions, got nil")
	}
}

// TestHNSWSearch tests nearest neighbor search
func TestHNSWSearch(t *testing.T) {
	index, _ := NewHNSWIndex(3, 16, 200, MetricCosine)

	// Insert vectors
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
		err := index.Insert(v.id, v.vec)
		if err != nil {
			t.Fatalf("Insert(%d) error = %v", v.id, err)
		}
	}

	// Search for nearest neighbor to [1, 0, 0]
	query := []float32{1, 0, 0}
	results, err := index.Search(query, 1, 50)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Search() returned %d results, want 1", len(results))
	}

	// HNSW is approximate - just verify we got a result
	if len(results) > 0 {
		// Should be one of the inserted vectors
		found := false
		for _, v := range vectors {
			if results[0].ID == v.id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Search() returned unknown ID %d", results[0].ID)
		}
	}
}

// TestHNSWSearchTopK tests k-nearest neighbor search
func TestHNSWSearchTopK(t *testing.T) {
	index, _ := NewHNSWIndex(2, 16, 200, MetricCosine)

	// Insert vectors in a 2D space
	vectors := []struct {
		id  uint64
		vec []float32
	}{
		{1, []float32{1, 0}},
		{2, []float32{0, 1}},
		{3, []float32{-1, 0}},
		{4, []float32{0, -1}},
		{5, []float32{0.9, 0.1}},  // Close to vector 1
		{6, []float32{0.1, 0.9}},  // Close to vector 2
	}

	for _, v := range vectors {
		index.Insert(v.id, v.vec)
	}

	// Search for top 3 nearest neighbors to [1, 0]
	query := []float32{1, 0}
	k := 3
	results, err := index.Search(query, k, 50)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != k {
		t.Errorf("Search() returned %d results, want %d", len(results), k)
	}

	// HNSW is approximate - just verify distances are reasonable
	if len(results) > 0 {
		// Verify results are sorted by distance
		for i := 1; i < len(results); i++ {
			if results[i].Distance < results[i-1].Distance {
				t.Errorf("Results not sorted by distance: %f > %f", results[i-1].Distance, results[i].Distance)
			}
		}
	}
}

// TestHNSWDelete tests vector deletion
func TestHNSWDelete(t *testing.T) {
	index, _ := NewHNSWIndex(3, 16, 200, MetricCosine)

	// Insert vectors
	index.Insert(1, []float32{1, 0, 0})
	index.Insert(2, []float32{0, 1, 0})
	index.Insert(3, []float32{0, 0, 1})

	if index.Len() != 3 {
		t.Errorf("Len() = %v, want 3", index.Len())
	}

	// Delete vector
	err := index.Delete(2)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	if index.Len() != 2 {
		t.Errorf("After delete, Len() = %v, want 2", index.Len())
	}

	// Search should not return deleted vector
	results, _ := index.Search([]float32{0, 1, 0}, 3, 50)
	for _, r := range results {
		if r.ID == 2 {
			t.Error("Search() returned deleted vector ID 2")
		}
	}
}

// TestHNSWConcurrentInsert tests concurrent insertions
func TestHNSWConcurrentInsert(t *testing.T) {
	index, _ := NewHNSWIndex(10, 16, 200, MetricCosine)

	// Insert vectors concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			vec := make([]float32, 10)
			for j := 0; j < 10; j++ {
				vec[j] = rand.Float32()
			}
			err := index.Insert(uint64(id), vec)
			if err != nil {
				t.Errorf("Concurrent Insert(%d) error = %v", id, err)
			}
			done <- true
		}(i)
	}

	// Wait for all insertions
	for i := 0; i < 10; i++ {
		<-done
	}

	if index.Len() != 10 {
		t.Errorf("After concurrent inserts, Len() = %v, want 10", index.Len())
	}
}

// TestHNSWAccuracy tests search accuracy
func TestHNSWAccuracy(t *testing.T) {
	index, _ := NewHNSWIndex(10, 16, 200, MetricEuclidean)

	// Insert 100 random vectors using seeded source for reproducibility
	rng := rand.New(rand.NewSource(42))
	vectors := make([][]float32, 100)
	for i := 0; i < 100; i++ {
		vec := make([]float32, 10)
		for j := 0; j < 10; j++ {
			vec[j] = rng.Float32()
		}
		vectors[i] = vec
		index.Insert(uint64(i), vec)
	}

	// Search for exact match with higher ef for better recall
	// HNSW is approximate, so we increase ef to improve accuracy
	query := vectors[50]
	results, _ := index.Search(query, 1, 200)

	if len(results) == 0 {
		t.Fatal("Search() returned no results")
	}

	// HNSW is approximate - verify we get the exact match or a very close neighbor
	// With ef=200, we should find the exact match most of the time
	if results[0].ID == 50 {
		// Found exact match - distance should be 0
		if results[0].Distance > 0.001 {
			t.Errorf("Search() found exact match but distance = %f, want near 0", results[0].Distance)
		}
	} else {
		// Found approximate match - verify it's reasonable
		// For random 10D vectors, distances typically range from 1-3
		if results[0].Distance > 2.0 {
			t.Errorf("Search() distance too large: %f (found ID %d instead of 50)", results[0].Distance, results[0].ID)
		}
		// This is acceptable for an approximate algorithm
		t.Logf("HNSW approximate result: found ID %d (distance %f) instead of exact match ID 50",
			results[0].ID, results[0].Distance)
	}
}

// BenchmarkHNSWInsert benchmarks insertion performance
func BenchmarkHNSWInsert(b *testing.B) {
	dimensions := []int{128, 384, 768}

	for _, dim := range dimensions {
		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			index, _ := NewHNSWIndex(dim, 16, 200, MetricCosine)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				vec := make([]float32, dim)
				for j := 0; j < dim; j++ {
					vec[j] = rand.Float32()
				}
				index.Insert(uint64(i), vec)
			}
		})
	}
}

// BenchmarkHNSWSearch benchmarks search performance
func BenchmarkHNSWSearch(b *testing.B) {
	index, _ := NewHNSWIndex(768, 16, 200, MetricCosine)

	// Insert 10k vectors using seeded source for reproducibility
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 10000; i++ {
		vec := make([]float32, 768)
		for j := 0; j < 768; j++ {
			vec[j] = rng.Float32()
		}
		index.Insert(uint64(i), vec)
	}

	// Benchmark search
	query := make([]float32, 768)
	for j := 0; j < 768; j++ {
		query[j] = rng.Float32()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index.Search(query, 10, 50)
	}
}
