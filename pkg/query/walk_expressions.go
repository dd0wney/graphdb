package query

// Clause names that walkQueryExpressions passes to its visitor. A refusal
// names the clause, so the strings are part of the error text.
const (
	clauseMatch         = "MATCH"
	clauseWhere         = "WHERE"
	clauseOptionalMatch = "OPTIONAL MATCH"
	clauseOptionalWhere = "OPTIONAL MATCH WHERE"
	clauseMerge         = "MERGE"
	clauseOnCreateSet   = "ON CREATE SET"
	clauseOnMatchSet    = "ON MATCH SET"
	clauseCreate        = "CREATE"
	clauseSet           = "SET"
	clauseRemove        = "REMOVE"
	clauseDelete        = "DELETE"
	clauseUnwind        = "UNWIND"
	clauseCall          = "CALL"
	clauseWith          = "WITH"
	clauseWithWhere     = "WITH WHERE"
	clauseReturn        = "RETURN"
	clauseGroupBy       = "GROUP BY"
	clauseOrderBy       = "ORDER BY"
)

// walkQueryExpressions calls visit for every expression of the whole
// statement: this segment, each later segment of the WITH chain (Next), and
// both sides of each UNION (UnionNext). It is the one place that knows which
// field of Query and of each clause holds an expression, so a check built on
// it cannot miss a clause that another check covers.
//
// visit gets the clause that holds the expression and the expression. It is
// called for every node, pre-order: a binary expression, then its operands.
// A reference that the AST keeps as a bare string is passed as a
// *PropertyExpression so that a check needs one case for it:
//   - the variable of SET, REMOVE (with its property) and DELETE
//   - a ParameterRef in a pattern property map arrives as a *ParameterExpression
//
// Pattern property maps hold literals and *ParameterRef only (parseValue), so
// they contain no variable. A hand-built AST may put an Expression there, and
// it is visited too.
func walkQueryExpressions(q *Query, visit func(clause string, expr Expression)) {
	for seg := q; seg != nil; seg = seg.Next {
		walkSegmentExpressions(seg, visit)
		if seg.UnionNext != nil {
			walkQueryExpressions(seg.UnionNext, visit)
		}
	}
}

// walkSegmentExpressions visits the expressions of one query segment, which
// ends at its WITH. It does not follow Next or UnionNext.
func walkSegmentExpressions(q *Query, visit func(clause string, expr Expression)) {
	if q.Match != nil {
		walkPatterns(clauseMatch, q.Match.Patterns, visit)
	}
	if q.Where != nil {
		walkExpression(clauseWhere, q.Where.Expression, visit)
	}
	for _, om := range q.OptionalMatches {
		walkPatterns(clauseOptionalMatch, om.Patterns, visit)
		if om.Where != nil {
			walkExpression(clauseOptionalWhere, om.Where.Expression, visit)
		}
	}
	for _, m := range q.Merges {
		if m.Pattern != nil {
			walkPatterns(clauseMerge, []*Pattern{m.Pattern}, visit)
		}
		walkSet(clauseOnCreateSet, m.OnCreate, visit)
		walkSet(clauseOnMatchSet, m.OnMatch, visit)
	}
	if q.Create != nil {
		walkPatterns(clauseCreate, q.Create.Patterns, visit)
	}
	walkSet(clauseSet, q.Set, visit)
	if q.Remove != nil {
		for _, item := range q.Remove.Items {
			walkVariable(clauseRemove, item.Variable, item.Property, visit)
		}
	}
	if q.Delete != nil {
		for _, v := range q.Delete.Variables {
			walkVariable(clauseDelete, v, "", visit)
		}
	}
	if q.Unwind != nil && q.Unwind.Expression != nil {
		walkExpression(clauseUnwind, q.Unwind.Expression, visit)
	}
	if q.Call != nil {
		for _, arg := range q.Call.Arguments {
			walkExpression(clauseCall, arg, visit)
		}
	}
	if q.With != nil {
		walkReturnItems(clauseWith, q.With.Items, visit)
		if q.With.Where != nil {
			walkExpression(clauseWithWhere, q.With.Where.Expression, visit)
		}
	}
	if q.Return != nil {
		walkReturnItems(clauseReturn, q.Return.Items, visit)
		for _, g := range q.Return.GroupBy {
			walkExpression(clauseGroupBy, g, visit)
		}
		for _, ob := range q.Return.OrderBy {
			// ValueExpr takes precedence over Expression, as in the executor.
			if ob.ValueExpr != nil {
				walkExpression(clauseOrderBy, ob.ValueExpr, visit)
			} else if ob.Expression != nil {
				walkExpression(clauseOrderBy, ob.Expression, visit)
			}
		}
	}
}

func walkSet(clause string, s *SetClause, visit func(string, Expression)) {
	if s == nil {
		return
	}
	for _, a := range s.Assignments {
		walkVariable(clause, a.Variable, a.Property, visit)
		if a.ValueExpr != nil {
			walkExpression(clause, a.ValueExpr, visit)
			continue
		}
		walkValue(clause, a.Value, visit)
	}
}

func walkVariable(clause, variable, property string, visit func(string, Expression)) {
	visit(clause, &PropertyExpression{Variable: variable, Property: property})
}

func walkReturnItems(clause string, items []*ReturnItem, visit func(string, Expression)) {
	for _, item := range items {
		// ValueExpr takes precedence over Expression, as in the executor. An
		// aggregate keeps its argument in Expression and its name in
		// Aggregate: it is not a FunctionCallExpression.
		if item.ValueExpr != nil {
			walkExpression(clause, item.ValueExpr, visit)
		} else if item.Expression != nil {
			walkExpression(clause, item.Expression, visit)
		}
	}
}

func walkPatterns(clause string, patterns []*Pattern, visit func(string, Expression)) {
	for _, p := range patterns {
		if p == nil {
			continue
		}
		for _, n := range p.Nodes {
			if n != nil {
				walkProperties(clause, n.Properties, visit)
			}
		}
		for _, r := range p.Relationships {
			if r != nil {
				walkProperties(clause, r.Properties, visit)
			}
		}
	}
}

func walkProperties(clause string, props map[string]any, visit func(string, Expression)) {
	for _, v := range props {
		walkValue(clause, v, visit)
	}
}

// walkValue visits what a property map or a literal holds: an Expression, a
// parameter reference, or a list or map that holds either.
func walkValue(clause string, v any, visit func(string, Expression)) {
	switch val := v.(type) {
	case Expression:
		walkExpression(clause, val, visit)
	case *ParameterRef:
		visit(clause, &ParameterExpression{Name: val.Name})
	case []any:
		for _, item := range val {
			walkValue(clause, item, visit)
		}
	case map[string]any:
		for _, item := range val {
			walkValue(clause, item, visit)
		}
	}
}

// walkExpression visits expr and every expression inside it. The switch names
// each Expression implementation in this package; a new one must get a case
// here, and TestWalkExpressionsCoversEachImplementation fails until it does.
func walkExpression(clause string, expr Expression, visit func(string, Expression)) {
	if expr == nil {
		return
	}
	visit(clause, expr)
	switch e := expr.(type) {
	case *BinaryExpression:
		walkExpression(clause, e.Left, visit)
		walkExpression(clause, e.Right, visit)
	case *ArithmeticExpression:
		walkExpression(clause, e.Left, visit)
		walkExpression(clause, e.Right, visit)
	case *UnaryExpression:
		walkExpression(clause, e.Operand, visit)
	case *FunctionCallExpression:
		for _, arg := range e.Args {
			walkExpression(clause, arg, visit)
		}
	case *CaseExpression:
		walkExpression(clause, e.Operand, visit)
		for _, wc := range e.WhenClauses {
			walkExpression(clause, wc.Condition, visit)
			walkExpression(clause, wc.Result, visit)
		}
		walkExpression(clause, e.ElseResult, visit)
	case *LiteralExpression:
		// A list literal holds values, not expressions, when the parser built
		// it. A hand-built one may hold expressions.
		walkValue(clause, e.Value, visit)
	case *PropertyExpression, *ParameterExpression:
		// Leaves.
	}
}
