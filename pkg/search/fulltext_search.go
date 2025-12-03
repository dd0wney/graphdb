package search

import (
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

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

	// Get positions of first term (defensive: check map existence)
	termMap, termExists := fti.index[terms[0]]
	if !termExists {
		return false
	}
	positions, nodeExists := termMap[nodeID]
	if !nodeExists {
		return false
	}

	// For each position of the first term, check if subsequent terms follow
	for _, pos := range positions {
		match := true
		for i := 1; i < len(terms); i++ {
			// Defensive: check nested map existence
			nextTermMap, nextTermExists := fti.index[terms[i]]
			if !nextTermExists {
				match = false
				break
			}
			nextPositions, nextNodeExists := nextTermMap[nodeID]
			if !nextNodeExists {
				match = false
				break
			}

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
