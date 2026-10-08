package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// setEdgeFixture is a -R-> b, with the edge carrying w = 1.
type setEdgeFixture struct {
	gs       *storage.GraphStorage
	a, b     *storage.Node
	ab       *storage.Edge
	executor *Executor
}

func newSetEdgeFixture(t *testing.T) *setEdgeFixture {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)

	f := &setEdgeFixture{gs: gs, executor: NewExecutor(gs)}
	var err error
	if f.a, err = gs.CreateNode([]string{"P"}, map[string]storage.Value{"name": storage.StringValue("a")}); err != nil {
		t.Fatalf("CreateNode a: %v", err)
	}
	if f.b, err = gs.CreateNode([]string{"Q"}, map[string]storage.Value{"name": storage.StringValue("b")}); err != nil {
		t.Fatalf("CreateNode b: %v", err)
	}
	if f.ab, err = gs.CreateEdge(f.a.ID, f.b.ID, "R", map[string]storage.Value{"w": storage.IntValue(1)}, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	return f
}

func (f *setEdgeFixture) run(t *testing.T, query string) error {
	t.Helper()
	_, err := f.executor.Execute(parseCallInput(t, query))
	return err
}

// edgeProps reads the edge back from storage, so the test sees what was
// written rather than what the query's bindings claim.
func (f *setEdgeFixture) edgeProps(t *testing.T) map[string]storage.Value {
	t.Helper()
	e, err := f.gs.GetEdge(f.ab.ID)
	if err != nil {
		t.Fatalf("GetEdge: %v", err)
	}
	return e.Properties
}

func assertIntProp(t *testing.T, props map[string]storage.Value, key string, want int64) {
	t.Helper()
	v, ok := props[key]
	if !ok {
		t.Fatalf("property %q missing; properties = %v", key, props)
	}
	got, err := v.AsInt()
	if err != nil || got != want {
		t.Fatalf("property %q = %v (err %v), want %d", key, v, err, want)
	}
}

func TestSet_RelationshipProperty_IsStored(t *testing.T) {
	f := newSetEdgeFixture(t)

	if err := f.run(t, "MATCH (a:P)-[r:R]->(b:Q) SET r.w = 5"); err != nil {
		t.Fatalf("SET r.w: %v", err)
	}
	assertIntProp(t, f.edgeProps(t), "w", 5)
}

func TestSet_TwoRelationshipProperties_BothStored(t *testing.T) {
	f := newSetEdgeFixture(t)

	if err := f.run(t, "MATCH (a:P)-[r:R]->(b:Q) SET r.x = 2, r.y = 3"); err != nil {
		t.Fatalf("SET r.x, r.y: %v", err)
	}
	props := f.edgeProps(t)
	assertIntProp(t, props, "x", 2)
	assertIntProp(t, props, "y", 3)
	assertIntProp(t, props, "w", 1)
}

func TestSet_RelationshipPropertyToNull_RemovesIt(t *testing.T) {
	f := newSetEdgeFixture(t)

	if err := f.run(t, "MATCH (a:P)-[r:R]->(b:Q) SET r.w = null"); err != nil {
		t.Fatalf("SET r.w = null: %v", err)
	}
	if v, ok := f.edgeProps(t)["w"]; ok {
		t.Fatalf("property w still on the edge: %v", v)
	}
}

func TestRemove_RelationshipProperty_RemovesIt(t *testing.T) {
	f := newSetEdgeFixture(t)

	if err := f.run(t, "MATCH (a:P)-[r:R]->(b:Q) REMOVE r.w"); err != nil {
		t.Fatalf("REMOVE r.w: %v", err)
	}
	if v, ok := f.edgeProps(t)["w"]; ok {
		t.Fatalf("property w still on the edge: %v", v)
	}
}

// A skipped write still reports its rows as affected, so anything SET or
// REMOVE cannot write to must refuse the query.
func TestSetAndRemove_UnwritableTargets_Refuse(t *testing.T) {
	for _, query := range []string{
		"MATCH (a:P) SET x.w = 1",
		"MATCH (a:P) REMOVE x.w",
		"MATCH (a:P)-[r:R*1..2]->(b:Q) SET r.w = 1",
		"MATCH (a:P)-[r:R*1..2]->(b:Q) REMOVE r.w",
	} {
		t.Run(query, func(t *testing.T) {
			f := newSetEdgeFixture(t)
			if err := f.run(t, query); err == nil {
				t.Fatal("query succeeded; want a refusal")
			}
			assertIntProp(t, f.edgeProps(t), "w", 1)
		})
	}
}

// OPTIONAL MATCH binds null when nothing matches; SET and REMOVE on null are
// no-ops in Cypher.
func TestSetAndRemove_NullFromOptionalMatch_IsNoOp(t *testing.T) {
	for _, query := range []string{
		"MATCH (a:P) OPTIONAL MATCH (a)-[r:NONE]->(x) SET r.w = 9",
		"MATCH (a:P) OPTIONAL MATCH (a)-[r:NONE]->(x) REMOVE r.w",
	} {
		t.Run(query, func(t *testing.T) {
			f := newSetEdgeFixture(t)
			if err := f.run(t, query); err != nil {
				t.Fatalf("%s: %v", query, err)
			}
			assertIntProp(t, f.edgeProps(t), "w", 1)
		})
	}
}

// An edge created with nil properties must accept a SET; the storage write
// path must not assume the map exists.
func TestSet_RelationshipCreatedWithNilProperties(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	a, err := gs.CreateNode([]string{"P"}, nil)
	if err != nil {
		t.Fatalf("CreateNode a: %v", err)
	}
	b, err := gs.CreateNode([]string{"Q"}, nil)
	if err != nil {
		t.Fatalf("CreateNode b: %v", err)
	}
	e, err := gs.CreateEdge(a.ID, b.ID, "R", nil, 1)
	if err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	if _, err := NewExecutor(gs).Execute(parseCallInput(t, "MATCH (a:P)-[r:R]->(b:Q) SET r.w = 5")); err != nil {
		t.Fatalf("SET r.w on an edge with nil properties: %v", err)
	}
	stored, err := gs.GetEdge(e.ID)
	if err != nil {
		t.Fatalf("GetEdge: %v", err)
	}
	assertIntProp(t, stored.Properties, "w", 5)
}
