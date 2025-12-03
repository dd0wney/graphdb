package search

import (
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// FullTextIndex provides full-text search capabilities
type FullTextIndex struct {
	gs *storage.GraphStorage

	// Inverted index: term -> list of (nodeID, positions)
	index   map[string]map[uint64][]int
	indexMu sync.RWMutex

	// Document frequency: term -> number of documents containing it
	docFreq map[string]int

	// Node content: nodeID -> concatenated text content
	nodeContent map[uint64]string

	// Total documents indexed
	totalDocs int

	// Configuration
	indexedLabels []string
	indexedProps  []string
}

// SearchResult represents a search result with score
type SearchResult struct {
	NodeID uint64
	Score  float64
	Node   *storage.Node
}

// NewFullTextIndex creates a new full-text search index
func NewFullTextIndex(gs *storage.GraphStorage) *FullTextIndex {
	return &FullTextIndex{
		gs:          gs,
		index:       make(map[string]map[uint64][]int),
		docFreq:     make(map[string]int),
		nodeContent: make(map[uint64]string),
	}
}
