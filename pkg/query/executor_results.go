package query

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/dd0wney/graphdb/pkg/storage"
)

const (
	// invalidColumnName is returned when column name cannot be determined
	invalidColumnName = "<invalid>"
)

// exprColumnName is the one place that formats the name of a variable with an
// optional property: "n" for a bare variable, "n.name" for a property. The
// column name, the row key, the aggregate key and the ORDER BY lookup must all
// use it, or a row key stops matching its column.
func exprColumnName(expr *PropertyExpression) string {
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
			return fmt.Sprintf("%s(%s)", item.Aggregate, exprColumnName(item.Expression))
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
		return exprColumnName(item.Expression)
	}

	return invalidColumnName
}

// buildResultSet builds the final result set
func (e *Executor) buildResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) (*ResultSet, error) {
	if err := checkAggregateInputs(ctx, returnClause.Items); err != nil {
		return nil, err
	}

	if err := checkOrderByExpressions(returnClause); err != nil {
		return nil, err
	}

	// Group when the query says GROUP BY, or, as openCypher does, when it
	// aggregates next to a non-aggregate item: that item is a grouping key.
	if len(returnClause.GroupBy) > 0 || (hasAggregates(returnClause.Items) && len(nonAggregateItems(returnClause.Items)) > 0) {
		return e.buildGroupedResultSet(ctx, returnClause, limit, skip)
	}

	// Aggregates with no key: one row over all bindings.
	if hasAggregates(returnClause.Items) {
		return e.buildAggregateResultSet(ctx, returnClause), nil
	}

	// Build regular results
	return e.buildRegularResultSet(ctx, returnClause, limit, skip)
}

// checkOrderByExpressions refuses an ORDER BY expression on a path that cannot
// evaluate it. Only the plain path has a binding per row to evaluate against.
// After an aggregate or DISTINCT a row is a result row, not a binding, and
// openCypher lets ORDER BY name only a returned column there. Sorting by
// nothing, in silence, is the defect this check prevents.
func checkOrderByExpressions(rc *ReturnClause) error {
	if !hasOrderByExpression(rc.OrderBy) {
		return nil
	}
	if len(rc.GroupBy) > 0 || hasAggregates(rc.Items) {
		return refusef("ORDER BY with an expression is not supported after an aggregate; order by a returned column or alias")
	}
	if rc.Distinct {
		return refusef("ORDER BY with an expression is not supported with DISTINCT; order by a returned column or alias")
	}
	return nil
}

func hasOrderByExpression(orderBy []*OrderByItem) bool {
	for _, ob := range orderBy {
		if ob.ValueExpr != nil {
			return true
		}
	}
	return false
}

// checkAggregateInputs refuses sum, avg, min or max of a node or a
// relationship. They have no numeric value, and the helpers silently gave 0,
// null or an arbitrary entity; count and collect of an entity are defined.
func checkAggregateInputs(ctx *ExecutionContext, items []*ReturnItem) error {
	for _, item := range items {
		switch item.Aggregate {
		case "SUM", "AVG", "MIN", "MAX":
		default:
			continue
		}
		if item.Expression == nil || item.Expression.Property != "" {
			continue
		}
		for _, binding := range ctx.results {
			switch binding.bindings[item.Expression.Variable].(type) {
			case *storage.Node, *storage.Edge, []*storage.Edge:
				return fmt.Errorf("%s(%s): a node or relationship has no value to aggregate; aggregate one of its properties",
					strings.ToLower(item.Aggregate), item.Expression.Variable)
			}
		}
	}
	return nil
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
func (e *Executor) buildGroupedResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) (*ResultSet, error) {
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

	order, err := groupBindings(ctx.results, func(binding *BindingSet) ([]any, error) {
		var parts []any
		if len(returnClause.GroupBy) > 0 {
			for _, expr := range returnClause.GroupBy {
				parts = append(parts, e.extractValueFromBinding(binding, expr, computer))
			}
			return parts, nil
		}
		row, err := e.buildRow(binding, keyItems, keyCols, computer)
		if err != nil {
			return nil, err
		}
		for _, col := range keyCols {
			parts = append(parts, row[col])
		}
		return parts, nil
	})
	if err != nil {
		return nil, err
	}

	for _, g := range order {
		row := computer.ComputeAggregates(ctx.aggregationContext(g.members), returnClause.Items)
		keyRow, err := e.buildRow(g.first, keyItems, keyCols, computer)
		if err != nil {
			return nil, err
		}
		for col, val := range keyRow {
			row[col] = val
		}
		// GROUP BY expressions are also columns under their own name, as
		// they were before implicit grouping existed.
		for _, expr := range returnClause.GroupBy {
			name := exprColumnName(expr)
			if _, present := row[name]; !present {
				row[name] = e.extractValueFromBinding(g.first, expr, computer)
			}
		}
		resultSet.Rows = append(resultSet.Rows, row)
	}
	resultSet.Count = len(resultSet.Rows)

	// ORDER BY n.city after RETURN n.city AS city sorts by the key's column;
	// copy it under the name the sort looks up, and drop it afterwards.
	var extraSortCols []string
	for _, ob := range returnClause.OrderBy {
		if ob.Expression == nil {
			continue
		}
		name := exprColumnName(ob.Expression)
		col := groupedOrderColumn(returnClause, ob.Expression)
		if col == "" || col == name {
			continue
		}
		extraSortCols = append(extraSortCols, name)
		for _, row := range resultSet.Rows {
			row[name] = row[col]
		}
	}

	// Apply post-processing (ORDER BY, SKIP, LIMIT)
	e.applyPostProcessing(resultSet, returnClause, limit, skip, false)

	for _, col := range extraSortCols {
		for _, row := range resultSet.Rows {
			delete(row, col)
		}
	}
	return resultSet, nil
}

// bindingGroup is the bindings that share one grouping key. The first one is
// kept because a group's key columns are read from it.
type bindingGroup struct {
	first   *BindingSet
	members []*BindingSet
}

// groupBindings splits bindings into groups by the key values keyParts returns.
// RETURN and WITH both group through it, so they cannot disagree on what makes
// two keys equal (see groupKey). Groups keep the order in which their first
// member appears.
func groupBindings(bindings []*BindingSet, keyParts func(*BindingSet) ([]any, error)) ([]*bindingGroup, error) {
	var order []*bindingGroup
	byKey := make(map[string]*bindingGroup)
	for _, binding := range bindings {
		parts, err := keyParts(binding)
		if err != nil {
			return nil, err
		}
		key := groupKey(parts)
		g, ok := byKey[key]
		if !ok {
			g = &bindingGroup{first: binding}
			byKey[key] = g
			order = append(order, g)
		}
		g.members = append(g.members, binding)
	}
	return order, nil
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

// groupKey encodes a group's key values so that two values share a group
// exactly when openCypher treats them as equivalent: numbers compare as
// numbers (1 and 1.0, 0 and -0, NaN and NaN), lists and maps element by
// element, a node or relationship by its ID, and null as its own value. Every
// part is length-prefixed, so no value can imitate a separator.
func groupKey(parts []any) string {
	var b strings.Builder
	for _, p := range parts {
		writeKeyPart(&b, canonicalKey(p))
	}
	return b.String()
}

func writeKeyPart(b *strings.Builder, s string) {
	fmt.Fprintf(b, "%d:%s|", len(s), s)
}

func canonicalKey(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return fmt.Sprintf("bool:%t", x)
	case string:
		return "str:" + x
	case int:
		return "num:" + strconv.FormatInt(int64(x), 10)
	case int64:
		return "num:" + strconv.FormatInt(x, 10)
	case int32:
		return "num:" + strconv.FormatInt(int64(x), 10)
	case float32:
		return canonicalFloat(float64(x))
	case float64:
		return canonicalFloat(x)
	case *storage.Node:
		return fmt.Sprintf("node:%d", x.ID)
	case *storage.Edge:
		return fmt.Sprintf("edge:%d", x.ID)
	case []*storage.Edge:
		var b strings.Builder
		for _, e := range x {
			writeKeyPart(&b, fmt.Sprintf("edge:%d", e.ID))
		}
		return "path[" + b.String() + "]"
	case []any:
		var b strings.Builder
		for _, e := range x {
			writeKeyPart(&b, canonicalKey(e))
		}
		return "list[" + b.String() + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			writeKeyPart(&b, k)
			writeKeyPart(&b, canonicalKey(x[k]))
		}
		return "map{" + b.String() + "}"
	}
	return fmt.Sprintf("%T:%v", v, v)
}

// canonicalFloat writes an integral float the way the same integer is
// written, so 1.0 groups with 1; -0 groups with 0, and every NaN with NaN.
func canonicalFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "num:NaN"
	case f == 0:
		return "num:0"
	case f == math.Trunc(f) && math.Abs(f) < 1<<63:
		return "num:" + strconv.FormatInt(int64(f), 10)
	}
	return "num:" + strconv.FormatFloat(f, 'g', -1, 64)
}

// buildRegularResultSet builds results for regular (non-aggregate) queries
func (e *Executor) buildRegularResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) (*ResultSet, error) {
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
				name := exprColumnName(ob.Expression)
				if !colSet[name] {
					extraSortCols = append(extraSortCols, name)
					colSet[name] = true
				}
			}
		}
	}

	// An expression has no column name, so its value goes in the row under a
	// key that no query can write (it starts with NUL) and leaves with the
	// other sort columns.
	exprOrder := hasOrderByExpression(returnClause.OrderBy)
	if exprOrder {
		for i, ob := range returnClause.OrderBy {
			if ob.ValueExpr != nil {
				extraSortCols = append(extraSortCols, orderBySortKey(i))
			}
		}
	}

	// Build rows with extra sort columns injected from bindings
	computer := &AggregationComputer{}
	for _, binding := range ctx.results {
		row, err := e.buildRow(binding, returnClause.Items, resultSet.Columns, computer)
		if err != nil {
			return nil, err
		}
		if exprOrder {
			if err := addOrderByExpressionValues(row, binding, returnClause); err != nil {
				return nil, err
			}
		}
		for _, ob := range returnClause.OrderBy {
			if ob.Expression != nil {
				name := exprColumnName(ob.Expression)
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

	return resultSet, nil
}

// addOrderByExpressionValues evaluates each ORDER BY expression once for the
// row and stores the value under its sort key. The expression reads the
// variables of the binding and the aliases of RETURN. An alias wins over a
// variable of the same name, as in openCypher: after RETURN p.age AS p, the
// name p in ORDER BY means the age.
func addOrderByExpressionValues(row map[string]any, binding *BindingSet, rc *ReturnClause) error {
	scope := make(map[string]any, len(binding.bindings)+len(rc.Items))
	for name, v := range binding.bindings {
		scope[name] = v
	}
	for _, item := range rc.Items {
		if item.Alias != "" {
			scope[item.Alias] = row[item.Alias]
		}
	}
	for i, ob := range rc.OrderBy {
		if ob.ValueExpr == nil {
			continue
		}
		v, err := evalOperand(ob.ValueExpr, scope)
		if err != nil {
			return err
		}
		row[orderBySortKey(i)] = v
	}
	return nil
}

// buildRow builds a single result row from a binding
func (e *Executor) buildRow(binding *BindingSet, items []*ReturnItem, columns []string, computer *AggregationComputer) (map[string]any, error) {
	row := make(map[string]any)

	for i, item := range items {
		columnName := columns[i]

		// ValueExpr takes precedence (e.g. function calls)
		if item.ValueExpr != nil {
			v, err := evalOperand(item.ValueExpr, binding.bindings)
			if err != nil {
				return nil, err
			}
			row[columnName] = v
			continue
		}

		// Extract value - check for nil Expression first
		if item.Expression != nil {
			row[columnName] = e.extractValueFromBinding(binding, item.Expression, computer)
		}
	}

	return row, nil
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
