package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// CREATE and MERGE act once for each row, and a node a row already binds is
// reused, never created again. CreateStep used to create every node in the
// pattern, bound or not, once for the whole query, and take relationship
// endpoints from the first row only; MergeStep matched with an empty binding
// and decided match-or-create for all rows at once.

func labelCounts(gs *storage.GraphStorage) map[string]int {
	counts := map[string]int{}
	for _, n := range gs.GetAllNodesAcrossTenants() {
		l := "<none>"
		if len(n.Labels) > 0 {
			l = n.Labels[0]
		}
		counts[l]++
	}
	return counts
}

func edgesOfType(t *testing.T, gs *storage.GraphStorage, edgeType string) []*storage.Edge {
	t.Helper()
	edges, err := gs.FindEdgesByTypeAcrossTenants(edgeType)
	if err != nil {
		t.Fatalf("edges %s: %v", edgeType, err)
	}
	return edges
}

func newCreateGraph(t *testing.T) (*storage.GraphStorage, *Executor, *storage.Node, *storage.Node) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	x := mustOracleNode(t, gs, "P", map[string]storage.Value{"name": storage.StringValue("x")})
	y := mustOracleNode(t, gs, "P", map[string]storage.Value{"name": storage.StringValue("y")})
	return gs, NewExecutor(gs), x, y
}

func mustRun(t *testing.T, ex *Executor, query string) {
	t.Helper()
	if _, err := ex.Execute(parseCallInput(t, query)); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func TestCreate_ReusesABoundNode(t *testing.T) {
	gs, ex, x, _ := newCreateGraph(t)

	mustRun(t, ex, "MATCH (a:P {name: 'x'}) CREATE (a)-[:R]->(b:Q)")

	if c := labelCounts(gs); c["P"] != 2 || c["Q"] != 1 || len(c) != 2 {
		t.Fatalf("nodes by label = %v, want P:2 Q:1 and no other node", c)
	}
	edges := edgesOfType(t, gs, "R")
	if len(edges) != 1 || edges[0].FromNodeID != x.ID {
		t.Fatalf("R edges = %v, want one edge from the matched node %d", edges, x.ID)
	}
}

func TestCreate_ActsOncePerRow(t *testing.T) {
	gs, ex, x, y := newCreateGraph(t)

	mustRun(t, ex, "MATCH (a:P) CREATE (a)-[:R]->(b:Q)")

	if c := labelCounts(gs); c["P"] != 2 || c["Q"] != 2 || len(c) != 2 {
		t.Fatalf("nodes by label = %v, want P:2 Q:2", c)
	}
	from := map[uint64]int{}
	for _, e := range edgesOfType(t, gs, "R") {
		from[e.FromNodeID]++
	}
	if from[x.ID] != 1 || from[y.ID] != 1 || len(from) != 2 {
		t.Fatalf("R edges by source = %v, want one from each P", from)
	}
}

func TestCreate_StandaloneAndNoRows(t *testing.T) {
	gs, ex, _, _ := newCreateGraph(t)

	mustRun(t, ex, "CREATE (a:X)-[:R]->(b:Y)")
	mustRun(t, ex, "MATCH (n:Nobody) CREATE (b:Z)")

	if c := labelCounts(gs); c["X"] != 1 || c["Y"] != 1 || c["Z"] != 0 {
		t.Fatalf("nodes by label = %v, want X:1 Y:1 and no Z (no rows, nothing created)", c)
	}
}

func TestCreate_BindsTheRelationshipVariable(t *testing.T) {
	gs, ex, _, _ := newCreateGraph(t)

	mustRun(t, ex, "MATCH (a:P {name: 'x'}) CREATE (a)-[r:R]->(b:Q) SET r.w = 7")

	edges := edgesOfType(t, gs, "R")
	if len(edges) != 1 {
		t.Fatalf("R edges = %d, want 1", len(edges))
	}
	if v, err := edges[0].Properties["w"].AsInt(); err != nil || v != 7 {
		t.Fatalf("r.w = %v (err %v), want 7", edges[0].Properties["w"], err)
	}
}

// openCypher refuses labels or properties on a variable the query already
// bound; graphdb used to create a new node for it.
func TestCreate_RefusesLabelsOnABoundVariable(t *testing.T) {
	gs, ex, _, _ := newCreateGraph(t)

	if _, err := ex.Execute(parseCallInput(t, "MATCH (a:P {name: 'x'}) CREATE (a:Extra)-[:R]->(b:Q)")); err == nil {
		t.Fatal("CREATE with a label on a bound variable succeeded")
	}
	if c := labelCounts(gs); len(c) != 1 || c["P"] != 2 {
		t.Fatalf("nodes by label = %v, want only the two P nodes", c)
	}
}

// x already has a tag; y does not. MERGE decides for each row.
func TestMerge_DecidesPerRow(t *testing.T) {
	gs, ex, x, _ := newCreateGraph(t)
	tag := mustOracleNode(t, gs, "Tag", nil)
	if _, err := gs.CreateEdge(x.ID, tag.ID, "HAS", map[string]storage.Value{}, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	for run := 1; run <= 2; run++ {
		mustRun(t, ex, "MATCH (p:P) MERGE (p)-[:HAS]->(t:Tag)")

		if c := labelCounts(gs); c["Tag"] != 2 {
			t.Fatalf("run %d: nodes by label = %v, want Tag:2 (x reuses its tag, y gets one)", run, c)
		}
		from := map[uint64]int{}
		for _, e := range edgesOfType(t, gs, "HAS") {
			from[e.FromNodeID]++
		}
		if len(from) != 2 || from[x.ID] != 1 {
			t.Fatalf("run %d: HAS edges by source = %v, want one from each P", run, from)
		}
	}
}

// syntopica's trust-cluster sync (workers/src/services/trust/
// trust-graph-sync-service.ts) MERGEs two memberships in one segment. The
// second MERGE used to replace the first, so u1's membership was never
// created. Run twice: MERGE must not duplicate anything on the second run.
func TestMerge_RepeatedClausesRunInTextOrder(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	ex := NewExecutor(gs)
	mustOracleNode(t, gs, "User", map[string]storage.Value{"id": storage.StringValue("a")})
	mustOracleNode(t, gs, "User", map[string]storage.Value{"id": storage.StringValue("b")})

	const q = "MERGE (cluster:AnswerCluster {id: 'k1'}) SET cluster.memberCount = 2 " +
		"WITH cluster MATCH (u1:User {id: 'a'}), (u2:User {id: 'b'}) " +
		"MERGE (u1)-[m1:MEMBER]->(cluster) MERGE (u2)-[m2:MEMBER]->(cluster) " +
		"SET m1.similarity = 1, m2.similarity = 2"

	for run := 1; run <= 2; run++ {
		if _, err := ex.Execute(parseCallInput(t, q)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}

		labels := map[string]int{}
		for _, n := range gs.GetAllNodesAcrossTenants() {
			labels[n.Labels[0]]++
		}
		if labels["AnswerCluster"] != 1 || labels["User"] != 2 || len(labels) != 2 {
			t.Fatalf("run %d: nodes by label = %v, want AnswerCluster:1 User:2", run, labels)
		}

		edges, err := gs.FindEdgesByTypeAcrossTenants("MEMBER")
		if err != nil {
			t.Fatalf("run %d: edges: %v", run, err)
		}
		sims := map[int64]bool{}
		for _, e := range edges {
			v, err := e.Properties["similarity"].AsInt()
			if err != nil {
				t.Fatalf("run %d: edge %d similarity: %v (props %v)", run, e.ID, err, e.Properties)
			}
			sims[v] = true
		}
		if len(edges) != 2 || !sims[1] || !sims[2] {
			t.Fatalf("run %d: %d MEMBER edges with similarities %v, want 2 edges with 1 and 2", run, len(edges), sims)
		}
	}
}

// CREATE (a)<-[:R]-(b) makes b -> a; the direction used to be ignored, so
// MERGE with <- never found what it had created and added an edge each run.
func TestCreateAndMerge_IncomingDirection(t *testing.T) {
	gs, ex, x, y := newCreateGraph(t)

	mustRun(t, ex, "MATCH (a:P {name: 'x'}), (b:P {name: 'y'}) CREATE (a)<-[:R]-(b)")
	for run := 1; run <= 2; run++ {
		mustRun(t, ex, "MATCH (a:P {name: 'x'}), (b:P {name: 'y'}) MERGE (a)<-[:M]-(b)")
	}

	for _, typ := range []string{"R", "M"} {
		edges := edgesOfType(t, gs, typ)
		if len(edges) != 1 || edges[0].FromNodeID != y.ID || edges[0].ToNodeID != x.ID {
			t.Fatalf("%s edges = %v, want exactly one from y (%d) to x (%d)", typ, edges, y.ID, x.ID)
		}
	}
}

// Each refusal must happen before anything is created.
func TestCreate_RefusesWithoutPartialWrites(t *testing.T) {
	cases := []string{
		// openCypher requires a direction in CREATE.
		"CREATE (a:X)-[:R]-(b:Y)",
		// A relationship variable the row already binds cannot be created again.
		"MATCH (a:P {name: 'x'})-[r:HAS]->(t) CREATE (z:Z)-[r:HAS]->(a)",
		// A null variable cannot be an endpoint; y must not be left behind.
		"OPTIONAL MATCH (x:Nobody) WITH x MERGE (y:Y)<-[:R]-(x)",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			gs, ex, x, _ := newCreateGraph(t)
			tag := mustOracleNode(t, gs, "Tag", nil)
			if _, err := gs.CreateEdge(x.ID, tag.ID, "HAS", map[string]storage.Value{}, 1); err != nil {
				t.Fatalf("CreateEdge: %v", err)
			}
			before := labelCounts(gs)

			if _, err := ex.Execute(parseCallInput(t, query)); err == nil {
				t.Fatal("query succeeded; want a refusal")
			}
			if after := labelCounts(gs); len(after) != len(before) || after["P"] != before["P"] || after["Tag"] != before["Tag"] {
				t.Fatalf("nodes changed from %v to %v", before, after)
			}
		})
	}
}

// With no variable of the pattern bound, the first row creates and every
// later row matches what it created: one Tag, ON CREATE once, ON MATCH after.
func TestMerge_UnboundPatternOverSeveralRows(t *testing.T) {
	gs, ex, _, _ := newCreateGraph(t)

	mustRun(t, ex, "MATCH (p:P) MERGE (t:Tag {name: 'x'}) ON CREATE SET t.created = 1 ON MATCH SET t.matched = 1")

	var tags []*storage.Node
	for _, n := range gs.GetAllNodesAcrossTenants() {
		if n.Labels[0] == "Tag" {
			tags = append(tags, n)
		}
	}
	if len(tags) != 1 {
		t.Fatalf("Tag nodes = %d, want 1", len(tags))
	}
	if _, ok := tags[0].Properties["created"]; !ok {
		t.Errorf("ON CREATE SET did not run: %v", tags[0].Properties)
	}
	if _, ok := tags[0].Properties["matched"]; !ok {
		t.Errorf("ON MATCH SET did not run for the second row: %v", tags[0].Properties)
	}
}

// MERGE may name an undirected relationship: it matches either direction and,
// when nothing matches, creates one left to right. A second run matches it.
func TestMerge_UndirectedRelationshipIsAllowedAndIdempotent(t *testing.T) {
	gs, ex, x, y := newCreateGraph(t)

	for run := 1; run <= 2; run++ {
		mustRun(t, ex, "MATCH (a:P {name: 'x'}), (b:P {name: 'y'}) MERGE (a)-[:U]-(b)")
	}
	edges := edgesOfType(t, gs, "U")
	if len(edges) != 1 || edges[0].FromNodeID != x.ID || edges[0].ToNodeID != y.ID {
		t.Fatalf("U edges = %v, want exactly one from x to y", edges)
	}
}
