package vector

import (
	"math/rand"
	"sort"
	"testing"
)

// makeClusteredEmbeddings builds nClusters Gaussian clusters of perCluster
// points each, with tight jitter — the clustered structure real embeddings
// have and the int8 spike's random vectors lacked. Deterministic via rng.
func makeClusteredEmbeddings(rng *rand.Rand, nClusters, perCluster, dim int) [][]float32 {
	out := make([][]float32, 0, nClusters*perCluster)
	for c := 0; c < nClusters; c++ {
		center := make([]float32, dim)
		for d := range center {
			center[d] = float32(rng.NormFloat64())
		}
		for p := 0; p < perCluster; p++ {
			v := make([]float32, dim)
			for d := range v {
				v[d] = center[d] + float32(rng.NormFloat64())*0.1
			}
			out = append(out, v)
		}
	}
	return out
}

// exactTopK returns the indices of the k nearest vectors to query by exact
// float32 brute force — the recall ground truth.
func exactTopK(vectors [][]float32, query []float32, k int, metric DistanceMetric) []uint64 {
	type pair struct {
		id   uint64
		dist float32
	}
	pairs := make([]pair, len(vectors))
	for i, v := range vectors {
		d, _ := Distance(query, v, metric)
		pairs[i] = pair{uint64(i), d}
	}
	sort.Slice(pairs, func(a, b int) bool { return pairs[a].dist < pairs[b].dist })
	ids := make([]uint64, k)
	for i := 0; i < k; i++ {
		ids[i] = pairs[i].id
	}
	return ids
}

func recallAtK(got, want []uint64) float64 {
	wantSet := make(map[uint64]bool, len(want))
	for _, id := range want {
		wantSet[id] = true
	}
	hits := 0
	for _, id := range got {
		if wantSet[id] {
			hits++
		}
	}
	return float64(hits) / float64(len(want))
}

func TestInt8HNSWRecallGate(t *testing.T) {
	const (
		dim        = 128
		nClusters  = 20
		perCluster = 50
		k          = 10
		ef         = 64
		nQueries   = 50
		// Gate: mean recall@10 on clustered embeddings vs exact ground truth.
		minMeanRecall = 0.98
	)
	rng := rand.New(rand.NewSource(42))
	vectors := makeClusteredEmbeddings(rng, nClusters, perCluster, dim)

	idx, err := NewHNSWIndex(dim, 16, 200, MetricCosine)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range vectors {
		if err := idx.Insert(uint64(i), v); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	var sumRecall float64
	for qi := 0; qi < nQueries; qi++ {
		query := vectors[rng.Intn(len(vectors))]
		results, err := idx.Search(query, k, ef)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		got := make([]uint64, len(results))
		for i, r := range results {
			got[i] = r.ID
		}
		want := exactTopK(vectors, query, k, MetricCosine)
		sumRecall += recallAtK(got, want)
	}

	meanRecall := sumRecall / float64(nQueries)
	t.Logf("int8-HNSW mean recall@%d on clustered embeddings: %.4f", k, meanRecall)
	if meanRecall < minMeanRecall {
		t.Errorf("mean recall@%d = %.4f, below gate %.2f — int8 quantization "+
			"degrades recall too much; consider int8 candidate-gen + float32 "+
			"re-rank (spec §6 Phase 2)", k, meanRecall, minMeanRecall)
	}
}
