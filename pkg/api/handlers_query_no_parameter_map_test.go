package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/tenant"
)

// postQueryBody sends a raw request body to the query handler. The raw form is
// necessary to send a request with no "parameters" field and one with null.
func postQueryBody(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), "default"))
	rr := httptest.NewRecorder()
	server.handleQuery(rr, req)
	return rr
}

// queryRows returns only the "rows" of a response. The full body holds a run
// time, so two equal results have different bodies.
func queryRows(t *testing.T, server *Server, q string) string {
	t.Helper()
	var response struct {
		Rows json.RawMessage `json:"rows"`
	}
	raw := postQueryJSON(t, server, q, "").Body.Bytes()
	if err := json.Unmarshal(raw, &response); err != nil || response.Rows == nil {
		t.Fatalf("the state query gave no rows field: %s", raw)
	}
	return string(response.Rows)
}

// noMapStoreState is the number of nodes and the number of :Person nodes that
// a SET marked. A DETACH DELETE changes the first and a SET changes the second.
func noMapStoreState(t *testing.T, server *Server) string {
	t.Helper()
	return queryRows(t, server, `MATCH (n) RETURN count(n) AS c`) +
		queryRows(t, server, `MATCH (p:Person) WHERE p.z = 1 RETURN count(p) AS c`)
}

func seedNoMapStore(t *testing.T, server *Server) {
	t.Helper()
	for id := 1; id <= 3; id++ {
		q := fmt.Sprintf(`CREATE (:Person {id: %d})`, id)
		if rr := postQueryJSON(t, server, q, ""); rr.Code != http.StatusOK {
			t.Fatalf("seed %s: status %d: %s", q, rr.Code, rr.Body.String())
		}
	}
}

// A write query that names a parameter must answer 400 and write nothing when
// the request gives no value for the parameter: no "parameters" field, null,
// an empty map, or a map with another name. On v1.12.0 the parameter read as
// null and "$missing IS NULL DETACH DELETE p" deleted every :Person node.
func TestQuery_MissingParameterWritesNothingForEachBodyForm(t *testing.T) {
	queries := []string{
		`MATCH (p:Person) WHERE $missing IS NULL DETACH DELETE p`,
		`MATCH (p:Person) WHERE NOT p.id = $missing DETACH DELETE p`,
		`MATCH (p:Person) WHERE NOT p.id IN [1, $missing] DETACH DELETE p`,
		`MATCH (p:Person) SET p.z = 1 RETURN $missing AS x`,
		`MATCH (p:Person) WITH p, $missing AS w WHERE w IS NULL DETACH DELETE p`,
		`MATCH (p:Person) SET p.z = 1 WITH p MATCH (q:Person) WHERE $missing IS NULL DETACH DELETE q`,
		`MATCH (p:Person) RETURN p.id AS r UNION MATCH (q:Person) WHERE $missing IS NULL DETACH DELETE q RETURN q.id AS r`,
		`MATCH (p:Person {id: $missing}) DETACH DELETE p`,
		`CREATE (:Made {v: $missing})`,
		`PROFILE MATCH (p:Person) WHERE $missing IS NULL DETACH DELETE p`,
	}
	bodies := map[string]string{
		"no parameters field": `{"query": %s}`,
		"parameters null":     `{"query": %s, "parameters": null}`,
		"parameters empty":    `{"query": %s, "parameters": {}}`,
		"another name":        `{"query": %s, "parameters": {"w": 1}}`,
	}
	for _, q := range queries {
		for form, template := range bodies {
			t.Run(form+"/"+q, func(t *testing.T) {
				server, cleanup := setupTestServer(t)
				defer cleanup()
				seedNoMapStore(t, server)
				before := noMapStoreState(t, server)

				quoted, err := json.Marshal(q)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				rr := postQueryBody(t, server, fmt.Sprintf(template, quoted))

				if rr.Code != http.StatusBadRequest {
					t.Errorf("status %d, want 400: %s", rr.Code, rr.Body.String())
				}
				if !strings.Contains(rr.Body.String(), "missing parameter: $missing") {
					t.Errorf("the body does not name the parameter: %s", rr.Body.String())
				}
				if after := noMapStoreState(t, server); after != before {
					t.Errorf("the refused query changed the store: before %s, after %s", before, after)
				}
			})
		}
	}
}

// The test above reports "the store did not change". This control proves that
// the state read can report a change: the same store, a literal in the place of
// the parameter, and the state must move.
func TestQuery_NoMapStoreStateControl(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	seedNoMapStore(t, server)

	start := noMapStoreState(t, server)
	if start != `[{"c":3}][{"c":0}]` {
		t.Fatalf("the seed is not visible: %s", start)
	}
	if again := noMapStoreState(t, server); again != start {
		t.Fatalf("two reads with no query between them differ: %s and %s", start, again)
	}
	if rr := postQueryBody(t, server, `{"query": "MATCH (p:Person) SET p.z = 1"}`); rr.Code != http.StatusOK {
		t.Fatalf("literal SET: status %d: %s", rr.Code, rr.Body.String())
	}
	if got := noMapStoreState(t, server); got != `[{"c":3}][{"c":3}]` {
		t.Fatalf("a literal SET did not move the state: %s", got)
	}
	if rr := postQueryBody(t, server, `{"query": "MATCH (p:Person) WHERE 1 IS NOT NULL DETACH DELETE p"}`); rr.Code != http.StatusOK {
		t.Fatalf("literal DETACH DELETE: status %d: %s", rr.Code, rr.Body.String())
	}
	if got := noMapStoreState(t, server); got != `[{"c":0}][{"c":0}]` {
		t.Fatalf("a literal DETACH DELETE did not move the state: %s", got)
	}
}
