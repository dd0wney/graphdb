package query

import (
	"fmt"
	"sort"
)

// applyPostProcessing applies DISTINCT, ORDER BY, SKIP, and LIMIT to results
func (e *Executor) applyPostProcessing(resultSet *ResultSet, returnClause *ReturnClause, limit, skip int, applyDistinct bool) {
	// Apply DISTINCT
	if applyDistinct && returnClause.Distinct {
		resultSet.Rows = e.deduplicateRows(resultSet.Rows)
	}

	// Apply ORDER BY
	if len(returnClause.OrderBy) > 0 {
		e.sortRows(resultSet.Rows, returnClause.OrderBy)
	}

	// Apply SKIP
	if skip > 0 {
		if skip >= len(resultSet.Rows) {
			resultSet.Rows = []map[string]any{}
		} else {
			resultSet.Rows = resultSet.Rows[skip:]
		}
	}

	// Apply LIMIT
	if limit > 0 && len(resultSet.Rows) > limit {
		resultSet.Rows = resultSet.Rows[:limit]
	}

	resultSet.Count = len(resultSet.Rows)
}

// deduplicateRows removes duplicate rows from results
func (e *Executor) deduplicateRows(rows []map[string]any) []map[string]any {
	seen := make(map[string]bool)
	result := make([]map[string]any, 0)

	for _, row := range rows {
		key := fmt.Sprintf("%v", row)
		if !seen[key] {
			seen[key] = true
			result = append(result, row)
		}
	}

	return result
}

// sortRows sorts result rows according to ORDER BY criteria
func (e *Executor) sortRows(rows []map[string]any, orderBy []*OrderByItem) {
	if len(orderBy) == 0 {
		return
	}

	sort.Slice(rows, func(i, j int) bool {
		// Compare by first order by item
		item := orderBy[0]
		columnName := fmt.Sprintf("%s.%s", item.Expression.Variable, item.Expression.Property)

		valI := rows[i][columnName]
		valJ := rows[j][columnName]

		cmp := compareValues(valI, valJ)

		if item.Ascending {
			return cmp < 0
		}
		return cmp > 0
	})
}
