package query

import (
	"strings"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Row expressions in pattern property maps ({id: item.id}).
//
// A *ParameterRef is resolved once, in a copy of the query, before the first
// row (pattern_params.go). A *RowExprRef reads a variable, so it has one value
// for each row. Each step that reads a pattern map calls resolveRowProps with
// the row it works on and gets a map of plain values back, which the code
// below it already knows how to use. No step reads a *RowExprRef itself.

// hasRowExpr reports whether props holds a *RowExprRef.
func hasRowExpr(props map[string]any) bool {
	for _, v := range props {
		if _, ok := v.(*RowExprRef); ok {
			return true
		}
	}
	return false
}

// resolveRowProps returns props with each *RowExprRef replaced by its value for
// row. A map without one is returned as it is, so the common pattern costs no
// allocation. props is never written: the same parsed pattern serves every row.
func resolveRowProps(props map[string]any, row *BindingSet) (map[string]any, error) {
	if !hasRowExpr(props) {
		return props, nil
	}
	resolved := make(map[string]any, len(props))
	for key, v := range props {
		ref, ok := v.(*RowExprRef)
		if !ok {
			resolved[key] = v
			continue
		}
		val, err := evalRowExpr(ref, row)
		if err != nil {
			return nil, err
		}
		resolved[key] = val
	}
	return resolved, nil
}

// evalRowExpr calculates one row expression against the bindings of row.
//
// A variable that the row does not bind is refused. evalOperand reads an unset
// name as null, and null in a pattern map matches nothing: a pattern such as
// (a)-[:R]->(b {id: a.id}), which names a variable the same pattern binds,
// would answer with no rows and no error.
func evalRowExpr(ref *RowExprRef, row *BindingSet) (any, error) {
	var bindings map[string]any
	if row != nil {
		bindings = row.bindings
	}
	var unbound string
	walkExpression("", ref.Expr, func(_ string, e Expression) {
		pe, ok := e.(*PropertyExpression)
		if !ok || unbound != "" || pe.Variable == "" || strings.HasPrefix(pe.Variable, "$") {
			return
		}
		if _, bound := bindings[pe.Variable]; !bound {
			unbound = pe.Variable
		}
	})
	if unbound != "" {
		return nil, refusef("variable %s is not bound where the pattern property map uses it", unbound)
	}

	val, err := evalOperand(ref.Expr, bindings)
	if err != nil {
		return nil, err
	}
	switch val.(type) {
	case *storage.Node, *storage.Edge:
		return nil, refusef("a node or relationship cannot be a property value")
	}
	return val, nil
}

// patternWithRowProps returns pattern with each row expression of its node and
// relationship maps calculated for row. A pattern with none is returned as it
// is. The copy keeps From and To pointing at the copied nodes of the same
// pattern, as resolvePatternParams does.
func patternWithRowProps(pattern *Pattern, row *BindingSet) (*Pattern, error) {
	if !patternHasRowExpr(pattern) {
		return pattern, nil
	}
	copies := make(map[*NodePattern]*NodePattern, len(pattern.Nodes))
	node := func(n *NodePattern) (*NodePattern, error) {
		if n == nil {
			return nil, nil
		}
		if done, ok := copies[n]; ok {
			return done, nil
		}
		props, err := resolveRowProps(n.Properties, row)
		if err != nil {
			return nil, err
		}
		cp := *n
		cp.Properties = props
		copies[n] = &cp
		return &cp, nil
	}

	out := *pattern
	out.Nodes = make([]*NodePattern, len(pattern.Nodes))
	for i, n := range pattern.Nodes {
		var err error
		if out.Nodes[i], err = node(n); err != nil {
			return nil, err
		}
	}
	out.Relationships = make([]*RelationshipPattern, len(pattern.Relationships))
	for i, rel := range pattern.Relationships {
		if rel == nil {
			continue
		}
		props, err := resolveRowProps(rel.Properties, row)
		if err != nil {
			return nil, err
		}
		cp := *rel
		cp.Properties = props
		if cp.From, err = node(rel.From); err != nil {
			return nil, err
		}
		if cp.To, err = node(rel.To); err != nil {
			return nil, err
		}
		out.Relationships[i] = &cp
	}
	return &out, nil
}

func patternHasRowExpr(pattern *Pattern) bool {
	for _, n := range pattern.Nodes {
		if n != nil && hasRowExpr(n.Properties) {
			return true
		}
	}
	for _, r := range pattern.Relationships {
		if r != nil && hasRowExpr(r.Properties) {
			return true
		}
	}
	return false
}

// refuseNullMergeValues refuses a MERGE whose property map calculates to null
// for any of rows, as openCypher does: a node with a null key cannot be found
// again, so each run would add another.
//
// MergeStep runs it over every row before the first write, so a null in the
// last row of a batch does not leave the earlier rows written.
func refuseNullMergeValues(pattern *Pattern, rows []*BindingSet) error {
	if pattern == nil || !patternHasRowExpr(pattern) {
		return nil
	}
	for _, row := range rows {
		for _, n := range pattern.Nodes {
			if n == nil {
				continue
			}
			if err := refuseNullRowValue("node", n.Properties, row); err != nil {
				return err
			}
		}
		for _, r := range pattern.Relationships {
			if r == nil {
				continue
			}
			if err := refuseNullRowValue("relationship", r.Properties, row); err != nil {
				return err
			}
		}
	}
	return nil
}

// refuseNullRowValue refuses props when a row expression in it is null for row.
func refuseNullRowValue(kind string, props map[string]any, row *BindingSet) error {
	for key, v := range props {
		ref, ok := v.(*RowExprRef)
		if !ok {
			continue
		}
		val, err := evalRowExpr(ref, row)
		if err != nil {
			return err
		}
		if val == nil {
			return refusef("MERGE: Cannot merge %s using null property value for %s", kind, key)
		}
	}
	return nil
}
