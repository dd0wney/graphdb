package search

import (
	"math"
	"sort"
	"strings"
)

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

		// Term frequency in document (defensive: check nested map existence)
		tf := 0.0
		if termMap, termExists := fti.index[term]; termExists {
			if positions, nodeExists := termMap[nodeID]; nodeExists {
				tf = float64(len(positions))
			}
		}

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

			matrix[i][j] = minInt(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

func minInt(a, b, c int) int {
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
