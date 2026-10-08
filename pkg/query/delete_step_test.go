package query

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// deleteFixture is a graph of a -R-> b and a -R-> c, so a appears in two rows
// of MATCH (a:P)-[r:R]->(x) and each edge appears in one.
type deleteFixture struct {
	gs       *storage.GraphStorage
	a, b, c  *storage.Node
	ab, ac   *storage.Edge
	executor *Executor
}

func newDeleteFixture(t *testing.T) *deleteFixture {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)

	f := &deleteFixture{gs: gs, executor: NewExecutor(gs)}
	f.a = mustOracleNode(t, gs, "P", map[string]storage.Value{"name": storage.StringValue("a")})
	f.b = mustOracleNode(t, gs, "Q", map[string]storage.Value{"name": storage.StringValue("b")})
	f.c = mustOracleNode(t, gs, "Q", map[string]storage.Value{"name": storage.StringValue("c")})
	var err error
	if f.ab, err = gs.CreateEdge(f.a.ID, f.b.ID, "R", map[string]storage.Value{}, 1); err != nil {
		t.Fatalf("CreateEdge a-b: %v", err)
	}
	if f.ac, err = gs.CreateEdge(f.a.ID, f.c.ID, "R", map[string]storage.Value{}, 1); err != nil {
		t.Fatalf("CreateEdge a-c: %v", err)
	}
	return f
}

func (f *deleteFixture) run(t *testing.T, query string) error {
	t.Helper()
	_, err := f.executor.Execute(parseCallInput(t, query))
	return err
}

func (f *deleteFixture) edgeExists(t *testing.T, id uint64) bool {
	t.Helper()
	_, err := f.gs.GetEdge(id)
	if err == nil {
		return true
	}
	if !errors.Is(err, storage.ErrEdgeNotFound) {
		t.Fatalf("GetEdge(%d): %v", id, err)
	}
	return false
}

func (f *deleteFixture) nodeExists(t *testing.T, id uint64) bool {
	t.Helper()
	_, err := f.gs.GetNode(id)
	if err == nil {
		return true
	}
	if !errors.Is(err, storage.ErrNodeNotFound) {
		t.Fatalf("GetNode(%d): %v", id, err)
	}
	return false
}

func TestDelete_RelationshipVariable_RemovesTheEdge(t *testing.T) {
	f := newDeleteFixture(t)

	if err := f.run(t, "MATCH (a:P)-[r:R]->(x:Q {name: 'b'}) DELETE r"); err != nil {
		t.Fatalf("DELETE r: %v", err)
	}

	if f.edgeExists(t, f.ab.ID) {
		t.Errorf("edge a-b still in storage after DELETE r")
	}
	if !f.edgeExists(t, f.ac.ID) {
		t.Errorf("edge a-c was not matched but is gone")
	}
	for _, n := range []*storage.Node{f.a, f.b, f.c} {
		if !f.nodeExists(t, n.ID) {
			t.Errorf("node %d is gone; DELETE r must not delete nodes", n.ID)
		}
	}
}

// The node delete cascades to its edges, so an edge delete that runs after it
// finds nothing. Deleting edges first makes the order of variables irrelevant.
func TestDelete_RelationshipAndItsNode_InOneQuery(t *testing.T) {
	for _, query := range []string{
		"MATCH (a:P)-[r:R]->(x:Q) DELETE r, a",
		"MATCH (a:P)-[r:R]->(x:Q) DELETE a, r",
	} {
		t.Run(query, func(t *testing.T) {
			f := newDeleteFixture(t)

			if err := f.run(t, query); err != nil {
				t.Fatalf("%s: %v", query, err)
			}

			if f.nodeExists(t, f.a.ID) {
				t.Errorf("node a still in storage")
			}
			if f.edgeExists(t, f.ab.ID) || f.edgeExists(t, f.ac.ID) {
				t.Errorf("an edge of a is still in storage")
			}
			if !f.nodeExists(t, f.b.ID) || !f.nodeExists(t, f.c.ID) {
				t.Errorf("a target node is gone")
			}
		})
	}
}

// a is bound in two rows; it must be deleted once, not fail on the second row.
func TestDelete_NodeBoundInSeveralRows_DeletedOnce(t *testing.T) {
	f := newDeleteFixture(t)

	if err := f.run(t, "MATCH (a:P)-[r:R]->(x:Q) DELETE a"); err != nil {
		t.Fatalf("DELETE a: %v", err)
	}
	if f.nodeExists(t, f.a.ID) {
		t.Errorf("node a still in storage")
	}
}

func TestDelete_UndefinedVariable_Refuses(t *testing.T) {
	f := newDeleteFixture(t)

	err := f.run(t, "MATCH (a:P) DELETE x")
	if err == nil {
		t.Fatal("DELETE of a variable the query never binds succeeded")
	}
	if !f.nodeExists(t, f.a.ID) {
		t.Errorf("node a is gone after a refused DELETE")
	}
}

// A variable-length relationship binds a list of edges. Refusing is loud;
// guessing whether the caller meant every edge on the path is not.
func TestDelete_VariableLengthRelationship_Refuses(t *testing.T) {
	f := newDeleteFixture(t)

	err := f.run(t, "MATCH (a:P)-[r:R*1..2]->(x:Q) DELETE r")
	if err == nil {
		t.Fatal("DELETE of a variable-length relationship succeeded")
	}
	if !f.edgeExists(t, f.ab.ID) || !f.edgeExists(t, f.ac.ID) {
		t.Errorf("an edge is gone after a refused DELETE")
	}
}

// OPTIONAL MATCH binds null when nothing matches, and DELETE null is a no-op.
func TestDelete_NullFromOptionalMatch_IsNoOp(t *testing.T) {
	f := newDeleteFixture(t)

	if err := f.run(t, "MATCH (a:P) OPTIONAL MATCH (a)-[r:NONE]->(x) DELETE r"); err != nil {
		t.Fatalf("DELETE of a null relationship: %v", err)
	}
	if !f.edgeExists(t, f.ab.ID) || !f.edgeExists(t, f.ac.ID) || !f.nodeExists(t, f.a.ID) {
		t.Errorf("DELETE of null removed something")
	}
}
