package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// setupTestServerWithLabelNode creates a fresh server with a single
// "Label" node carrying name/age properties. setupTestServerWithData's
// fixed Employee dataset doesn't isolate the whole-node RETURN shape as
// cleanly (extra fields, multiple rows), so the decodeResultRows tests
// build their own minimal fixture instead (#454).
func setupTestServerWithLabelNode(t *testing.T) (*Server, func(), *storage.Node) {
	t.Helper()

	server, cleanup := setupTestServer(t)
	gs := server.graph

	node, err := gs.CreateNode([]string{"Label"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	if err != nil {
		cleanup()
		t.Fatalf("Failed to create test node: %v", err)
	}

	return server, cleanup, node
}

// TestAPI_Query_Decode_PropertyAccess is the sanity / no-regression case
// for issue #454: property access (n.name) already decodes through
// extractStorageValue/ExtractValue before decodeResultRows exists, so
// this passes both before and after the fix. It's pinned here so a
// future change to decodeResultRows can't silently break the
// already-working property-access path.
func TestAPI_Query_Decode_PropertyAccess(t *testing.T) {
	server, cleanup, _ := setupTestServerWithLabelNode(t)
	defer cleanup()

	rr := makeQueryRequest(t, server, "MATCH (n:Label) RETURN n.name")
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d, body: %s", rr.Code, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(resp.Rows), resp.Rows)
	}

	name, ok := resp.Rows[0]["n.name"].(string)
	if !ok {
		t.Fatalf(`expected resp.Rows[0]["n.name"] to be a string, got %v (%T)`,
			resp.Rows[0]["n.name"], resp.Rows[0]["n.name"])
	}
	if name != "Alice" {
		t.Errorf(`expected "Alice", got %q`, name)
	}
}

// TestAPI_Query_Decode_ReturnNode is the load-bearing case for #454.
// MATCH (n:Label) RETURN n puts a raw *storage.Node into the row
// (pkg/query/match_node.go binds it, executor_results.go returns it
// unchanged when there's no .property suffix). Before the fix, that
// pointer round-trips through encoding/json with no json tags at all
// (capitalized field names) and Properties values still carrying the
// raw {Type, Data} shape — asserting on the lowercase "properties" key
// containing a decoded "name"/"age" must fail pre-fix.
//
// Uses "AS n" to pin the column name to "n": buildColumnName
// (pkg/query/executor_results.go, out of scope for this fix) names a
// bare unaliased `RETURN n` column "n." (variable + "." + empty
// property) rather than "n" — a pre-existing, unrelated quirk. The
// explicit alias sidesteps it so this test exercises only the
// decode-shape behavior under test.
func TestAPI_Query_Decode_ReturnNode(t *testing.T) {
	server, cleanup, _ := setupTestServerWithLabelNode(t)
	defer cleanup()

	rr := makeQueryRequest(t, server, "MATCH (n:Label) RETURN n AS n")
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d, body: %s", rr.Code, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(resp.Rows), resp.Rows)
	}

	nodeVal, ok := resp.Rows[0]["n"].(map[string]any)
	if !ok {
		t.Fatalf(`expected resp.Rows[0]["n"] to decode as map[string]any (the JSON shape of *NodeResponse), got %v (%T)`,
			resp.Rows[0]["n"], resp.Rows[0]["n"])
	}

	propsVal, ok := nodeVal["properties"].(map[string]any)
	if !ok {
		t.Fatalf(`expected "properties" key (lowercase, per NodeResponse json tag) to be a map[string]any, got %v (%T); full node value: %+v`,
			nodeVal["properties"], nodeVal["properties"], nodeVal)
	}

	name, ok := propsVal["name"].(string)
	if !ok {
		t.Fatalf(`expected properties["name"] to be a string "Alice", got %v (%T)`,
			propsVal["name"], propsVal["name"])
	}
	if name != "Alice" {
		t.Errorf(`expected name "Alice", got %q`, name)
	}

	// JSON numbers decode to float64, never assume int here.
	age, ok := propsVal["age"].(float64)
	if !ok {
		t.Fatalf(`expected properties["age"] to be a float64 (JSON numbers decode to float64), got %v (%T)`,
			propsVal["age"], propsVal["age"])
	}
	if age != 30 {
		t.Errorf("expected age 30, got %v", age)
	}
}

// TestAPI_Query_Decode_AggregationPassThrough guards against
// decodeResultRows breaking the already-working aggregation path.
// Aggregation scalars come back as native Go values via
// AggregationComputer.ExtractValue, never as storage.Value, so the
// decode dispatch's default case must pass them through unchanged.
func TestAPI_Query_Decode_AggregationPassThrough(t *testing.T) {
	server, cleanup, _ := setupTestServerWithLabelNode(t)
	defer cleanup()

	rr := makeQueryRequest(t, server, "MATCH (n:Label) RETURN COUNT(n) AS total")
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d, body: %s", rr.Code, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(resp.Rows), resp.Rows)
	}

	total, ok := resp.Rows[0]["total"].(float64)
	if !ok {
		t.Fatalf(`expected resp.Rows[0]["total"] to be a float64, got %v (%T)`,
			resp.Rows[0]["total"], resp.Rows[0]["total"])
	}
	if total != 1 {
		t.Errorf("expected COUNT=1, got %v", total)
	}
}

// TestDecodeResultRows_Idempotent exercises decodeResultRows directly
// (not via HTTP) and requires that running it twice in a row produces
// an identical result to running it once. This specifically targets
// the *storage.Node case: after the first pass the value becomes a
// *NodeResponse, which must NOT be re-matched by the *storage.Node
// case on a second pass — it must fall through to the default
// pass-through case instead.
func TestDecodeResultRows_Idempotent(t *testing.T) {
	server, cleanup, node := setupTestServerWithLabelNode(t)
	defer cleanup()

	rows := []map[string]any{
		{
			"n":      node,
			"n.name": storage.StringValue("Alice"),
			"total":  int64(1), // already-native scalar, as aggregation would produce
		},
	}

	ctx := context.Background()
	firstPass := server.decodeResultRows(ctx, rows)
	secondPass := server.decodeResultRows(ctx, firstPass)

	if !reflect.DeepEqual(firstPass, secondPass) {
		t.Fatalf("decodeResultRows is not idempotent: first=%+v second=%+v", firstPass, secondPass)
	}

	nodeResp, ok := firstPass[0]["n"].(*NodeResponse)
	if !ok {
		t.Fatalf(`expected firstPass[0]["n"] to be *NodeResponse after first decode pass, got %T`, firstPass[0]["n"])
	}
	nodeResp2, ok := secondPass[0]["n"].(*NodeResponse)
	if !ok {
		t.Fatalf(`expected secondPass[0]["n"] to remain *NodeResponse after second decode pass, got %T`, secondPass[0]["n"])
	}
	if !reflect.DeepEqual(nodeResp, nodeResp2) {
		t.Fatalf("NodeResponse changed between decode passes: first=%+v second=%+v", nodeResp, nodeResp2)
	}
}

// TestAPI_Query_Decode_ReturnEdge mirrors TestAPI_Query_Decode_ReturnNode
// for the *storage.Edge case (MATCH ... RETURN r). Optional/nice-to-have
// per the #454 plan — the node case above is the load-bearing test.
// Uses "AS r" for the same reason as TestAPI_Query_Decode_ReturnNode.
func TestAPI_Query_Decode_ReturnEdge(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	gs := server.graph

	from, err := gs.CreateNode([]string{"Label"}, map[string]storage.Value{
		"name": storage.StringValue("A"),
	})
	if err != nil {
		t.Fatalf("failed to create from-node: %v", err)
	}
	to, err := gs.CreateNode([]string{"Label"}, map[string]storage.Value{
		"name": storage.StringValue("B"),
	})
	if err != nil {
		t.Fatalf("failed to create to-node: %v", err)
	}
	if _, err := gs.CreateEdge(from.ID, to.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.5); err != nil {
		t.Fatalf("failed to create edge: %v", err)
	}

	rr := makeQueryRequest(t, server, "MATCH (a:Label)-[r]->(b:Label) RETURN r AS r")
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d, body: %s", rr.Code, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(resp.Rows), resp.Rows)
	}

	edgeVal, ok := resp.Rows[0]["r"].(map[string]any)
	if !ok {
		t.Fatalf(`expected resp.Rows[0]["r"] to decode as map[string]any (the JSON shape of *EdgeResponse), got %v (%T)`,
			resp.Rows[0]["r"], resp.Rows[0]["r"])
	}

	propsVal, ok := edgeVal["properties"].(map[string]any)
	if !ok {
		t.Fatalf(`expected "properties" key to be a map[string]any, got %v (%T); full edge value: %+v`,
			edgeVal["properties"], edgeVal["properties"], edgeVal)
	}

	since, ok := propsVal["since"].(float64)
	if !ok {
		t.Fatalf(`expected properties["since"] to be a float64, got %v (%T)`,
			propsVal["since"], propsVal["since"])
	}
	if since != 2020 {
		t.Errorf("expected since 2020, got %v", since)
	}
}
