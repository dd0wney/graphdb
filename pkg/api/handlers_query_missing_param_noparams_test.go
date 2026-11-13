package api

import (
	"net/http"
	"strings"
	"testing"
)

// With no "parameters" field, or an empty one, the handler takes the entry
// point with no parameters. It returned zero rows, or a null row, for a
// parameter in an expression.
func TestQuery_MissingParameterWithoutParametersAnswers400(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for name, q := range map[string]string{
		"WHERE":      `MATCH (p:Person) WHERE p.id = $missing RETURN p.name AS n`,
		"RETURN":     `RETURN $missing AS x`,
		"WITH WHERE": `MATCH (p:Person) WITH p WHERE p.id = $missing RETURN p.name AS n`,
		"SET":        `MATCH (p:Person {id: 2}) SET p.z = $missing RETURN p.z AS n`,
	} {
		for _, raw := range []string{"", `{}`} {
			t.Run(name+"/parameters="+raw, func(t *testing.T) {
				rr := postQueryJSON(t, server, q, raw)
				if rr.Code != http.StatusBadRequest {
					t.Fatalf("status %d, want 400: %s", rr.Code, rr.Body.String())
				}
				if !strings.Contains(rr.Body.String(), "$missing") {
					t.Errorf("body does not name the parameter: %s", rr.Body.String())
				}
			})
		}
	}
}
