package api

import (
	"net/http"
	"strings"
	"testing"
)

// A missing parameter is the caller's fault, in whichever clause it sits. A
// pattern parameter answered 400 and a WHERE parameter answered 500 with the
// text hidden, for the same mistake.
func TestQuery_MissingParameterAnswers400WithItsName(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for name, q := range map[string]string{
		"pattern":      `MATCH (p:Person {id: $v}) RETURN p.name`,
		"WHERE":        `MATCH (p:Person) WHERE p.id = $v RETURN p.name`,
		"RETURN":       `MATCH (p:Person) RETURN p.id + $v AS x`,
		"after WITH":   `MATCH (a:Person) WITH a MATCH (b:Person) WHERE b.id = $v RETURN b.name`,
		"UNION second": `MATCH (a:Person) RETURN a.name AS n UNION MATCH (b:Person) WHERE b.id = $v RETURN b.name AS n`,
	} {
		t.Run(name, func(t *testing.T) {
			// A different parameter is supplied, so the request takes the
			// ExecuteWithParams path where the check lives.
			rr := postQueryJSON(t, server, q, `{"other": 1}`)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "$v") {
				t.Errorf("body does not name the parameter: %s", rr.Body.String())
			}
		})
	}
}
