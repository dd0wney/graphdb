package api

import (
	"net/http"
	"strings"
	"testing"
)

// A CALL that yields no row must still answer "columns":[] and not null, so a
// client can iterate the field the way it does for every other response.
func TestAPI_Query_CallWithNoRowsHasEmptyColumnsArray(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := makeQueryRequest(t, server, "MATCH (x:NoSuch) CALL algo.pageRank()")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, `"columns":[]`) {
		t.Errorf("body %s does not contain \"columns\":[]", body)
	}
}
