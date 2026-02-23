package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

const (
	// invalidColumnName is returned when column name cannot be determined
	invalidColumnName = "<invalid>"
)

// buildColumnName builds a column name from a return item
func buildColumnName(item *ReturnItem) string {
	if item.Alias != "" {
		return item.Alias
	}

	if item.Aggregate != "" {
		if item.Expression != nil {
			return fmt.Sprintf("%s(%s.%s)", item.Aggregate, item.Expression.Variable, item.Expression.Property)
		}
		return fmt.Sprintf("%s(*)", item.Aggregate)
	}

	if item.ValueExpr != nil {
		if fce, ok := item.ValueExpr.(*FunctionCallExpression); ok {
			return fce.Name + "(...)"
		}
	}

	if item.Expression != nil {
		return fmt.Sprintf("%s.%s", item.Expression.Variable, item.Expression.Property)
	}

	return invalidColumnName
}

// buildResultSet builds the final result set
func (e *Executor) buildResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	// Check if we have GROUP BY
	if len(returnClause.GroupBy) > 0 {
		return e.buildGroupedResultSet(ctx, returnClause, limit, skip)
	}

	// Check if we have aggregates (without GROUP BY)
	if hasAggregates(returnClause.Items) {
		return e.buildAggregateResultSet(ctx, returnClause)
	}

	// Build regular results
	return e.buildRegularResultSet(ctx, returnClause, limit, skip)
}

// buildAggregateResultSet builds results for aggregate queries without GROUP BY
func (e *Executor) buildAggregateResultSet(ctx *ExecutionContext, returnClause *ReturnClause) *ResultSet {
	resultSet := &ResultSet{
		Columns: make([]string, 0),
		Rows:    make([]map[string]any, 0),
	}

	computer := &AggregationComputer{}
	aggregateResult := computer.ComputeAggregates(ctx, returnClause.Items)

	// Build columns
	for _, item := range returnClause.Items {
		resultSet.Columns = append(resultSet.Columns, buildColumnName(item))
	}

	// Add single row with aggregate results
	resultSet.Rows = append(resultSet.Rows, aggregateResult)
	resultSet.Count = 1
	return resultSet
}

// buildGroupedResultSet builds results for GROUP BY queries
func (e *Executor) buildGroupedResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	resultSet := &ResultSet{
		Columns: make([]string, 0),
		Rows:    make([]map[string]any, 0),
	}

	computer := &AggregationComputer{}
	groupedResults := computer.ComputeGroupedAggregates(ctx, returnClause.Items, returnClause.GroupBy)

	// Build columns
	for _, item := range returnClause.Items {
		resultSet.Columns = append(resultSet.Columns, buildColumnName(item))
	}

	resultSet.Rows = groupedResults
	resultSet.Count = len(groupedResults)

	// Apply post-processing (ORDER BY, SKIP, LIMIT)
	e.applyPostProcessing(resultSet, returnClause, limit, skip, false)

	return resultSet
}

// buildRegularResultSet builds results for regular (non-aggregate) queries
func (e *Executor) buildRegularResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	resultSet := &ResultSet{
		Columns: make([]string, 0),
		Rows:    make([]map[string]any, 0),
	}

	// Determine columns
	for _, item := range returnClause.Items {
		resultSet.Columns = append(resultSet.Columns, buildColumnName(item))
	}

	// Build rows
	computer := &AggregationComputer{}
	for _, binding := range ctx.results {
		row := e.buildRow(binding, returnClause.Items, resultSet.Columns, computer)
		resultSet.Rows = append(resultSet.Rows, row)
	}

	// Apply post-processing (DISTINCT, ORDER BY, SKIP, LIMIT)
	e.applyPostProcessing(resultSet, returnClause, limit, skip, true)

	return resultSet
}

// buildRow builds a single result row from a binding
func (e *Executor) buildRow(binding *BindingSet, items []*ReturnItem, columns []string, computer *AggregationComputer) map[string]any {
	row := make(map[string]any)

	for i, item := range items {
		columnName := columns[i]

		// ValueExpr takes precedence (e.g. function calls)
		if item.ValueExpr != nil {
			row[columnName] = extractValue(item.ValueExpr, binding.bindings)
			continue
		}

		// Extract value - check for nil Expression first
		if item.Expression != nil {
			row[columnName] = e.extractValueFromBinding(binding, item.Expression, computer)
		}
	}

	return row
}

// extractValueFromBinding extracts a value from a binding based on expression
func (e *Executor) extractValueFromBinding(binding *BindingSet, expr *PropertyExpression, computer *AggregationComputer) any {
	obj, ok := binding.bindings[expr.Variable]
	if !ok {
		return nil
	}

	// Handle *storage.Node bindings
	if node, ok := obj.(*storage.Node); ok {
		if expr.Property != "" {
			// Real property takes precedence
			if prop, exists := node.Properties[expr.Property]; exists {
				return computer.ExtractValue(prop)
			}
			// Synthetic property: similarity_score from VectorSearchStep
			if expr.Property == "similarity_score" && binding.vectorScores != nil {
				if score, ok := binding.vectorScores[expr.Variable]; ok {
					return score
				}
			}
			return nil
		}
		return node
	}

	// Handle raw value bindings (e.g., from WITH projections)
	if expr.Property == "" {
		return obj
	}

	// Try map access for backwards compatibility
	if m, ok := obj.(map[string]any); ok {
		return m[expr.Property]
	}

	return nil
}
