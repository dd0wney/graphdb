package vector

import (
	"math"
	"math/rand"
	"testing"
)

func TestEmeans_EuclideanClusters(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const (
		nPerCluster = 40
		k           = 3
		d           = 8
	)
	centers := [][]float32{
		{10, 0, 0, 0, 0, 0, 0, 0},
		{0, 10, 0, 0, 0, 0, 0, 0},
		{0, 0, 10, 0, 0, 0, 0, 0},
	}

	var data [][]float32
	for cIdx := 0; cIdx < k; cIdx++ {
		for i := 0; i < nPerCluster; i++ {
			row := make([]float32, d)
			for j := 0; j < d; j++ {
				row[j] = centers[cIdx][j] + float32(rng.NormFloat64()*0.15)
			}
			data = append(data, row)
		}
	}

	em, err := NewEmeans(EmeansConfig{
		D:       d,
		K:       k,
		NEpochs: 3.0,
		Seed:    7,
	})
	if err != nil {
		t.Fatalf("NewEmeans: %v", err)
	}
	centroids, labels, err := em.Train(data)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if len(centroids) != k {
		t.Fatalf("len(centroids) = %d, want %d", len(centroids), k)
	}
	if len(labels) != len(data) {
		t.Fatalf("len(labels) = %d, want %d", len(labels), len(data))
	}

	// Each block of nPerCluster points must share a single distinct label.
	seenClusters := make(map[int]bool)
	for cIdx := 0; cIdx < k; cIdx++ {
		lbl := labels[cIdx*nPerCluster]
		seenClusters[lbl] = true
		for i := 1; i < nPerCluster; i++ {
			if got := labels[cIdx*nPerCluster+i]; got != lbl {
				t.Fatalf("cluster %d point %d assigned to %d, want %d", cIdx, i, got, lbl)
			}
		}
	}
	if len(seenClusters) != k {
		t.Fatalf("expected %d distinct cluster labels, got %d", k, len(seenClusters))
	}
}

func TestEmeans_SphericalCosineClusters(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	const (
		nPerCluster = 32
		k           = 4
		d           = 24
	)
	var data [][]float32
	for cIdx := 0; cIdx < k; cIdx++ {
		for i := 0; i < nPerCluster; i++ {
			row := make([]float32, d)
			row[cIdx*4] = 1.0
			for j := 0; j < d; j++ {
				row[j] += float32(rng.NormFloat64() * 0.05)
			}
			data = append(data, Normalize(row))
		}
	}

	em, err := NewEmeans(EmeansConfig{
		D:         d,
		K:         k,
		NEpochs:   2.5,
		Spherical: true,
		Seed:      123,
	})
	if err != nil {
		t.Fatalf("NewEmeans: %v", err)
	}
	centroids, labels, err := em.Train(data)
	if err != nil {
		t.Fatalf("Train: %v", err)
	}
	if len(em.IterationStats) != 3 { // 2.5 epochs = 2 full + 1 half pass
		t.Fatalf("expected 3 epoch stats for NEpochs=2.5, got %d", len(em.IterationStats))
	}

	// Verify all centroids lie on the unit sphere.
	for j, c := range centroids {
		mag := Magnitude(c)
		if math.Abs(float64(mag)-1.0) > 1e-4 {
			t.Errorf("centroid[%d] magnitude = %f, want 1.0", j, mag)
		}
	}

	sims, assigned, err := em.Assign(data)
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	for i := range labels {
		if assigned[i] != labels[i] {
			t.Fatalf("assigned[%d]=%d != labels[%d]=%d", i, assigned[i], i, labels[i])
		}
		if sims[i] < 0.90 {
			t.Errorf("point %d cosine similarity to assigned centroid = %f, want >= 0.90", i, sims[i])
		}
	}
}
