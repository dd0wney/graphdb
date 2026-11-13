package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAPI_Query_CallStopsAtQueryTimeout pins v1.4-procedures-ignore-context at
// the REST surface. The status alone cannot tell a fixed build from a broken
// one: the executor checks the context after each step, so before the fix a
// slow CALL still answered 408, but only after it had run to completion. The
// elapsed time is the assertion that matters.
//
// The graph is a hub joined both ways to its leaves. With damping 1 the
// PageRank scores swap on every iteration and never converge, so the CALL runs
// all its iterations: about 60 seconds for 1,000,000 on a development machine.
func TestAPI_Query_CallStopsAtQueryTimeout(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	hub, err := server.graph.CreateNode([]string{"Hub"}, nil)
	if err != nil {
		t.Fatalf("CreateNode hub: %v", err)
	}
	for i := 0; i < 200; i++ {
		leaf, err := server.graph.CreateNode([]string{"Leaf"}, nil)
		if err != nil {
			t.Fatalf("CreateNode leaf: %v", err)
		}
		if _, err := server.graph.CreateEdge(hub.ID, leaf.ID, "L", nil, 1.0); err != nil {
			t.Fatalf("CreateEdge out: %v", err)
		}
		if _, err := server.graph.CreateEdge(leaf.ID, hub.ID, "L", nil, 1.0); err != nil {
			t.Fatalf("CreateEdge back: %v", err)
		}
	}

	oneSecond := 1 // the smallest timeout_seconds the handler accepts
	body, err := json.Marshal(QueryRequest{
		Query:          "CALL algo.pageRank(1.0, 1000000)",
		TimeoutSeconds: &oneSecond,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	start := time.Now()
	server.handleQuery(rr, req)
	elapsed := time.Since(start)
	t.Logf("POST /query answered %d after %v", rr.Code, elapsed)

	if rr.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusRequestTimeout, rr.Body.String())
	}
	if elapsed > 10*time.Second {
		t.Errorf("handler took %v to answer a 1s timeout; the CALL ignores its context", elapsed)
	}
}
