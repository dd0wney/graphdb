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

// orderBySortKey is the row key that holds the value of the ORDER BY
// expression at index i. The leading NUL byte keeps it from colliding with any
// column name that a query can write.
func orderBySortKey(i int) string {
	return fmt.Sprintf("\x00order-by-%d", i)
}

// orderByKeys resolves the row key of each ORDER BY item, one time for the
// query. A bare variable and an alias both resolve to the variable name, a
// property to "var.prop", an expression to its sort key. Callers must hoist
// this out of any per-row or per-comparison loop, because each key is a new
// string. Every caller must have put a value under each key or refused the
// query first.
func orderByKeys(orderBy []*OrderByItem) []string {
	keys := make([]string, len(orderBy))
	for i, item := range orderBy {
		switch {
		case item.ValueExpr != nil:
			keys[i] = orderBySortKey(i)
		case item.Expression != nil:
			keys[i] = exprColumnName(item.Expression)
		default:
			keys[i] = invalidColumnName
		}
	}
	return keys
}

// sortRows sorts result rows according to ORDER BY criteria
func (e *Executor) sortRows(rows []map[string]any, orderBy []*OrderByItem) {
	if len(orderBy) == 0 || len(rows) == 0 {
		return
	}

	keys := orderByKeys(orderBy)
	sort.SliceStable(rows, func(i, j int) bool {
		for k, item := range orderBy {
			colName := keys[k]
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
