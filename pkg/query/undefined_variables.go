package query

import (
	"fmt"
	"strings"
)

// segmentScope is the set of names a query segment can refer to.
type segmentScope struct {
	names map[string]struct{}
	// unknown is true when the names cannot be listed before the query runs.
	// A check then refuses nothing in the segment, because a refusal would be
	// a guess and a false refusal breaks a query that works today.
	unknown bool
	// callColumns is true when the segment has a CALL with no YIELD. Its
	// columns are known only when it runs, so every clause but the CALL
	// arguments may use a name that is not in the set. The arguments run
	// before the procedure and cannot.
	callColumns bool
}

func (s *segmentScope) add(name string) {
	if name == "" {
		return
	}
	s.names[name] = struct{}{}
}

func (s *segmentScope) has(name string) bool {
	_, ok := s.names[name]
	return ok
}

// checkUndefinedVariables refuses a reference to a variable that no clause of
// its segment bound. It reads the parsed query only, so the answer is the same
// for zero rows as for a million, and it runs before the first segment, which
// may write.
//
// Before this check an unbound name read as null: RETURN q.name gave a column
// of nulls, SET q.x = 1 wrote nothing, and CALL algo.x(nosuch) ran with its
// defaults.
//
// Expression nodes carry no source position, so the error names the variable
// and the clause but has no line and column.
func checkUndefinedVariables(q *Query) error {
	return checkChainVariables(q)
}

// checkChainVariables checks one WITH chain: a segment, then the segment after
// its WITH, and so on. Each UNION side starts a chain of its own.
func checkChainVariables(head *Query) error {
	scope := initialScope(head)
	for seg := head; seg != nil; seg = seg.Next {
		scope = bindSegment(scope, seg)
		if err := checkSegmentVariables(seg, scope); err != nil {
			return err
		}
		if seg.UnionNext != nil {
			// The other side of this UNION is a chain with its own start.
			if err := checkChainVariables(seg.UnionNext); err != nil {
				return err
			}
		}
		if seg.With == nil {
			return nil
		}
		scope = scopeAfterWith(seg.With)
	}
	return nil
}

// initialScope holds the names the caller bound before the first clause. A
// parameter is "$name" in a binding and is not a variable.
func initialScope(head *Query) *segmentScope {
	scope := &segmentScope{names: map[string]struct{}{}}
	if head.InitialBindings == nil {
		return scope
	}
	// The caller said it has bindings but not which, so no name is certain
	// to be missing.
	if len(head.InitialBindings) == 0 {
		scope.unknown = true
		return scope
	}
	for _, b := range head.InitialBindings {
		if b == nil {
			continue
		}
		for name := range b.bindings {
			if !strings.HasPrefix(name, "$") {
				scope.add(name)
			}
		}
	}
	return scope
}

// scopeAfterWith is the scope of the segment that follows a WITH. Nothing but
// the names the WITH binds crosses it.
func scopeAfterWith(with *WithClause) *segmentScope {
	scope := &segmentScope{names: map[string]struct{}{}}
	for _, item := range with.Items {
		scope.add(withBoundName(item))
	}
	return scope
}

// withBoundName is the name projectWithBinding binds a WITH item under: the
// alias, or the variable of a property reference or a bare variable. An
// aggregate has only its alias.
func withBoundName(item *ReturnItem) string {
	if item.Alias != "" {
		return item.Alias
	}
	if item.Aggregate == "" && item.Expression != nil {
		return item.Expression.Variable
	}
	return ""
}

// bindSegment returns the scope of seg: what it inherits plus what its own
// clauses bind. The executor runs the clauses in a fixed order that differs
// from the text order, so the scope is one set for the whole segment.
func bindSegment(inherited *segmentScope, seg *Query) *segmentScope {
	scope := &segmentScope{names: make(map[string]struct{}, len(inherited.names)), unknown: inherited.unknown, callColumns: inherited.callColumns}
	for name := range inherited.names {
		scope.names[name] = struct{}{}
	}
	if seg.Match != nil {
		bindPatterns(scope, seg.Match.Patterns)
	}
	for _, om := range seg.OptionalMatches {
		bindPatterns(scope, om.Patterns)
	}
	for _, m := range seg.Merges {
		if m.Pattern != nil {
			bindPatterns(scope, []*Pattern{m.Pattern})
		}
	}
	if seg.Create != nil {
		bindPatterns(scope, seg.Create.Patterns)
	}
	if seg.Unwind != nil {
		scope.add(seg.Unwind.Alias)
	}
	if seg.Call != nil {
		if len(seg.Call.YieldItems) == 0 {
			// The columns of a procedure are known only when it runs.
			scope.callColumns = true
		}
		for _, y := range seg.Call.YieldItems {
			scope.add(y)
		}
	}
	return scope
}

func bindPatterns(scope *segmentScope, patterns []*Pattern) {
	for _, p := range patterns {
		if p == nil {
			continue
		}
		for _, n := range p.Nodes {
			if n != nil {
				scope.add(n.Variable)
			}
		}
		for _, r := range p.Relationships {
			if r != nil {
				scope.add(r.Variable)
			}
		}
	}
}

// checkSegmentVariables refuses the first reference in seg to a name that
// scope lacks. Two clauses read a different set:
//   - the WHERE after a WITH sees the names that the WITH binds
//   - ORDER BY also sees the aliases of the RETURN of its segment
func checkSegmentVariables(seg *Query, scope *segmentScope) error {
	whereScope := &segmentScope{names: map[string]struct{}{}}
	if seg.With != nil {
		whereScope = scopeAfterWith(seg.With)
	}

	orderScope := &segmentScope{names: scope.names, unknown: scope.unknown, callColumns: scope.callColumns}
	if seg.Return != nil {
		orderScope = &segmentScope{names: make(map[string]struct{}, len(scope.names)), unknown: scope.unknown, callColumns: scope.callColumns}
		for name := range scope.names {
			orderScope.names[name] = struct{}{}
		}
		for _, item := range seg.Return.Items {
			orderScope.add(item.Alias)
		}
	}

	var found error
	walkSegmentExpressions(seg, func(clause string, expr Expression) {
		if found != nil {
			return
		}
		pe, ok := expr.(*PropertyExpression)
		if !ok || pe.Variable == "" || strings.HasPrefix(pe.Variable, "$") {
			return
		}
		use := scope
		switch clause {
		case clauseWithWhere:
			use = whereScope
		case clauseOrderBy:
			use = orderScope
		}
		if use.unknown || (use.callColumns && clause != clauseCall) || use.has(pe.Variable) {
			return
		}
		found = fmt.Errorf("undefined variable: %s (in %s)", pe.Variable, clause)
	})
	return found
}
