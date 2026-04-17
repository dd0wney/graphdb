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

type queueItem struct {
	id       uint64
	distance float32
}

// priorityQueue is a max-heap used for the result set W.
// Keeping the farthest element at the root lets us efficiently drop the
// worst result when the set exceeds ef, and drain in descending order to
// extract the k nearest at query time.
type priorityQueue []*queueItem

func (pq priorityQueue) Len() int            { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool  { return pq[i].distance > pq[j].distance }
func (pq priorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *priorityQueue) Push(x any)         { *pq = append(*pq, x.(*queueItem)) }
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// minPriorityQueue is a min-heap used for the candidate set C.
// Popping the nearest candidate enables correct greedy graph traversal
// toward the query — HNSW's correctness depends on exploring nearest-first.
type minPriorityQueue []*queueItem

func (pq minPriorityQueue) Len() int            { return len(pq) }
func (pq minPriorityQueue) Less(i, j int) bool  { return pq[i].distance < pq[j].distance }
func (pq minPriorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *minPriorityQueue) Push(x any)         { *pq = append(*pq, x.(*queueItem)) }
func (pq *minPriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}
