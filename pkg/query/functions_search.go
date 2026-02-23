package query

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
)

// SetSearchIndex configures full-text search for use in queries.
// Registers the search() function that captures the index via closure.
func (e *Executor) SetSearchIndex(idx *search.FullTextIndex) {
	e.searchIndex = idx

	// Register search function that closes over the index
	RegisterFunction("search", func(args []any) (any, error) {
		if len(args) < 2 {
			return float64(0), fmt.Errorf("search requires 2 arguments (text, query)")
		}

		text, ok := args[0].(string)
		if !ok {
			return float64(0), nil
		}

		queryText, ok := args[1].(string)
		if !ok {
			return float64(0), nil
		}

		// Simple relevance scoring: count term matches in the text
		return scoreText(text, queryText), nil
	})
}

// scoreText computes a simple TF-based relevance score for text against a query.
// Returns a float64 between 0.0 (no match) and 1.0 (all terms found).
func scoreText(text, queryText string) float64 {
	lowerText := strings.ToLower(text)
	terms := strings.Fields(strings.ToLower(queryText))

	if len(terms) == 0 {
		return float64(0)
	}

	matches := 0
	for _, term := range terms {
		if strings.Contains(lowerText, term) {
			matches++
		}
	}

	return float64(matches) / float64(len(terms))
}
