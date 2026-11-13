package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A query the executor refuses because of its text is the caller's mistake.
// POST /query used to answer 500 "query execution failed" for it, so the
// caller never saw which name was wrong.
func TestAPI_Query_RefusalIs400WithMessage(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("a"),
		"age":  storage.IntValue(3),
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	cases := []struct{ query, want string }{
		{`MATCH (p:Person) RETURN q.name`, "undefined variable: q"},
		{`RETURN nosuchfn(1) AS x`, "unknown function: nosuchfn"},
		{`CALL algo.nope()`, "unknown procedure: algo.nope"},
		{`CALL algo.pageRank() YIELD nodeId RETURN nodeId AS n`, `has no column "nodeId"`},
		{`MATCH (p:Person) WITH p.age AS age, count(p) RETURN age AS age`, "WITH"},
		{`RETURN 1 / 0 AS x`, "division by zero"},
		{`MATCH (p:Person) RETURN p.name + 1 AS x`, "cannot apply + to string and integer"},
		{`MATCH (p:Person) WHERE p.name AND true RETURN p.name`, "a boolean is necessary here, but the value is a string"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			rr := makeQueryRequest(t, server, tc.query)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rr.Code, rr.Body.String())
			}
			// Decode first: JSON escapes the quotes in the message.
			var body struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %q: %v", rr.Body.String(), err)
			}
			if !strings.Contains(body.Message, tc.want) {
				t.Errorf("message %q does not contain %q", body.Message, tc.want)
			}
			if !strings.HasPrefix(body.Message, "Query error: ") {
				t.Errorf("message %q lacks the prefix %q", body.Message, "Query error: ")
			}
		})
	}
}

func TestAPI_Query_ParserErrorStill400(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	rr := makeQueryRequest(t, server, `MATCH (p:Person RETURN p`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Parser error:") {
		t.Errorf("body %q lacks %q", rr.Body.String(), "Parser error:")
	}
}

// An executor error that is not a refusal keeps the 500 and the hidden text.
// A procedure's own argument error is such an error: algo.kHop reads its
// first argument as a node ID and fails for a string. (Seeded as
// v1.4-procedure-argument-errors-are-500.)
func TestAPI_Query_NonRefusalExecutorErrorStays500(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	rr := makeQueryRequest(t, server, `CALL algo.kHop('x', 1)`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "query execution failed") {
		t.Errorf("body %q lacks the hidden text", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "numeric node ID") {
		t.Errorf("body %q shows the procedure message", rr.Body.String())
	}
}
