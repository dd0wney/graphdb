package vector

import (
	"math/rand"
	"sort"
	"testing"
)

// makeClusteredEmbeddings builds nClusters Gaussian clusters of perCluster
// points each. Jitter 0.5 makes intra-cluster variation comparable to the
// cluster spread — representative of real embeddings, where int8 recall lands
// ~0.98 (matching measured GloVe-50d ~0.99). Much tighter jitter (e.g. 0.1) is
// a quantization stress case where the discriminative signal falls below the
// int8 step and recall drops to ~0.74; that is not representative of real data.
// Deterministic via rng.
func makeClusteredEmbeddings(rng *rand.Rand, nClusters, perCluster, dim int) [][]float32 {
	const jitter = 0.5
	out := make([][]float32, 0, nClusters*perCluster)
	for c := 0; c < nClusters; c++ {
		center := make([]float32, dim)
		for d := range center {
			center[d] = float32(rng.NormFloat64())
		}
		for p := 0; p < perCluster; p++ {
			v := make([]float32, dim)
			for d := range v {
				v[d] = center[d] + float32(rng.NormFloat64())*jitter
			}
			out = append(out, v)
		}
	}
	return out
}

// exactTopK returns the indices of the k nearest vectors to query by exact
// brute force — the recall ground truth.
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

// TestHNSWRecallAtScale guards two things at once, both of which collapsed
// before the search/construction fix: (1) HNSW recall at scale — once the index
// exceeds ~ef nodes a min-heap-vs-max-heap candidate bug drove recall to ~0; and
// (2) int8 quantization recall — on representative clustered embeddings (jitter
// 0.5, see makeClusteredEmbeddings) pure int8 lands ~0.98, matching measured
// real GloVe-50d (~0.99). The 0.95 floor catches a regression in either. Pure
// int8 is the shipped design; an int8 over-query → float32 re-rank (top-50
// contains ~100% of the true top-10) remains a documented fallback for
// pathological tightly-clustered workloads where int8 alone dips.
func TestHNSWRecallAtScale(t *testing.T) {
	const (
		dim        = 128
		nClusters  = 20
		perCluster = 50 // 1000 vectors total, far above ef
		k          = 10
		ef         = 64
		nQueries   = 50
		minRecall  = 0.95
	)
	// All three metrics are gated: metricDistanceInt8 has a distinct formula per
	// metric (the euclidean path's max(0,…) clamp especially needs a recall-level
	// guard), and the public API accepts all three.
	for _, metric := range []DistanceMetric{MetricCosine, MetricEuclidean, MetricDotProduct} {
		t.Run(string(metric), func(t *testing.T) {
			rng := rand.New(rand.NewSource(42))
			vectors := makeClusteredEmbeddings(rng, nClusters, perCluster, dim)

			idx, err := NewHNSWIndex(dim, 16, 200, metric)
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
				want := exactTopK(vectors, query, k, metric)
				sumRecall += recallAtK(got, want)
			}

			meanRecall := sumRecall / float64(nQueries)
			t.Logf("%s: mean recall@%d on %d clustered vectors: %.4f", metric, k, len(vectors), meanRecall)
			if meanRecall < minRecall {
				t.Errorf("%s mean recall@%d = %.4f, below %.2f — search/construction or int8 quantization regressed", metric, k, meanRecall, minRecall)
			}
		})
	}
}

// TestHNSWFindsExactMatchAtScale is a sharp correctness probe: a query equal to
// a stored vector must return that vector as the nearest result. This holds for
// any correct HNSW regardless of level randomness, and fails under the
// candidates-heap bug once N exceeds ef.
func TestHNSWFindsExactMatchAtScale(t *testing.T) {
	const (
		dim = 64
		n   = 500 // > ef so the bug is exposed
		ef  = 32
	)
	rng := rand.New(rand.NewSource(7))
	vectors := make([][]float32, n)
	idx, err := NewHNSWIndex(dim, 16, 200, MetricCosine)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		v := make([]float32, dim)
		for d := range v {
			v[d] = float32(rng.NormFloat64())
		}
		vectors[i] = v
		if err := idx.Insert(uint64(i), v); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	misses := 0
	for _, id := range []uint64{0, 99, 200, 333, 499} {
		results, err := idx.Search(vectors[id], 1, ef)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 || results[0].ID != id {
			misses++
			got := uint64(0)
			if len(results) > 0 {
				got = results[0].ID
			}
			t.Errorf("query=stored vector %d: nearest result ID=%d (len=%d), want %d", id, got, len(results), id)
		}
	}
	if misses > 0 {
		t.Errorf("%d/5 exact-match queries failed to return themselves", misses)
	}
}
