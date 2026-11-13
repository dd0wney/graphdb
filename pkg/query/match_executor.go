package query

import (
	"fmt"

	"github.com/dd0wney/graphdb/pkg/storage"
)

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

	// index, when set, replaces the scan of every node of the tenant with an
	// index lookup when matchNode has no bound node to check. An IndexLookupStep
	// sets it, so the index supplies the candidates and this step still binds
	// them to each input row.
	index *IndexLookupStep
}

// candidateNodes returns the nodes a single-node pattern is tried against.
func (ms *MatchStep) candidateNodes(ctx *ExecutionContext) ([]*storage.Node, error) {
	if ms.index != nil {
		return ms.index.lookup(ctx)
	}
	// Audit A6c-query: tenant-scoped node enumeration. Replaces the
	// "iterate 1..stats.NodeCount via GetNode" cross-tenant scan
	// anti-pattern (same shape as the A6c-graphql-resolvers cleanup).
	nodes, err := ctx.graph.GetAllNodesForTenant(ctx.tenantID)
	if err != nil {
		return nil, fmt.Errorf("node scan: %w", err)
	}
	return nodes, nil
}

func (ms *MatchStep) Execute(ctx *ExecutionContext) error {
	var newResults []*BindingSet

	// The patterns of a MATCH are a join: each row is extended through every
	// pattern in turn, and a variable an earlier pattern bound constrains the
	// later ones. Appending each pattern's matches as separate rows, as this
	// once did, returned rows that bound only one pattern's variables.
	for _, binding := range ctx.results {
		rows, err := ms.joinPatterns(ctx, binding, len(newResults))
		if err != nil {
			return err
		}
		if newResults == nil {
			newResults = rows
			continue
		}
		newResults = append(newResults, rows...)
	}

	// Always update results, even if empty
	// If no matches found, results should be empty
	ctx.results = newResults

	return nil
}

// joinPatterns extends one input row through every pattern. produced counts the
// rows earlier input rows already made, for the intermediate-result limit.
func (ms *MatchStep) joinPatterns(ctx *ExecutionContext, binding *BindingSet, produced int) ([]*BindingSet, error) {
	patterns := ms.match.Patterns
	// The common MATCH has one pattern. It needs none of the row lists below.
	if len(patterns) == 1 {
		rows, err := ms.matchPattern(ctx, patterns[0], binding)
		if err != nil {
			return nil, err
		}
		return rows, intermediateLimit(produced + len(rows))
	}

	rows := []*BindingSet{binding}
	for _, pattern := range patterns {
		next := make([]*BindingSet, 0, len(rows))
		for _, row := range rows {
			matches, err := ms.matchPattern(ctx, pattern, row)
			if err != nil {
				return nil, err
			}
			next = append(next, matches...)

			// Check intermediate result limit to prevent memory exhaustion
			if err := intermediateLimit(produced + len(next)); err != nil {
				return nil, err
			}
		}
		rows = next
		if len(rows) == 0 {
			break
		}
	}
	return rows, nil
}

func intermediateLimit(n int) error {
	if n > MaxIntermediateResults {
		return fmt.Errorf("query produced %d intermediate results, exceeding limit of %d; consider adding more specific filters or LIMIT clause",
			n, MaxIntermediateResults)
	}
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
