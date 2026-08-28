package query

import "testing"

// These tests cover the conformance hole around `WHERE id(n) = <value>` and
// `WHERE id(r) = <value>`. There was previously NO test anywhere in pkg/query
// exercising id() inside a WHERE predicate (only `RETURN id(n)` was covered by
// TestPhase5_IdFunction), which is exactly where the bug lived: the "=" branch
// in evalComparison used a type-strict Go `==` on `any`, so id() (which returns
// int64) never compared equal to a float64 — the type every JSON-decoded query
// parameter arrives as. See ast_eval.go evalComparison.

// idOfAlice runs `RETURN id(n)` to discover the internal ID assigned to Alice,
// so the WHERE-clause tests below can pin an exact, known-good id value.
func idOfAlice(t *testing.T, executor *Executor) int64 {
	t.Helper()
	res := parseAndExecute(t, executor,
		`MATCH (n:Person) WHERE n.name = 'Alice' RETURN id(n)`)
	if len(res.Rows) != 1 {
		t.Fatalf("setup: expected 1 row for Alice, got %d", len(res.Rows))
	}
	id, ok := res.Rows[0]["id(...)"].(int64)
	if !ok {
		t.Fatalf("setup: expected int64 id, got %T: %v", res.Rows[0]["id(...)"], res.Rows[0]["id(...)"])
	}
	return id
}

// TestWhereID_NodeLiteral covers `WHERE id(n) = <int literal>`. Integer literals
// parse to int64 (parser_properties.go parseValue), so this path matched even
// before the fix — it is here as a regression guard and to document the shape.
func TestWhereID_NodeLiteral(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	id := idOfAlice(t, executor)

	query := `MATCH (n:Person) WHERE id(n) = ` + itoa(id) + ` RETURN n.name`
	res := parseAndExecute(t, executor, query)

	if len(res.Rows) != 1 {
		t.Fatalf("expected 1 row matching id(n) = %d, got %d", id, len(res.Rows))
	}
	if res.Rows[0]["n.name"] != "Alice" {
		t.Errorf("expected Alice, got %v", res.Rows[0]["n.name"])
	}
}

// TestWhereID_NodeParamFloat64 is the actual regression. HTTP query parameters
// are JSON-decoded, and encoding/json unmarshals every JSON number into a Go
// float64. id() returns int64, so `WHERE id(n) = $param` compared int64 to
// float64. The pre-fix "=" branch used `leftVal == rightVal` (type-strict), so
// this silently matched zero rows.
func TestWhereID_NodeParamFloat64(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	id := idOfAlice(t, executor)

	// Bind the param as float64, exactly as the JSON HTTP layer delivers it.
	res := parseAndExecuteWithParams(t, executor,
		`MATCH (n:Person) WHERE id(n) = $nodeId RETURN n.name`,
		map[string]any{"nodeId": float64(id)},
	)

	if len(res.Rows) != 1 {
		t.Fatalf("expected 1 row matching id(n) = $nodeId (float64 %v), got %d", float64(id), len(res.Rows))
	}
	if res.Rows[0]["n.name"] != "Alice" {
		t.Errorf("expected Alice, got %v", res.Rows[0]["n.name"])
	}
}

// TestWhereID_NodeParamInt64 guards the int64-param path (a Go caller passing a
// native int64 rather than a JSON float64). This matched before the fix too;
// it must keep matching after.
func TestWhereID_NodeParamInt64(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	id := idOfAlice(t, executor)

	res := parseAndExecuteWithParams(t, executor,
		`MATCH (n:Person) WHERE id(n) = $nodeId RETURN n.name`,
		map[string]any{"nodeId": id},
	)

	if len(res.Rows) != 1 {
		t.Fatalf("expected 1 row matching id(n) = $nodeId (int64 %d), got %d", id, len(res.Rows))
	}
}

// TestWhereID_EdgeParamFloat64 exercises the same fix on an edge:
// `WHERE id(r) = $edgeId` with a float64-bound param. The Phase5 graph seeds
// two edges from Alice; we discover one edge's id via RETURN id(r), then match
// it back by id in WHERE.
func TestWhereID_EdgeParamFloat64(t *testing.T) {
	_, executor, cleanup := setupPhase5Graph(t)
	defer cleanup()

	// Discover an edge id.
	idRes := parseAndExecute(t, executor,
		`MATCH (a:Person)-[r:KNOWS]->(b:Person) RETURN id(r)`)
	if len(idRes.Rows) != 1 {
		t.Fatalf("setup: expected 1 KNOWS edge, got %d", len(idRes.Rows))
	}
	edgeID, ok := idRes.Rows[0]["id(...)"].(int64)
	if !ok {
		t.Fatalf("setup: expected int64 edge id, got %T", idRes.Rows[0]["id(...)"])
	}

	res := parseAndExecuteWithParams(t, executor,
		`MATCH (a:Person)-[r:KNOWS]->(b:Person) WHERE id(r) = $edgeId RETURN id(r)`,
		map[string]any{"edgeId": float64(edgeID)},
	)

	if len(res.Rows) != 1 {
		t.Fatalf("expected 1 row matching id(r) = $edgeId (float64 %v), got %d", float64(edgeID), len(res.Rows))
	}
}

// itoa avoids importing strconv just for one call site in this test file.
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
