package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// GitHub #765: UNWIND over a list parameter answered 400 "UNWIND expression
// must be a property reference", so a client sent one request per item. This
// is the request from the issue, through the REST handler, with JSON
// parameters that arrive as []any and map[string]any.
func TestHandleQuery_UnwindListParameter(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	cases := []struct {
		name, body, wantRows string
	}{
		{"scalars", `{"query": "UNWIND $xs AS x RETURN x", "parameters": {"xs": [1, 2]}}`,
			`[{"x":1},{"x":2}]`},
		{"maps with field access", `{"query": "UNWIND $items AS item RETURN item.id AS id, item.text AS text", "parameters": {"items": [{"id": "a", "text": "one"}, {"id": "b", "text": "two"}]}}`,
			`[{"id":"a","text":"one"},{"id":"b","text":"two"}]`},
		{"literal list", `{"query": "UNWIND [3, 4] AS x RETURN x"}`,
			`[{"x":3},{"x":4}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := postQueryBody(t, server, tc.body)
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d, want 200: %s", rr.Code, rr.Body.String())
			}
			var response struct {
				Rows json.RawMessage `json:"rows"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Fatalf("parse response: %v: %s", err, rr.Body.String())
			}
			var got, want any
			if err := json.Unmarshal(response.Rows, &got); err != nil {
				t.Fatalf("parse rows %s: %v", response.Rows, err)
			}
			if err := json.Unmarshal([]byte(tc.wantRows), &want); err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("rows = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}
