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

// orderByColumnName resolves the column name for an ORDER BY item.
// It checks the row's existing keys for alias matches and falls back
// to the formatted property expression.
func orderByColumnName(item *OrderByItem, row map[string]any) string {
	if item.Expression != nil {
		// When property is empty, it's a bare variable name (possibly an alias)
		if item.Expression.Property == "" {
			if _, ok := row[item.Expression.Variable]; ok {
				return item.Expression.Variable
			}
		}
		// Try formatted name: "var.prop"
		name := fmt.Sprintf("%s.%s", item.Expression.Variable, item.Expression.Property)
		if _, ok := row[name]; ok {
			return name
		}
		return name
	}
	return "<expr>"
}

// sortRows sorts result rows according to ORDER BY criteria
func (e *Executor) sortRows(rows []map[string]any, orderBy []*OrderByItem) {
	if len(orderBy) == 0 || len(rows) == 0 {
		return
	}

	sort.SliceStable(rows, func(i, j int) bool {
		for _, item := range orderBy {
			colName := orderByColumnName(item, rows[i])
			valI := rows[i][colName]
			valJ := rows[j][colName]

			cmp := compareValues(valI, valJ)
			if cmp == 0 {
				continue // tie — check next column
			}
			if item.Ascending {
				return cmp < 0
			}
			return cmp > 0
		}
		return false // all columns equal
	})
}
