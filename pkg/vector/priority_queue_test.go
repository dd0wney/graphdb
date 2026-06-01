package vector

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"testing"
)

// TestPriorityQueueMaxHeapPopOrder pins the behavioral contract the value-typed
// heap must preserve when it replaces container/heap: pop returns items in
// descending distance order (max-heap), so pq[0] is always the furthest
// element. Every searchLayer relies on w[0] being the furthest candidate.
func TestPriorityQueueMaxHeapPopOrder(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var pq priorityQueue
	const n = 500
	for i := 0; i < n; i++ {
		pqPush(&pq, queueItem{id: uint64(i), distance: rng.Float32()})
	}
	if pq.Len() != n {
		t.Fatalf("Len after %d pushes = %d, want %d", n, pq.Len(), n)
	}

	prev := float32(math.MaxFloat32)
	popped := 0
	for pq.Len() > 0 {
		item := pqPop(&pq)
		if item.distance > prev {
			t.Fatalf("pop order not descending: got %v after %v", item.distance, prev)
		}
		prev = item.distance
		popped++
	}
	if popped != n {
		t.Fatalf("popped %d items, want %d (item loss)", popped, n)
	}
}

// TestPriorityQueueKeepsNearestEf exercises the exact bounded-set pattern from
// searchLayerKNN: a max-heap where the furthest is evicted once size exceeds
// ef, leaving the ef nearest. Validates the search invariant directly.
func TestPriorityQueueKeepsNearestEf(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	const total, ef = 200, 50

	dists := make([]float32, total)
	var w priorityQueue
	for i := 0; i < total; i++ {
		d := rng.Float32()
		dists[i] = d
		pqPush(&w, queueItem{id: uint64(i), distance: d})
		if w.Len() > ef {
			pqPop(&w) // evict furthest
		}
	}
	if w.Len() != ef {
		t.Fatalf("bounded set size = %d, want %d", w.Len(), ef)
	}

	// The retained set must be exactly the ef smallest distances.
	sort.Slice(dists, func(i, j int) bool { return dists[i] < dists[j] })
	threshold := dists[ef-1] // ef-th smallest
	for w.Len() > 0 {
		item := pqPop(&w)
		if item.distance > threshold {
			t.Fatalf("retained distance %v exceeds ef-th smallest %v", item.distance, threshold)
		}
	}
}

// TestHNSWConcurrentSearchMatchesSequential guards the value-heap rewrite under
// the RLock concurrency that Search actually runs with: many goroutines search
// the same index simultaneously and every result must match the sequential
// answer. The failure mode this catches is shared per-search state leaking
// between concurrent calls — which would silently corrupt recall, not panic.
func TestHNSWConcurrentSearchMatchesSequential(t *testing.T) {
	const dim, n, k, ef = 64, 1000, 10, 50
	index, err := NewHNSWIndex(dim, 16, 200, MetricCosine)
	if err != nil {
		t.Fatalf("NewHNSWIndex: %v", err)
	}

	rng := rand.New(rand.NewSource(99))
	for i := 0; i < n; i++ {
		vec := make([]float32, dim)
		for j := range vec {
			vec[j] = rng.Float32()
		}
		index.Insert(uint64(i), vec)
	}

	query := make([]float32, dim)
	for j := range query {
		query[j] = rng.Float32()
	}

	want, err := index.Search(query, k, ef)
	if err != nil {
		t.Fatalf("sequential Search: %v", err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := index.Search(query, k, ef)
			if err != nil {
				t.Errorf("concurrent Search: %v", err)
				return
			}
			if len(got) != len(want) {
				t.Errorf("result length = %d, want %d", len(got), len(want))
				return
			}
			for i := range got {
				if got[i].ID != want[i].ID || got[i].Distance != want[i].Distance {
					t.Errorf("result[%d] = {%d, %v}, want {%d, %v}",
						i, got[i].ID, got[i].Distance, want[i].ID, want[i].Distance)
					return
				}
			}
		}()
	}
	wg.Wait()
}
