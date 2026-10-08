package query

import (
	"fmt"
	"strings"

	"github.com/dd0wney/graphdb/pkg/storage"
)

const (
	// invalidColumnName is returned when column name cannot be determined
	invalidColumnName = "<invalid>"
)

// orderByColKey returns the map key for an ORDER BY expression.
// Bare variables (empty Property) use just the variable name to match aliases.
func orderByColKey(expr *PropertyExpression) string {
	if expr.Property == "" {
		return expr.Variable
	}
	return fmt.Sprintf("%s.%s", expr.Variable, expr.Property)
}

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
		switch item.ValueExpr.(type) {
		case *FunctionCallExpression:
			return item.ValueExpr.(*FunctionCallExpression).Name + "(...)"
		case *ArithmeticExpression, *UnaryExpression, *BinaryExpression:
			return "<expr>"
		}
	}

	if item.Expression != nil {
		return fmt.Sprintf("%s.%s", item.Expression.Variable, item.Expression.Property)
	}

	return invalidColumnName
}

// buildResultSet builds the final result set
func (e *Executor) buildResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	// Group when the query says GROUP BY, or, as openCypher does, when it
	// aggregates next to a non-aggregate item: that item is a grouping key.
	if len(returnClause.GroupBy) > 0 || (hasAggregates(returnClause.Items) && len(nonAggregateItems(returnClause.Items)) > 0) {
		return e.buildGroupedResultSet(ctx, returnClause, limit, skip)
	}

	// Aggregates with no key: one row over all bindings.
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

// buildGroupedResultSet groups the bindings and computes the aggregates of
// each group. The key is the non-aggregate RETURN items' values, or the GROUP
// BY expressions when the query has them. Groups keep the order in which they
// first appear, and each output row takes its key columns from the first
// binding of its group, so a key keeps its type: an int stays an int, a node
// stays a node.
func (e *Executor) buildGroupedResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	resultSet := &ResultSet{
		Columns: make([]string, 0, len(returnClause.Items)),
		Rows:    make([]map[string]any, 0),
	}
	for _, item := range returnClause.Items {
		resultSet.Columns = append(resultSet.Columns, buildColumnName(item))
	}

	computer := &AggregationComputer{}
	keyItems := nonAggregateItems(returnClause.Items)
	keyCols := make([]string, len(keyItems))
	for i, item := range keyItems {
		keyCols[i] = buildColumnName(item)
	}

	type group struct {
		first   *BindingSet
		members []*BindingSet
	}
	var order []*group
	byKey := make(map[string]*group)
	for _, binding := range ctx.results {
		var parts []any
		if len(returnClause.GroupBy) > 0 {
			for _, expr := range returnClause.GroupBy {
				parts = append(parts, e.extractValueFromBinding(binding, expr, computer))
			}
		} else {
			row := e.buildRow(binding, keyItems, keyCols, computer)
			for _, col := range keyCols {
				parts = append(parts, row[col])
			}
		}
		key := groupKey(parts)
		g, ok := byKey[key]
		if !ok {
			g = &group{first: binding}
			byKey[key] = g
			order = append(order, g)
		}
		g.members = append(g.members, binding)
	}

	for _, g := range order {
		row := computer.ComputeAggregates(ctx.aggregationContext(g.members), returnClause.Items)
		for col, val := range e.buildRow(g.first, keyItems, keyCols, computer) {
			row[col] = val
		}
		// GROUP BY expressions are also columns under their own name, as
		// they were before implicit grouping existed.
		for _, expr := range returnClause.GroupBy {
			name := fmt.Sprintf("%s.%s", expr.Variable, expr.Property)
			if _, present := row[name]; !present {
				row[name] = e.extractValueFromBinding(g.first, expr, computer)
			}
		}
		resultSet.Rows = append(resultSet.Rows, row)
	}
	resultSet.Count = len(resultSet.Rows)

	// Apply post-processing (ORDER BY, SKIP, LIMIT)
	e.applyPostProcessing(resultSet, returnClause, limit, skip, false)

	return resultSet
}

// nonAggregateItems returns the RETURN items that are grouping keys.
func nonAggregateItems(items []*ReturnItem) []*ReturnItem {
	keys := make([]*ReturnItem, 0, len(items))
	for _, item := range items {
		if item.Aggregate == "" {
			keys = append(keys, item)
		}
	}
	return keys
}

// groupKey encodes a group's key values so that two values share a group only
// when they are equal and of the same type: a node or relationship by its ID,
// null as its own value, anything else by type and value. Each part is length
// prefixed, so no value can contain a separator.
func groupKey(parts []any) string {
	var b strings.Builder
	for _, p := range parts {
		var s string
		switch v := p.(type) {
		case nil:
			s = "null"
		case *storage.Node:
			s = fmt.Sprintf("node:%d", v.ID)
		case *storage.Edge:
			s = fmt.Sprintf("edge:%d", v.ID)
		default:
			s = fmt.Sprintf("%T:%v", v, v)
		}
		fmt.Fprintf(&b, "%d:%s|", len(s), s)
	}
	return b.String()
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

	// Identify ORDER BY columns that aren't already in the result set
	var extraSortCols []string
	if len(returnClause.OrderBy) > 0 {
		colSet := make(map[string]bool, len(resultSet.Columns))
		for _, c := range resultSet.Columns {
			colSet[c] = true
		}
		for _, ob := range returnClause.OrderBy {
			if ob.Expression != nil {
				name := orderByColKey(ob.Expression)
				if !colSet[name] {
					extraSortCols = append(extraSortCols, name)
					colSet[name] = true
				}
			}
		}
	}

	// Build rows with extra sort columns injected from bindings
	computer := &AggregationComputer{}
	for _, binding := range ctx.results {
		row := e.buildRow(binding, returnClause.Items, resultSet.Columns, computer)
		for _, ob := range returnClause.OrderBy {
			if ob.Expression != nil {
				name := orderByColKey(ob.Expression)
				if _, exists := row[name]; !exists {
					row[name] = e.extractValueFromBinding(binding, ob.Expression, computer)
				}
			}
		}
		resultSet.Rows = append(resultSet.Rows, row)
	}

	// Apply post-processing (DISTINCT, ORDER BY, SKIP, LIMIT)
	e.applyPostProcessing(resultSet, returnClause, limit, skip, true)

	// Strip extra sort columns from final results
	for _, col := range extraSortCols {
		for _, row := range resultSet.Rows {
			delete(row, col)
		}
	}

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

	// Handle *storage.Edge bindings
	if edge, ok := obj.(*storage.Edge); ok {
		if expr.Property != "" {
			if prop, exists := edge.Properties[expr.Property]; exists {
				return extractStorageValue(prop)
			}
			return nil
		}
		return edge
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
