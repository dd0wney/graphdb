package query

import "fmt"

const (
	// MaxCartesianProductResults limits the maximum size of cartesian product
	// to prevent memory exhaustion from queries like MATCH (a), (b), (c)
	MaxCartesianProductResults = 100000

	// MaxIntermediateResults limits intermediate results during query execution
	MaxIntermediateResults = 1000000
)

// MatchStep executes a MATCH clause
type MatchStep struct {
	match *MatchClause
}

func (ms *MatchStep) Execute(ctx *ExecutionContext) error {
	newResults := make([]*BindingSet, 0)

	// The patterns of a MATCH are a join: each row is extended through every
	// pattern in turn, and a variable an earlier pattern bound constrains the
	// later ones. Appending each pattern's matches as separate rows, as this
	// once did, returned rows that bound only one pattern's variables.
	for _, binding := range ctx.results {
		rows := []*BindingSet{binding}
		for _, pattern := range ms.match.Patterns {
			next := make([]*BindingSet, 0, len(rows))
			for _, row := range rows {
				matches, err := ms.matchPattern(ctx, pattern, row)
				if err != nil {
					return err
				}
				next = append(next, matches...)

				// Check intermediate result limit to prevent memory exhaustion
				if len(newResults)+len(next) > MaxIntermediateResults {
					return fmt.Errorf("query produced %d intermediate results, exceeding limit of %d; consider adding more specific filters or LIMIT clause",
						len(newResults)+len(next), MaxIntermediateResults)
				}
			}
			rows = next
			if len(rows) == 0 {
				break
			}
		}
		newResults = append(newResults, rows...)
	}

	// Always update results, even if empty
	// If no matches found, results should be empty
	ctx.results = newResults

	return nil
}

func (ms *MatchStep) matchPattern(ctx *ExecutionContext, pattern *Pattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)

	// Simple case: single node
	if len(pattern.Nodes) == 1 && len(pattern.Relationships) == 0 {
		return ms.matchNode(ctx, pattern.Nodes[0], existingBinding)
	}

	// Pattern with relationships
	if len(pattern.Nodes) >= 2 && len(pattern.Relationships) >= 1 {
		return ms.matchPath(ctx, pattern, existingBinding)
	}

	// Multiple independent nodes (cartesian product)
	if len(pattern.Nodes) >= 2 && len(pattern.Relationships) == 0 {
		return ms.matchCartesianProduct(ctx, pattern, existingBinding)
	}

	return results, nil
}
