package search

import (
	"fmt"
	"math"
	"sort"
	"strings"
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

// IndexNodes indexes all nodes with specified labels and properties
func (fti *FullTextIndex) IndexNodes(labels []string, properties []string) error {
	fti.indexMu.Lock()
	defer fti.indexMu.Unlock()

	fti.indexedLabels = labels
	fti.indexedProps = properties

	// Get all nodes with specified labels
	for _, label := range labels {
		nodes, err := fti.gs.FindNodesByLabel(label)
		if err != nil {
			return fmt.Errorf("failed to get nodes for label %s: %w", label, err)
		}

		for _, node := range nodes {
			fti.indexNode(node, properties)
		}
	}

	return nil
}

// indexNode indexes a single node (must be called with lock held)
func (fti *FullTextIndex) indexNode(node *storage.Node, properties []string) {
	// Extract text content from specified properties
	var textParts []string
	for _, prop := range properties {
		if val, ok := node.Properties[prop]; ok {
			if val.Type == storage.TypeString {
				if str, err := val.AsString(); err == nil {
					textParts = append(textParts, str)
				}
			}
		}
	}

	if len(textParts) == 0 {
		return
	}

	content := strings.Join(textParts, " ")
	fti.nodeContent[node.ID] = content

	// Tokenize and index
	tokens := tokenize(content)
	seenTerms := make(map[string]bool)

	for pos, token := range tokens {
		term := normalize(token)
		if term == "" {
			continue
		}

		// Add to inverted index
		if fti.index[term] == nil {
			fti.index[term] = make(map[uint64][]int)
		}
		fti.index[term][node.ID] = append(fti.index[term][node.ID], pos)

		// Update document frequency (once per term per document)
		if !seenTerms[term] {
			fti.docFreq[term]++
			seenTerms[term] = true
		}
	}

	fti.totalDocs++
}

// UpdateNode updates the index for a specific node
func (fti *FullTextIndex) UpdateNode(nodeID uint64) error {
	fti.indexMu.Lock()
	defer fti.indexMu.Unlock()

	// Remove old index entries for this node
	for term := range fti.index {
		if _, exists := fti.index[term][nodeID]; exists {
			delete(fti.index[term], nodeID)
			fti.docFreq[term]--
			if fti.docFreq[term] == 0 {
				delete(fti.docFreq, term)
			}
			if len(fti.index[term]) == 0 {
				delete(fti.index, term)
			}
		}
	}

	// Remove from node content
	delete(fti.nodeContent, nodeID)
	fti.totalDocs--

	// Reindex the node
	node, err := fti.gs.GetNode(nodeID)
	if err != nil {
		return err
	}

	fti.indexNode(node, fti.indexedProps)
	return nil
}

// Search performs a basic text search (multi-word is treated as AND)
func (fti *FullTextIndex) Search(query string) ([]SearchResult, error) {
	if query == "" {
		return []SearchResult{}, nil
	}

	fti.indexMu.RLock()
	defer fti.indexMu.RUnlock()

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return []SearchResult{}, nil
	}

	// For multi-word queries, find intersection (AND)
	var candidateNodes map[uint64]bool
	for i, token := range tokens {
		term := normalize(token)
		if term == "" {
			continue
		}

		termNodes := make(map[uint64]bool)
		if postings, ok := fti.index[term]; ok {
			for nodeID := range postings {
				termNodes[nodeID] = true
			}
		}

		if i == 0 {
			candidateNodes = termNodes
		} else {
			// Intersection
			newCandidates := make(map[uint64]bool)
			for nodeID := range candidateNodes {
				if termNodes[nodeID] {
					newCandidates[nodeID] = true
				}
			}
			candidateNodes = newCandidates
		}
	}

	// Score and rank results
	results := fti.scoreResults(candidateNodes, tokens)
	return results, nil
}

// SearchPhrase searches for an exact phrase
func (fti *FullTextIndex) SearchPhrase(phrase string) ([]SearchResult, error) {
	if phrase == "" {
		return []SearchResult{}, nil
	}

	fti.indexMu.RLock()
	defer fti.indexMu.RUnlock()

	tokens := tokenize(phrase)
	if len(tokens) == 0 {
		return []SearchResult{}, nil
	}

	// Find nodes containing all terms
	terms := make([]string, len(tokens))
	for i, token := range tokens {
		terms[i] = normalize(token)
	}

	// Start with nodes containing the first term
	candidateNodes := make(map[uint64]bool)
	if postings, ok := fti.index[terms[0]]; ok {
		for nodeID := range postings {
			candidateNodes[nodeID] = true
		}
	}

	// Check each candidate for the exact phrase
	matchingNodes := make(map[uint64]bool)
	for nodeID := range candidateNodes {
		if fti.containsPhrase(nodeID, terms) {
			matchingNodes[nodeID] = true
		}
	}

	results := fti.scoreResults(matchingNodes, tokens)
	return results, nil
}

// containsPhrase checks if a node contains the exact phrase
func (fti *FullTextIndex) containsPhrase(nodeID uint64, terms []string) bool {
	if len(terms) == 0 {
		return false
	}

	// Get positions of first term
	positions := fti.index[terms[0]][nodeID]

	// For each position of the first term, check if subsequent terms follow
	for _, pos := range positions {
		match := true
		for i := 1; i < len(terms); i++ {
			nextPositions := fti.index[terms[i]][nodeID]
			expectedPos := pos + i

			found := false
			for _, p := range nextPositions {
				if p == expectedPos {
					found = true
					break
				}
			}

			if !found {
				match = false
				break
			}
		}

		if match {
			return true
		}
	}

	return false
}

// SearchBoolean performs boolean search with AND, OR, NOT operators
func (fti *FullTextIndex) SearchBoolean(query string) ([]SearchResult, error) {
	fti.indexMu.RLock()
	defer fti.indexMu.RUnlock()

	// Simple boolean parser
	query = strings.ToUpper(query)

	var results map[uint64]bool

	if strings.Contains(query, " AND ") {
		parts := strings.Split(query, " AND ")
		results = fti.searchAND(parts)
	} else if strings.Contains(query, " OR ") {
		parts := strings.Split(query, " OR ")
		results = fti.searchOR(parts)
	} else if strings.Contains(query, " NOT ") {
		parts := strings.Split(query, " NOT ")
		results = fti.searchNOT(parts)
	} else {
		// No boolean operator, treat as regular search
		fti.indexMu.RUnlock()
		return fti.Search(query)
	}

	return fti.scoreResults(results, tokenize(query)), nil
}

func (fti *FullTextIndex) searchAND(terms []string) map[uint64]bool {
	result := make(map[uint64]bool)

	for i, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		termNodes := make(map[uint64]bool)

		if postings, ok := fti.index[normalize(term)]; ok {
			for nodeID := range postings {
				termNodes[nodeID] = true
			}
		}

		if i == 0 {
			result = termNodes
		} else {
			// Intersection
			newResult := make(map[uint64]bool)
			for nodeID := range result {
				if termNodes[nodeID] {
					newResult[nodeID] = true
				}
			}
			result = newResult
		}
	}

	return result
}

func (fti *FullTextIndex) searchOR(terms []string) map[uint64]bool {
	result := make(map[uint64]bool)

	for _, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		if postings, ok := fti.index[normalize(term)]; ok {
			for nodeID := range postings {
				result[nodeID] = true
			}
		}
	}

	return result
}

func (fti *FullTextIndex) searchNOT(parts []string) map[uint64]bool {
	if len(parts) < 2 {
		return make(map[uint64]bool)
	}

	// Get nodes matching the first part
	include := fti.searchAND([]string{parts[0]})

	// Get nodes matching the NOT part
	exclude := fti.searchAND([]string{parts[1]})

	// Remove excluded nodes
	result := make(map[uint64]bool)
	for nodeID := range include {
		if !exclude[nodeID] {
			result[nodeID] = true
		}
	}

	return result
}

// SearchFuzzy performs fuzzy search with edit distance tolerance
func (fti *FullTextIndex) SearchFuzzy(query string, maxDistance int) ([]SearchResult, error) {
	fti.indexMu.RLock()
	defer fti.indexMu.RUnlock()

	queryTerm := normalize(query)
	matches := make(map[uint64]bool)

	// Find all terms within edit distance
	for term := range fti.index {
		if levenshteinDistance(queryTerm, term) <= maxDistance {
			for nodeID := range fti.index[term] {
				matches[nodeID] = true
			}
		}
	}

	results := fti.scoreResults(matches, []string{query})
	return results, nil
}

// SearchInProperty searches only in a specific property
func (fti *FullTextIndex) SearchInProperty(property string, query string) ([]SearchResult, error) {
	fti.indexMu.RLock()
	defer fti.indexMu.RUnlock()

	queryTerm := normalize(query)
	matches := make(map[uint64]bool)

	// Get candidate nodes
	if postings, ok := fti.index[queryTerm]; ok {
		for nodeID := range postings {
			// Check if the term appears in the specified property
			node, err := fti.gs.GetNode(nodeID)
			if err != nil {
				continue
			}

			if val, ok := node.Properties[property]; ok {
				if val.Type == storage.TypeString {
					if str, err := val.AsString(); err == nil {
						text := strings.ToLower(str)
						if strings.Contains(text, queryTerm) {
							matches[nodeID] = true
						}
					}
				}
			}
		}
	}

	results := fti.scoreResults(matches, []string{query})
	return results, nil
}

// scoreResults calculates TF-IDF scores and returns sorted results
func (fti *FullTextIndex) scoreResults(nodeIDs map[uint64]bool, queryTokens []string) []SearchResult {
	results := make([]SearchResult, 0, len(nodeIDs))

	for nodeID := range nodeIDs {
		score := fti.calculateScore(nodeID, queryTokens)

		node, err := fti.gs.GetNode(nodeID)
		if err != nil {
			continue
		}

		results = append(results, SearchResult{
			NodeID: nodeID,
			Score:  score,
			Node:   node,
		})
	}

	// Sort by score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// calculateScore calculates TF-IDF score for a document
func (fti *FullTextIndex) calculateScore(nodeID uint64, queryTokens []string) float64 {
	score := 0.0

	for _, token := range queryTokens {
		term := normalize(token)
		if term == "" {
			continue
		}

		// Term frequency in document
		tf := float64(len(fti.index[term][nodeID]))

		// Inverse document frequency
		df := float64(fti.docFreq[term])
		idf := 1.0 // Default IDF of 1.0
		if df > 0 && fti.totalDocs > 0 {
			// Add 1 to avoid log(1) = 0 for terms in all documents
			idf = math.Log(float64(fti.totalDocs+1) / (df + 1))
		}

		// Even if IDF is low, TF still contributes to score
		score += tf * (1.0 + idf)
	}

	return score
}

// tokenize splits text into tokens
func tokenize(text string) []string {
	// Simple whitespace tokenization
	text = strings.ToLower(text)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	return words
}

// normalize normalizes a term for indexing
func normalize(term string) string {
	return strings.ToLower(strings.TrimSpace(term))
}

// levenshteinDistance calculates the edit distance between two strings
func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	// Create matrix
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
	}

	// Initialize first row and column
	for i := 0; i <= len(s1); i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len(s2); j++ {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}

			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
