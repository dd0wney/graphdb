package vector

import (
	"fmt"
)

// KVStore is a generic key-value interface
type KVStore interface {
	Get(key []byte) ([]byte, bool)
	Put(key, value []byte) error
	Delete(key []byte) error
}

// hnswNode represents a node in the HNSW graph
type hnswNode struct {
	id      uint64
	vector  []float32
	level   int
	friends [][]uint64 // Connections at each layer [layer][neighbors]
}

// nodeStore abstracts the storage of HNSW nodes
type nodeStore interface {
	Get(id uint64) (*hnswNode, bool)
	Put(node *hnswNode)
	Delete(id uint64)
	Len() int
	Iterate(fn func(*hnswNode) bool)
}

// memoryNodeStore is an in-memory implementation of nodeStore
type memoryNodeStore struct {
	nodes map[uint64]*hnswNode
}

func (s *memoryNodeStore) Get(id uint64) (*hnswNode, bool) {
	n, ok := s.nodes[id]
	return n, ok
}

func (s *memoryNodeStore) Put(node *hnswNode) {
	s.nodes[node.id] = node
}

func (s *memoryNodeStore) Delete(id uint64) {
	delete(s.nodes, id)
}

func (s *memoryNodeStore) Len() int {
	return len(s.nodes)
}

func (s *memoryNodeStore) Iterate(fn func(*hnswNode) bool) {
	for _, n := range s.nodes {
		if !fn(n) {
			break
		}
	}
}

// kvNodeStore is a persistent implementation of nodeStore backed by KVStore
type kvNodeStore struct {
	kv     KVStore
	prefix string
	cache  map[uint64]*hnswNode
}

func (s *kvNodeStore) Get(id uint64) (*hnswNode, bool) {
	if n, ok := s.cache[id]; ok {
		return n, true
	}
	key := []byte(fmt.Sprintf("%s%d", s.prefix, id))
	data, ok := s.kv.Get(key)
	if !ok {
		return nil, false
	}
	n := DeserializeHNSWNode(data)
	if n != nil {
		s.cache[id] = n
	}
	return n, n != nil
}

func (s *kvNodeStore) Put(node *hnswNode) {
	s.cache[node.id] = node
	key := []byte(fmt.Sprintf("%s%d", s.prefix, node.id))
	_ = s.kv.Put(key, SerializeHNSWNode(node))
}

func (s *kvNodeStore) Delete(id uint64) {
	delete(s.cache, id)
	key := []byte(fmt.Sprintf("%s%d", s.prefix, id))
	_ = s.kv.Delete(key)
}

func (s *kvNodeStore) Len() int {
	return len(s.cache)
}

func (s *kvNodeStore) Iterate(fn func(*hnswNode) bool) {
	// For the spike, we'll only iterate over cached nodes.
	// A real implementation would need KV range scan.
	for _, n := range s.cache {
		if !fn(n) {
			break
		}
	}
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
