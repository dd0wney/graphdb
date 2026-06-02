package vector

// hnswNode represents a node in the HNSW graph
type hnswNode struct {
	id      uint64
	vector  []float32
	level   int
	friends [][]uint64 // Connections at each layer [layer][neighbors]
}

// SearchResult represents a search result with ID and distance
type SearchResult struct {
	ID       uint64
	Distance float32
}

// priorityQueue is a max-heap of queueItems ordered by distance: the element
// with the largest distance sits at index 0.
//
// It uses value semantics ([]queueItem, not []*queueItem) and the package-level
// sift helpers pqPush/pqPop instead of container/heap. container/heap's Push/Pop
// take `any`, so every element was heap-boxed and every push allocated a
// *queueItem. On the HNSW search hot path that was ~96% of per-search
// allocations (lines 113-114 of the old hnsw_search.go; see BenchmarkHNSWSearch).
// Storing values inline in the backing array removes the per-item allocation
// entirely while preserving the exact max-heap ordering the search relies on.
type priorityQueue []queueItem

type queueItem struct {
	id       uint64
	distance float32
}

func (pq priorityQueue) Len() int { return len(pq) }

// less reports whether element i outranks j in the max-heap: a larger distance
// has higher priority. Mirrors the previous priorityQueue.Less exactly so the
// heap ordering — and therefore search recall — is unchanged.
func (pq priorityQueue) less(i, j int) bool { return pq[i].distance > pq[j].distance }

// pqPush appends item and restores the max-heap invariant by sifting it up.
// Behaviourally equivalent to heap.Push(&pq, &item), without the allocation.
func pqPush(pq *priorityQueue, item queueItem) {
	*pq = append(*pq, item)
	pqUp(*pq, len(*pq)-1)
}

// pqPop removes and returns the max element (index 0), restoring the invariant
// by sifting the moved tail element down. Behaviourally equivalent to
// heap.Pop(&pq). Caller must ensure Len() > 0.
func pqPop(pq *priorityQueue) queueItem {
	old := *pq
	n := len(old) - 1
	old[0], old[n] = old[n], old[0]
	pqDown(old, 0, n)
	item := old[n]
	*pq = old[:n]
	return item
}

// pqUp and pqDown reproduce container/heap's up/down sift loops (0-based:
// parent = (i-1)/2, children = 2i+1, 2i+2) against priorityQueue.less.
func pqUp(pq priorityQueue, j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !pq.less(j, i) {
			break
		}
		pq[i], pq[j] = pq[j], pq[i]
		j = i
	}
}

func pqDown(pq priorityQueue, i, n int) {
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && pq.less(j2, j1) {
			j = j2 // right child sorts first
		}
		if !pq.less(j, i) {
			break
		}
		pq[i], pq[j] = pq[j], pq[i]
		i = j
	}
}

// candidateQueue is a MIN-heap of queueItems ordered by distance: the element
// with the SMALLEST distance sits at index 0. HNSW's layer search must always
// expand the nearest unexplored candidate, so the candidate set is a min-heap
// (whereas the result set `w` is a priorityQueue max-heap, so its furthest
// element is the eviction target). It mirrors priorityQueue's value-semantics,
// no-alloc design with the comparison reversed; a distinct type prevents
// accidentally driving it with the max-heap pq* helpers.
type candidateQueue []queueItem

func (cq candidateQueue) Len() int { return len(cq) }

// less reports whether element i outranks j in the min-heap: a smaller distance
// has higher priority (sits closer to the root).
func (cq candidateQueue) less(i, j int) bool { return cq[i].distance < cq[j].distance }

// cqPush/cqPop/cqUp/cqDown are the min-heap analogues of pqPush/pqPop/pqUp/pqDown.
func cqPush(cq *candidateQueue, item queueItem) {
	*cq = append(*cq, item)
	cqUp(*cq, len(*cq)-1)
}

func cqPop(cq *candidateQueue) queueItem {
	old := *cq
	n := len(old) - 1
	old[0], old[n] = old[n], old[0]
	cqDown(old, 0, n)
	item := old[n]
	*cq = old[:n]
	return item
}

func cqUp(cq candidateQueue, j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !cq.less(j, i) {
			break
		}
		cq[i], cq[j] = cq[j], cq[i]
		j = i
	}
}

func cqDown(cq candidateQueue, i, n int) {
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && cq.less(j2, j1) {
			j = j2 // right child sorts first
		}
		if !cq.less(j, i) {
			break
		}
		cq[i], cq[j] = cq[j], cq[i]
		i = j
	}
}

// extractNearest drains the max-heap result set w and returns up to n nearest
// items in ascending-distance order. w is a max-heap (furthest at root), so
// popping yields furthest-first; filling the backing slice from the tail
// produces ascending order, and the leading n are the nearest. Used by Search
// (n = k) and selectNeighbors (n = m), both of which need the NEAREST subset —
// the previous pqPop-first logic returned the farthest subset.
func extractNearest(w *priorityQueue, n int) []queueItem {
	count := w.Len()
	ordered := make([]queueItem, count)
	for i := count - 1; i >= 0; i-- {
		ordered[i] = pqPop(w) // furthest-first into the tail → ascending
	}
	if n > count {
		n = count
	}
	return ordered[:n]
}
