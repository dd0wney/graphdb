package query

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fillNonZero sets each field of the struct that v points to, except those in
// skip, to a non-zero value. It panics on a kind it does not know, so a future
// field of a new kind forces this test to be taught about it.
func fillNonZero(t *testing.T, v reflect.Value, skip map[string]bool) {
	t.Helper()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if skip[f.Name] {
			continue
		}
		fv := v.Field(i)
		switch f.Type.Kind() {
		case reflect.String:
			fv.SetString("x")
		case reflect.Bool:
			fv.SetBool(true)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fv.SetInt(3)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fv.SetUint(3)
		case reflect.Float32, reflect.Float64:
			fv.SetFloat(3)
		case reflect.Slice:
			s := reflect.MakeSlice(f.Type, 1, 1)
			if f.Type.Elem().Kind() == reflect.String {
				s.Index(0).SetString("x")
			}
			fv.Set(s)
		case reflect.Map:
			m := reflect.MakeMap(f.Type)
			fv.Set(m)
		default:
			t.Fatalf("fillNonZero: field %s.%s has kind %s; teach the test how to fill it",
				typ.Name(), f.Name, f.Type.Kind())
		}
	}
}

// droppedFields names the fields of orig that are not equal in cp, except
// those in holders, which the copy replaces on purpose.
func droppedFields(orig, cp any, holders map[string]bool) []string {
	ov, cv := reflect.ValueOf(orig).Elem(), reflect.ValueOf(cp).Elem()
	var dropped []string
	for i := 0; i < ov.NumField(); i++ {
		name := ov.Type().Field(i).Name
		if holders[name] {
			continue
		}
		if !reflect.DeepEqual(ov.Field(i).Interface(), cv.Field(i).Interface()) {
			dropped = append(dropped, name)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// The detector itself: a copy that forgets a field must be reported. Without
// this, a detector that always says "nothing dropped" would pass below.
func TestDroppedFields_ReportsAForgottenField(t *testing.T) {
	type thing struct {
		Keep  []*Pattern
		Label string
		N     int
	}
	orig := &thing{Keep: []*Pattern{{}}, Label: "x", N: 3}
	forgetful := &thing{Keep: orig.Keep} // what &T{Keep: ...} does
	got := droppedFields(orig, forgetful, map[string]bool{"Keep": true})
	if strings.Join(got, ",") != "Label,N" {
		t.Errorf("droppedFields = %v, want [Label N]", got)
	}
	if got := droppedFields(orig, &thing{Keep: nil, Label: "x", N: 3}, map[string]bool{"Keep": true}); len(got) != 0 {
		t.Errorf("droppedFields = %v, want none", got)
	}
}

// mapQueryPatterns and resolvePatternParams build new MatchClause,
// CreateClause and Pattern values. A field added to one of those types later
// must survive the copy.
func TestPatternParamsCopy_KeepsEveryField(t *testing.T) {
	identity := func(p *Pattern) (*Pattern, error) { return p, nil }

	t.Run("MatchClause", func(t *testing.T) {
		m := &MatchClause{}
		fillNonZero(t, reflect.ValueOf(m).Elem(), map[string]bool{"Patterns": true})
		m.Patterns = []*Pattern{{}}
		cp, err := mapQueryPatterns(&Query{Match: m}, identity)
		if err != nil {
			t.Fatal(err)
		}
		if d := droppedFields(m, cp.Match, map[string]bool{"Patterns": true}); len(d) > 0 {
			t.Errorf("the copy dropped %v", d)
		}
	})
	t.Run("CreateClause", func(t *testing.T) {
		c := &CreateClause{}
		fillNonZero(t, reflect.ValueOf(c).Elem(), map[string]bool{"Patterns": true})
		c.Patterns = []*Pattern{{}}
		cp, err := mapQueryPatterns(&Query{Create: c}, identity)
		if err != nil {
			t.Fatal(err)
		}
		if d := droppedFields(c, cp.Create, map[string]bool{"Patterns": true}); len(d) > 0 {
			t.Errorf("the copy dropped %v", d)
		}
	})
	t.Run("Pattern", func(t *testing.T) {
		p := &Pattern{}
		fillNonZero(t, reflect.ValueOf(p).Elem(), map[string]bool{"Nodes": true, "Relationships": true})
		cp, err := resolvePatternParams(p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if d := droppedFields(p, cp, map[string]bool{"Nodes": true, "Relationships": true}); len(d) > 0 {
			t.Errorf("the copy dropped %v", d)
		}
	})
}

// EXPLAIN builds a plan and runs nothing, so it needs no parameter value.
func TestExplain_NeedsNoParameterValues(t *testing.T) {
	for _, text := range []string{
		`EXPLAIN MATCH (p:Person {id: $v}) RETURN p.name AS n`,
		`EXPLAIN MATCH (p:Person) WHERE p.id = $v RETURN p.name AS n`,
		`EXPLAIN OPTIONAL MATCH (p:Person {id: $v}) RETURN p.name AS n`,
		`EXPLAIN MATCH (a:Person) WITH a MATCH (b:Person {id: $v}) RETURN b.name AS n`,
	} {
		t.Run(text, func(t *testing.T) {
			res, err := patternParamsStore(t).Execute(parsePatternQuery(t, text))
			if err != nil {
				t.Fatalf("EXPLAIN with no parameter values: %v", err)
			}
			if res.Count == 0 || len(res.Columns) != 2 || res.Columns[0] != "step" {
				t.Errorf("not a plan: columns=%v rows=%v", res.Columns, res.Rows)
			}
		})
	}
}

// The EXPLAIN exemption must not let a step run: a write in an EXPLAIN query
// with an unresolved parameter creates nothing.
func TestExplain_WithUnresolvedParameterRunsNoStep(t *testing.T) {
	ex := patternParamsStore(t)
	res, err := ex.Execute(parsePatternQuery(t, `EXPLAIN CREATE (p:Made {id: $v})`))
	if err != nil {
		t.Fatalf("EXPLAIN CREATE: %v", err)
	}
	if res.Columns[0] != "step" {
		t.Errorf("not a plan: %v", res.Columns)
	}
	nodes, err := ex.graph.GetNodesByLabelForTenant("default", "Made")
	if err != nil {
		t.Fatalf("GetNodesByLabelForTenant: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("EXPLAIN created %d node(s)", len(nodes))
	}
}

// EXPLAIN of a UNION runs the second part today (executeUnion), so it keeps
// the parameter check: a plan is not what it would return.
func TestExplain_UnionWithUnresolvedParameterIsRefused(t *testing.T) {
	_, err := patternParamsStore(t).Execute(parsePatternQuery(t,
		`EXPLAIN MATCH (p:Person {id: 1}) RETURN p.name AS n UNION MATCH (q:Person {id: $v}) RETURN q.name AS n`))
	if err == nil || !strings.Contains(err.Error(), "$v") {
		t.Errorf("err = %v, want missing parameter: $v", err)
	}
}

// PROFILE runs the query, so it keeps the check.
func TestProfile_WithUnresolvedParameterIsRefused(t *testing.T) {
	for _, text := range []string{
		`PROFILE MATCH (p:Person {id: $v}) RETURN p.name AS n`,
	} {
		_, err := patternParamsStore(t).Execute(parsePatternQuery(t, text))
		if err == nil || !strings.Contains(err.Error(), "$v") {
			t.Errorf("%q: err = %v, want missing parameter: $v", text, err)
		}
	}
}

// createEndpoint finds the endpoints of a created relationship by pointer. If
// resolution split a relationship's From/To from the pattern's nodes, CREATE
// would make extra nodes or no edge. Anonymous nodes have no variable to fall
// back on.
func TestCreate_AnonymousEndpointsWithParameterKeepIdentity(t *testing.T) {
	ex := patternParamsStore(t)
	_, err := ex.ExecuteWithParams(parsePatternQuery(t, `CREATE (:A {id: $v})-[:R]->(:B)`),
		map[string]any{"v": int64(1)})
	if err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	as, _ := ex.graph.GetNodesByLabelForTenant("default", "A")
	bs, _ := ex.graph.GetNodesByLabelForTenant("default", "B")
	if len(as) != 1 || len(bs) != 1 {
		t.Fatalf("A nodes = %d, B nodes = %d, want 1 and 1", len(as), len(bs))
	}
	edges := ex.graph.GetEdgesByTypeForTenant("default", "R")
	if len(edges) != 1 {
		t.Fatalf("R edges = %d, want 1", len(edges))
	}
	if edges[0].FromNodeID != as[0].ID || edges[0].ToNodeID != bs[0].ID {
		t.Errorf("edge %d->%d, want %d->%d", edges[0].FromNodeID, edges[0].ToNodeID, as[0].ID, bs[0].ID)
	}
}
