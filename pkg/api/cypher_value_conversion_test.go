package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A Cypher write converted any value that was not a string, int, float or
// bool with fmt.Sprintf("%v"), so null, lists and maps were stored as the
// strings "<nil>", "[a b]" and "map[k:1]" with a 200 and no error. REST and
// GraphQL already convert through storage.ValueFromJSON; these pin that
// Cypher writes keep the same structure.

func cypherProbeNode(t *testing.T, s *Server) uint64 {
	t.Helper()
	n, err := s.graph.CreateNodeWithTenant("default", []string{"Probe"}, map[string]storage.Value{
		"x":    storage.StringValue("before"),
		"keep": storage.StringValue("k"),
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	return n.ID
}

func runCypher(t *testing.T, s *Server, query string, params map[string]any) {
	t.Helper()
	body := map[string]any{"query": query}
	if params != nil {
		body["parameters"] = params
	}
	rr := httptest.NewRecorder()
	s.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", body, "default"))
	if rr.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", query, rr.Code, rr.Body.String())
	}
}

func storedProperty(t *testing.T, s *Server, id uint64, key string) (storage.Value, bool) {
	t.Helper()
	n, err := s.graph.GetNodeForTenant(id, "default")
	if err != nil {
		t.Fatalf("get node %d: %v", id, err)
	}
	v, ok := n.Properties[key]
	return v, ok
}

// TestCypherSet_NullRemovesProperty pins Cypher semantics: SET n.x = null
// removes the property, the same rule PUT follows since #634 (CC23).
func TestCypherSet_NullRemovesProperty(t *testing.T) {
	for _, tc := range []struct {
		name   string
		query  string
		params map[string]any
	}{
		{"literal", "MATCH (n:Probe) SET n.x = null", nil},
		{"bound", "MATCH (n:Probe) SET n.x = $v", map[string]any{"v": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()
			id := cypherProbeNode(t, server)

			runCypher(t, server, tc.query, tc.params)

			if v, ok := storedProperty(t, server, id, "x"); ok {
				t.Errorf("x = %+v after SET null, want the key removed", v)
			}
			if v, _ := storedProperty(t, server, id, "keep"); v.Type != storage.TypeString {
				t.Errorf("keep = %+v, want the other property kept", v)
			}
		})
	}
}

// TestCypherWrite_ListAndMapKeepStructure pins that a list and a map written
// through Cypher are stored as REST would store them, not as Go %v text.
func TestCypherWrite_ListAndMapKeepStructure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"string list", []any{"a", "b"}},
		{"number list", []any{1.5, 2.5}},
		{"map", map[string]any{"k": 1.0}},
	} {
		t.Run("SET "+tc.name, func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()
			id := cypherProbeNode(t, server)

			runCypher(t, server, "MATCH (n:Probe) SET n.x = $v", map[string]any{"v": tc.value})

			got, _ := storedProperty(t, server, id, "x")
			want := storage.ValueFromJSON(tc.value)
			if got.Type != want.Type || string(got.Data) != string(want.Data) {
				t.Errorf("stored %v as type %v data %q, want type %v data %q", tc.value, got.Type, got.Data, want.Type, want.Data)
			}
		})
		t.Run("CREATE "+tc.name, func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()

			runCypher(t, server, "CREATE (m:Made {x: $v})", map[string]any{"v": tc.value})

			nodes, err := server.graph.GetNodesByLabelForTenant("default", "Made")
			if err != nil || len(nodes) != 1 {
				t.Fatalf("created nodes = %d, err = %v; want 1", len(nodes), err)
			}
			got := nodes[0].Properties["x"]
			want := storage.ValueFromJSON(tc.value)
			if got.Type != want.Type || string(got.Data) != string(want.Data) {
				t.Errorf("stored %v as type %v data %q, want type %v data %q", tc.value, got.Type, got.Data, want.Type, want.Data)
			}
		})
	}
}

// TestCypherCreate_NullStoresNull pins that CREATE with a null property
// stores a JSON null, the rule REST POST follows (CC24), not the string
// "<nil>".
func TestCypherCreate_NullStoresNull(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	runCypher(t, server, "CREATE (m:Made {x: $v})", map[string]any{"v": nil})

	nodes, err := server.graph.GetNodesByLabelForTenant("default", "Made")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("created nodes = %d, err = %v; want 1", len(nodes), err)
	}
	got, ok := nodes[0].Properties["x"]
	want := storage.ValueFromJSON(nil)
	if !ok || got.Type != want.Type || string(got.Data) != string(want.Data) {
		t.Errorf("x = %+v (present=%v), want a JSON null %+v", got, ok, want)
	}
}

// TestCypherSet_ScalarsUnchanged pins that the scalar conversions Cypher
// already had stay as they were. A whole float stays a float, unlike
// ValueFromJSON, which would turn 2.0 into an int.
func TestCypherSet_ScalarsUnchanged(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	id := cypherProbeNode(t, server)

	runCypher(t, server, "MATCH (n:Probe) SET n.f = 2.0, n.i = 3, n.s = 's', n.b = true", nil)

	for key, want := range map[string]storage.ValueType{
		"f": storage.TypeFloat, "i": storage.TypeInt, "s": storage.TypeString, "b": storage.TypeBool,
	} {
		if got, _ := storedProperty(t, server, id, key); got.Type != want {
			t.Errorf("%s stored as type %v, want %v", key, got.Type, want)
		}
	}
}

// TestCypherWhere_CollectionEquality pins that WHERE n.x = $v compares a list
// or a map by value. valuesEqual fell back to ==, which panics on two values
// of the same uncomparable type ([]any, map[string]any); the executor turned
// that into a 500. It was unreachable while a list read back as nil, and
// became reachable once Cypher read lists and maps back.
func TestCypherWhere_CollectionEquality(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	cypherProbeNode(t, server)
	runCypher(t, server, "MATCH (n:Probe) SET n.mixed = $mixed, n.tags = $tags, n.m = $m", map[string]any{
		"mixed": []any{"a", 1.0},
		"tags":  []any{"a", "b"},
		"m":     map[string]any{"k": 1.0, "j": "x"},
	})

	for _, tc := range []struct {
		name  string
		where string
		value any
		want  int
	}{
		{"mixed list equal", "n.mixed = $v", []any{"a", 1.0}, 1},
		{"mixed list differs", "n.mixed = $v", []any{"a", 2.0}, 0},
		{"string list equal", "n.tags = $v", []any{"a", "b"}, 1},
		{"string list order matters", "n.tags = $v", []any{"b", "a"}, 0},
		{"map equal, other key order", "n.m = $v", map[string]any{"j": "x", "k": 1.0}, 1},
		{"map differs", "n.m = $v", map[string]any{"k": 2.0, "j": "x"}, 0},
		{"inline list pattern", "", []any{"a", "b"}, 1},
		{"inline map pattern", "", map[string]any{"j": "x", "k": 1.0}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", map[string]any{
				"query":      cypherCollectionQuery(tc.where, tc.value),
				"parameters": map[string]any{"v": tc.value},
			}, "default"))
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
			}
			var resp struct {
				Rows []map[string]any `json:"rows"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(resp.Rows) != tc.want {
				t.Errorf("rows = %d, want %d: %s", len(resp.Rows), tc.want, rr.Body.String())
			}
		})
	}
}

// cypherCollectionQuery is the WHERE form when where is set, and otherwise the
// inline pattern form on the property the value's shape was stored under.
func cypherCollectionQuery(where string, value any) string {
	if where != "" {
		return "MATCH (n:Probe) WHERE " + where + " RETURN n.keep AS k"
	}
	if _, isMap := value.(map[string]any); isMap {
		return "MATCH (n:Probe {m: $v}) RETURN n.keep AS k"
	}
	return "MATCH (n:Probe {tags: $v}) RETURN n.keep AS k"
}

// TestCypherReturn_ListAndMapRoundTrip pins that RETURN n.x gives back a list
// or a map. The Cypher read converters returned nil for every type but the
// scalars and vectors, so a list came back as null even after a correct write.
func TestCypherReturn_ListAndMapRoundTrip(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	cypherProbeNode(t, server)
	runCypher(t, server, "MATCH (n:Probe) SET n.x = $v, n.m = $m", map[string]any{
		"v": []any{"a", "b"},
		"m": map[string]any{"k": 1.0},
	})

	rr := httptest.NewRecorder()
	server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", map[string]any{
		"query": "MATCH (n:Probe) RETURN n.x AS x, n.m AS m",
	}, "default"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Rows []struct {
			X []string       `json:"x"`
			M map[string]any `json:"m"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1: %s", len(resp.Rows), rr.Body.String())
	}
	row := resp.Rows[0]
	if len(row.X) != 2 || row.X[0] != "a" || row.X[1] != "b" || row.M["k"] != 1.0 {
		t.Errorf("RETURN gave x=%v m=%v, want [a b] and {k:1}: %s", row.X, row.M, rr.Body.String())
	}
}

// TestCypherSet_FailedValueRefusesAndKeepsProperty pins that a SET whose value
// could not be evaluated refuses the query and changes nothing. extractValue
// turns a missing parameter or an evaluation error into nil, and nil now
// removes the property, so a typo in a parameter name deleted data with a 200.
func TestCypherSet_FailedValueRefusesAndKeepsProperty(t *testing.T) {
	for _, tc := range []struct {
		name   string
		query  string
		params map[string]any
	}{
		{"missing parameter, others bound", "MATCH (n:Probe) SET n.x = $missing", map[string]any{"other": 1.0}},
		{"missing parameter, none bound", "MATCH (n:Probe) SET n.x = $missing", nil},
		{"missing parameter in MERGE ON MATCH", "MERGE (n:Probe {keep: 'k'}) ON MATCH SET n.x = $missing", map[string]any{"other": 1.0}},
		{"division by zero", "MATCH (n:Probe) SET n.x = n.i / 0", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()
			id := cypherProbeNode(t, server)
			if err := server.graph.UpdateNodeForTenant(id, map[string]storage.Value{"i": storage.IntValue(4)}, "default"); err != nil {
				t.Fatal(err)
			}

			body := map[string]any{"query": tc.query}
			if tc.params != nil {
				body["parameters"] = tc.params
			}
			rr := httptest.NewRecorder()
			server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", body, "default"))
			if rr.Code == http.StatusOK {
				t.Errorf("status 200, want a refusal: %s", rr.Body.String())
			}
			if v, ok := storedProperty(t, server, id, "x"); !ok {
				t.Errorf("x removed by a refused SET, want it kept (value was %+v)", v)
			}
		})
	}
}

// TestCypherSet_NullValuedExpressionRemoves pins the other side: an expression
// that evaluates to null without an error removes the property, as Cypher
// specifies. A missing property reads as null.
func TestCypherSet_NullValuedExpressionRemoves(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	id := cypherProbeNode(t, server)

	runCypher(t, server, "MATCH (n:Probe) SET n.x = n.absent", nil)

	if v, ok := storedProperty(t, server, id, "x"); ok {
		t.Errorf("x = %+v after SET n.x = n.absent, want removed", v)
	}
}

// TestCypherAggregate_ListValuesIgnoredByNumericAggregates pins MIN, MAX and
// AVG over a property where one node holds a list. A list read back as nil
// before lists became readable, so these aggregates never met one: MIN and
// MAX kept a list in first place (compareValues reports 0 against it), and
// AVG divided by the count of every value while SUM skipped the list. IN on
// a list of lists panicked on ==; on [5] it matched the list row.
func TestCypherAggregate_ListValuesIgnoredByNumericAggregates(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	for _, v := range []storage.Value{storage.StringArrayValue([]string{"a", "b"}), storage.IntValue(5), storage.IntValue(1)} {
		if _, err := server.graph.CreateNodeWithTenant("default", []string{"Agg"}, map[string]storage.Value{"v": v}); err != nil {
			t.Fatal(err)
		}
	}

	rr := httptest.NewRecorder()
	server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", map[string]any{
		"query": "MATCH (n:Agg) RETURN MIN(n.v) AS lo, MAX(n.v) AS hi, AVG(n.v) AS mean",
	}, "default"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || len(resp.Rows) != 1 {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	row := resp.Rows[0]
	if row["lo"] != 1.0 || row["hi"] != 5.0 || row["mean"] != 3.0 {
		t.Errorf("min/max/avg = %v/%v/%v, want 1/5/3: %s", row["lo"], row["hi"], row["mean"], rr.Body.String())
	}

	for _, tc := range []struct {
		name string
		list []any
		want int
	}{
		{"list of lists", []any{[]any{"a", "b"}}, 1},
		{"number does not match a list", []any{5.0}, 1},
		{"string does not match a number", []any{"5"}, 0},
	} {
		t.Run("IN "+tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", map[string]any{
				"query":      "MATCH (n:Agg) WHERE n.v IN $l RETURN n.v AS v",
				"parameters": map[string]any{"l": tc.list},
			}, "default"))
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
			}
			var resp struct {
				Rows []map[string]any `json:"rows"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(resp.Rows) != tc.want {
				t.Errorf("rows = %d, want %d: %s", len(resp.Rows), tc.want, rr.Body.String())
			}
		})
	}
}
