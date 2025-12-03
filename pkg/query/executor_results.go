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

	node, ok := obj.(*storage.Node)
	if !ok {
		return nil
	}

	if expr.Property != "" {
		if prop, exists := node.Properties[expr.Property]; exists {
			// Reuse ExtractValue from AggregationComputer
			return computer.ExtractValue(prop)
		}
		return nil
	}

	// Return whole node
	return node
}
