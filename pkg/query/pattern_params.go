package query

// Parameters in pattern property maps ({id: $v}).
//
// The parser stores such a value as a *ParameterRef. Resolution replaces it
// with the caller's value, in a COPY of the query. It never writes into the
// parsed Query: a caller may parse once and run the same *Query with
// different parameters, and a write would make the second run use the first
// run's values.
//
// Two traversals exist on purpose, so that neither can hide a miss in the
// other:
//   - mapQueryPatterns finds the patterns to resolve. It is the only code
//     that names the fields of Query and of each clause that hold a *Pattern.
//   - checkNoUnresolvedPatternParams looks for what is left, through
//     walkQueryExpressions, which knows the clauses from the expression side.
//     A clause that mapQueryPatterns forgets is refused, not run with a
//     parameter that matches nothing.

// resolveParameters returns a copy of query in which each *ParameterRef in a
// pattern is replaced with its value from params. The copy shares everything
// that is not a pattern with the original, which resolution does not touch.
//
// keepMissing leaves a parameter that params does not hold in place and does
// not fail. EXPLAIN uses it: a plan needs no value.
func resolveParameters(query *Query, params map[string]any, keepMissing bool) (*Query, error) {
	return mapQueryPatterns(query, func(p *Pattern) (*Pattern, error) {
		return resolvePatternParams(p, params, keepMissing)
	})
}

// mapQueryPatterns returns a copy of q with each pattern replaced by fn(pattern).
// It covers this segment, each later segment of the WITH chain (Next) and each
// part of a UNION (UnionNext). Only the clauses that hold a pattern are copied.
func mapQueryPatterns(q *Query, fn func(*Pattern) (*Pattern, error)) (*Query, error) {
	if q == nil {
		return nil, nil
	}
	cp := *q

	if q.Match != nil {
		patterns, err := mapPatternList(q.Match.Patterns, fn)
		if err != nil {
			return nil, err
		}
		match := *q.Match
		match.Patterns = patterns
		cp.Match = &match
	}
	if q.Create != nil {
		patterns, err := mapPatternList(q.Create.Patterns, fn)
		if err != nil {
			return nil, err
		}
		create := *q.Create
		create.Patterns = patterns
		cp.Create = &create
	}
	if q.OptionalMatches != nil {
		cp.OptionalMatches = make([]*OptionalMatchClause, len(q.OptionalMatches))
		for i, om := range q.OptionalMatches {
			if om == nil {
				continue
			}
			patterns, err := mapPatternList(om.Patterns, fn)
			if err != nil {
				return nil, err
			}
			omCopy := *om
			omCopy.Patterns = patterns
			cp.OptionalMatches[i] = &omCopy
		}
	}
	if q.Merges != nil {
		cp.Merges = make([]*MergeClause, len(q.Merges))
		for i, m := range q.Merges {
			if m == nil {
				continue
			}
			mCopy := *m
			if m.Pattern != nil {
				p, err := fn(m.Pattern)
				if err != nil {
					return nil, err
				}
				mCopy.Pattern = p
			}
			cp.Merges[i] = &mCopy
		}
	}

	var err error
	if cp.Next, err = mapQueryPatterns(q.Next, fn); err != nil {
		return nil, err
	}
	if cp.UnionNext, err = mapQueryPatterns(q.UnionNext, fn); err != nil {
		return nil, err
	}
	return &cp, nil
}

func mapPatternList(patterns []*Pattern, fn func(*Pattern) (*Pattern, error)) ([]*Pattern, error) {
	if patterns == nil {
		return nil, nil
	}
	out := make([]*Pattern, len(patterns))
	for i, p := range patterns {
		if p == nil {
			continue
		}
		mapped, err := fn(p)
		if err != nil {
			return nil, err
		}
		out[i] = mapped
	}
	return out, nil
}

// resolvePatternParams returns a copy of pattern with its parameters resolved.
// A relationship's From and To point at nodes of the same pattern, and the
// copy keeps that identity.
func resolvePatternParams(pattern *Pattern, params map[string]any, keepMissing bool) (*Pattern, error) {
	copies := make(map[*NodePattern]*NodePattern, len(pattern.Nodes))
	resolveNode := func(n *NodePattern) (*NodePattern, error) {
		if n == nil {
			return nil, nil
		}
		if done, ok := copies[n]; ok {
			return done, nil
		}
		props, err := resolvePropertyParams(n.Properties, params, keepMissing)
		if err != nil {
			return nil, err
		}
		nCopy := *n
		nCopy.Properties = props
		copies[n] = &nCopy
		return &nCopy, nil
	}

	// A copy of the struct value, so a field added to Pattern later is kept.
	out := *pattern
	out.Nodes, out.Relationships = nil, nil
	if pattern.Nodes != nil {
		out.Nodes = make([]*NodePattern, len(pattern.Nodes))
		for i, n := range pattern.Nodes {
			nCopy, err := resolveNode(n)
			if err != nil {
				return nil, err
			}
			out.Nodes[i] = nCopy
		}
	}
	if pattern.Relationships != nil {
		out.Relationships = make([]*RelationshipPattern, len(pattern.Relationships))
		for i, rel := range pattern.Relationships {
			if rel == nil {
				continue
			}
			props, err := resolvePropertyParams(rel.Properties, params, keepMissing)
			if err != nil {
				return nil, err
			}
			relCopy := *rel
			relCopy.Properties = props
			if relCopy.From, err = resolveNode(rel.From); err != nil {
				return nil, err
			}
			if relCopy.To, err = resolveNode(rel.To); err != nil {
				return nil, err
			}
			out.Relationships[i] = &relCopy
		}
	}
	return &out, nil
}

// resolvePropertyParams returns a new map with ParameterRef values replaced by actual values.
// The original map is not modified, making repeated calls with different params safe.
func resolvePropertyParams(props map[string]any, params map[string]any, keepMissing bool) (map[string]any, error) {
	if props == nil {
		return nil, nil
	}
	resolved := make(map[string]any, len(props))
	for key, val := range props {
		ref, ok := val.(*ParameterRef)
		if !ok {
			resolved[key] = val
			continue
		}
		actual, exists := params[ref.Name]
		if !exists && keepMissing {
			resolved[key] = val
			continue
		}
		if !exists {
			// A refusal, so a server answers 400: the name came from the caller.
			return nil, refusef("missing parameter: $%s", ref.Name)
		}
		resolved[key] = actual
	}
	return resolved, nil
}

// patternClauses are the clause names that walkQueryExpressions reports for an
// expression that came out of a pattern property map. No other field of a
// clause is walked under these names (ON CREATE SET and the SET clause have
// their own).
var patternClauses = map[string]bool{
	clauseMatch:         true,
	clauseOptionalMatch: true,
	clauseMerge:         true,
	clauseCreate:        true,
}
