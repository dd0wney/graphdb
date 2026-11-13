package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// refPattern is a pattern with a parameter in a node and in a relationship.
func refPattern(name string) *Pattern {
	a := &NodePattern{Variable: "a", Properties: map[string]any{"id": &ParameterRef{Name: name}}}
	b := &NodePattern{Variable: "b"}
	return &Pattern{
		Nodes: []*NodePattern{a, b},
		Relationships: []*RelationshipPattern{{
			Type: "KNOWS", From: a, To: b,
			Properties: map[string]any{"w": &ParameterRef{Name: name}},
		}},
	}
}

// handMadeQueries puts a parameter in one place each, with no parser: the
// places are the ones that ast.go lets hold a pattern.
func handMadeQueries() map[string]*Query {
	return map[string]*Query{
		"Match":          {Match: &MatchClause{Patterns: []*Pattern{refPattern("p")}}},
		"OptionalMatch":  {OptionalMatches: []*OptionalMatchClause{{Patterns: []*Pattern{refPattern("p")}}}},
		"Create":         {Create: &CreateClause{Patterns: []*Pattern{refPattern("p")}}},
		"Merge":          {Merges: []*MergeClause{{Pattern: refPattern("p")}}},
		"Next":           {Next: &Query{Match: &MatchClause{Patterns: []*Pattern{refPattern("p")}}}},
		"NextOptional":   {Next: &Query{OptionalMatches: []*OptionalMatchClause{{Patterns: []*Pattern{refPattern("p")}}}}},
		"NextNext":       {Next: &Query{Next: &Query{Merges: []*MergeClause{{Pattern: refPattern("p")}}}}},
		"UnionNext":      {UnionNext: &Query{Match: &MatchClause{Patterns: []*Pattern{refPattern("p")}}}},
		"UnionNextNext":  {UnionNext: &Query{UnionNext: &Query{OptionalMatches: []*OptionalMatchClause{{Patterns: []*Pattern{refPattern("p")}}}}}},
		"UnionOfNext":    {UnionNext: &Query{Next: &Query{Create: &CreateClause{Patterns: []*Pattern{refPattern("p")}}}}},
		"NextOfUnionArm": {Next: &Query{UnionNext: &Query{Match: &MatchClause{Patterns: []*Pattern{refPattern("p")}}}}},
	}
}

// The check is the loud half of the fix: a parameter that a place keeps is
// refused and named, whichever place it is.
func TestCheckNoUnresolvedPatternParams_FindsEachPlace(t *testing.T) {
	for name, q := range handMadeQueries() {
		t.Run(name, func(t *testing.T) {
			err := validateParameterExpressions(q, nil)
			if err == nil {
				t.Fatalf("a parameter in %s was not found", name)
			}
			if !strings.Contains(err.Error(), "$p") {
				t.Errorf("error does not name the parameter: %v", err)
			}
		})
	}
}

// The resolver and the check come from two traversals. For each place, the
// resolver must leave nothing for the check to find, and must not write into
// the original.
func TestResolveParameters_LeavesNothingAndKeepsOriginal(t *testing.T) {
	for name, q := range handMadeQueries() {
		t.Run(name, func(t *testing.T) {
			resolved, err := resolveParameters(q, map[string]any{"p": int64(5)}, false)
			if err != nil {
				t.Fatalf("resolveParameters: %v", err)
			}
			if err := validateParameterExpressions(resolved, map[string]any{"p": int64(5)}); err != nil {
				t.Errorf("resolveParameters left a parameter: %v", err)
			}
			if err := validateParameterExpressions(q, map[string]any{"p": int64(5)}); err == nil {
				t.Errorf("resolveParameters wrote into the original query")
			}
		})
	}
}

func TestResolveParameters_MissingParameterNamesItInEachPlace(t *testing.T) {
	for name, q := range handMadeQueries() {
		t.Run(name, func(t *testing.T) {
			_, err := resolveParameters(q, map[string]any{}, false)
			if err == nil || !strings.Contains(err.Error(), "missing parameter: $p") {
				t.Errorf("error = %v, want missing parameter: $p", err)
			}
		})
	}
}

// injectParams runs the check on the resolved copy. A resolver that forgot a
// place would return a copy that still holds the reference, and the check
// must turn that into an error. mapQueryPatterns is given a mapper that
// forgets, which stands for a future clause that the resolver does not cover.
func TestInjectParamsCheckCatchesAForgottenPlace(t *testing.T) {
	q := handMadeQueries()["Merge"]
	forgotten, err := mapQueryPatterns(q, func(p *Pattern) (*Pattern, error) { return p, nil })
	if err != nil {
		t.Fatalf("mapQueryPatterns: %v", err)
	}
	// Even with the name supplied, a parameter that a pattern still holds is refused.
	err = validateParameterExpressions(forgotten, map[string]any{"p": int64(5)})
	if err == nil || !strings.Contains(err.Error(), "$p") {
		t.Errorf("a pattern parameter that the resolver forgot was not refused: %v", err)
	}
}

// A relationship's From and To are the nodes of the same pattern. A copy that
// split them would make a later filter read the unresolved node.
func TestResolvePatternParams_KeepsNodeIdentity(t *testing.T) {
	orig := refPattern("p")
	got, err := resolvePatternParams(orig, map[string]any{"p": int64(5)}, false)
	if err != nil {
		t.Fatalf("resolvePatternParams: %v", err)
	}
	rel := got.Relationships[0]
	if rel.From != got.Nodes[0] || rel.To != got.Nodes[1] {
		t.Errorf("From/To no longer point at the nodes of the copied pattern")
	}
	if got.Nodes[0] == orig.Nodes[0] {
		t.Errorf("the node was not copied")
	}
	if got.Nodes[0].Properties["id"] != int64(5) || rel.Properties["w"] != int64(5) {
		t.Errorf("values not resolved: node=%v rel=%v", got.Nodes[0].Properties, rel.Properties)
	}
	if _, ok := orig.Nodes[0].Properties["id"].(*ParameterRef); !ok {
		t.Errorf("the original node lost its ParameterRef")
	}
}

// Every field of the AST that holds a *Pattern must be one that
// mapQueryPatterns copies and walkQueryExpressions walks. A new clause with a
// pattern fails here, with the two places to update.
func TestPatternHoldingFieldsAreKnown(t *testing.T) {
	patternType := reflect.TypeOf(Pattern{})
	seen := map[reflect.Type]bool{}
	var found []string
	var visit func(t reflect.Type)
	visit = func(typ reflect.Type) {
		for typ.Kind() == reflect.Ptr || typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] || typ == patternType {
			return
		}
		seen[typ] = true
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			elem := f.Type
			for elem.Kind() == reflect.Ptr || elem.Kind() == reflect.Slice {
				elem = elem.Elem()
			}
			if elem == patternType {
				found = append(found, typ.Name()+"."+f.Name)
			}
			visit(f.Type)
		}
	}
	visit(reflect.TypeOf(Query{}))
	sort.Strings(found)

	want := []string{
		"CreateClause.Patterns",
		"MatchClause.Patterns",
		"MergeClause.Pattern",
		"OptionalMatchClause.Patterns",
	}
	if !reflect.DeepEqual(found, want) {
		t.Errorf("pattern-holding fields = %v, want %v.\n"+
			"Update mapQueryPatterns (pattern_params.go) and walkSegmentExpressions "+
			"(walk_expressions.go) for the new field, then this list.", found, want)
	}
}
