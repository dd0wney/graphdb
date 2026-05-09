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

// priorityQueue implements a max-heap for nearest neighbor search
type priorityQueue []*queueItem

type queueItem struct {
	id       uint64
	distance float32
}

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// Max-heap: larger distances have higher priority
	return pq[i].distance > pq[j].distance
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *priorityQueue) Push(x any) {
	// heap.Interface.Push contract: callers always pass *queueItem.
	// Mirrors rankedEdgeHeap.Push / rankedNodeHeap.Push in pkg/algorithms.
	item, ok := x.(*queueItem)
	if !ok {
		panic("priorityQueue.Push: expected *queueItem")
	}
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}
