package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// A parameter in a list literal came back as {"Name":"v"}.
func TestQuery_ListLiteralParameterHasItsValue(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := postQueryJSON(t, server, `RETURN [1, $v] AS l`, `{"v": 1}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Rows []struct {
			L json.RawMessage `json:"l"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows = %d: %s", len(resp.Rows), rr.Body.String())
	}
	if got := string(resp.Rows[0].L); got != "[1,1]" {
		t.Errorf("l = %s, want [1,1]", got)
	}
}
