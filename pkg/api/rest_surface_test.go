package api

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGolden rewrites the transcript instead of comparing against it.
// Run: go test ./pkg/api/ -run TestRESTSurface -update-golden
var updateGolden = flag.Bool("update-golden", false, "rewrite pkg/api/testdata/rest_surface.golden")

// TestRESTSurface pins what the REST API does, as one readable transcript.
//
// The other tests in this package each check one behaviour, and reading all of
// them is the only way to answer "what does this API do". This test answers it
// in one file: every step is a request and the response it produced, in order,
// and the whole thing fits on a screen or two.
//
// It is an approval test, so it asserts nothing about what is correct. It
// asserts only that nothing changed. That is the point. A diff in
// testdata/rest_surface.golden is a change to the API's observable behaviour,
// and it has to be looked at and explained rather than discovered by a
// consumer. The risk of the technique is approving a transcript without
// reading it, so the transcript is written to be read.
//
// Every consumer contract in docs/CONSUMER_CONTRACTS.md was found by driving a
// real consumer. This is the cheap standing version of that drive.
func TestRESTSurface(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tr := &transcript{t: t, server: server}

	tr.section("A node is created, read back, updated and deleted")
	tr.do("POST", "/nodes", "default", map[string]any{
		"labels": []string{"Person"}, "properties": map[string]any{"name": "ada", "born": 1815},
	})
	tr.do("GET", "/nodes/1", "default", nil)
	tr.do("PUT", "/nodes/1", "default", map[string]any{
		"properties": map[string]any{"name": "ada", "born": 1815, "field": "analysis"},
	})
	tr.do("GET", "/nodes/1", "default", nil)
	tr.do("DELETE", "/nodes/1", "default", nil)
	tr.do("GET", "/nodes/1", "default", nil)

	tr.section("A label listing returns the nodes and their properties")
	tr.do("POST", "/nodes", "default", map[string]any{
		"labels": []string{"City"}, "properties": map[string]any{"name": "dublin"},
	})
	tr.do("POST", "/nodes", "default", map[string]any{
		"labels": []string{"City"}, "properties": map[string]any{"name": "cork"},
	})
	tr.do("GET", "/nodes?label=City", "default", nil)
	tr.do("GET", "/nodes?label=Nowhere", "default", nil)

	tr.section("An edge joins two nodes, and a traversal finds the neighbour")
	tr.do("POST", "/edges", "default", map[string]any{
		"from_node_id": 2, "to_node_id": 3, "type": "ROAD_TO",
		"properties": map[string]any{"km": 260},
	})
	tr.do("GET", "/edges/1", "default", nil)
	tr.do("POST", "/traverse", "default", map[string]any{
		"start_node_id": 2, "max_depth": 1, "direction": "outgoing",
	})

	tr.section("A second tenant cannot see the first tenant's data")
	tr.do("GET", "/nodes/2", "other", nil)
	tr.do("GET", "/nodes?label=City", "other", nil)

	tr.section("Rejections")
	tr.do("POST", "/nodes", "default", map[string]any{"labels": []string{}})
	tr.do("GET", "/nodes/99999", "default", nil)
	tr.do("DELETE", "/nodes/99999", "default", nil)

	golden := filepath.Join("testdata", "rest_surface.golden")
	got := tr.String()

	if *updateGolden {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatalf("write %s: %v", golden, err)
		}
		t.Logf("wrote %s (%d bytes)", golden, len(got))
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read %s: %v\nCreate it with: go test ./pkg/api/ -run TestRESTSurface -update-golden", golden, err)
	}
	if string(want) != got {
		t.Errorf("the REST surface changed.\n"+
			"If the change is intended, re-approve it with:\n"+
			"    go test ./pkg/api/ -run TestRESTSurface -update-golden\n"+
			"and say in the pull request what changed and why.\n\n%s",
			firstDifference(string(want), got))
	}
}

// transcript records each request and its response in a form meant to be read.
type transcript struct {
	t      *testing.T
	server *Server
	b      strings.Builder
}

func (tr *transcript) section(title string) {
	if tr.b.Len() > 0 {
		tr.b.WriteString("\n")
	}
	fmt.Fprintf(&tr.b, "# %s\n\n", title)
}

// route maps a path to the handler the real mux gives it.
//
// The mux in server.go wraps each of these in requireAuth(withTenant(...)).
// Every test in this package calls the handler directly and supplies the tenant
// through the context instead, and this does the same. So the transcript covers
// the handlers and the method dispatch inside them, and it does NOT cover
// authentication or the tenant middleware. Those need their own tests, and they
// have them.
func (tr *transcript) route(path string) http.HandlerFunc {
	tr.t.Helper()
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	switch {
	case path == "/nodes":
		return tr.server.handleNodes
	case strings.HasPrefix(path, "/nodes/"):
		return tr.server.handleNode
	case path == "/edges":
		return tr.server.handleEdges
	case strings.HasPrefix(path, "/edges/"):
		return tr.server.handleEdge
	case path == "/traverse":
		return tr.server.handleTraversal
	}
	tr.t.Fatalf("no handler for %q: add it to route", path)
	return nil
}

// do performs one request and appends the exchange to the transcript.
func (tr *transcript) do(method, path, tenantID string, body any) {
	tr.t.Helper()

	req := reqWithTenant(tr.t, method, path, body, tenantID)
	rr := httptest.NewRecorder()
	tr.route(path)(rr, req)

	line := method + " " + path
	if tenantID != "default" {
		line += "   (tenant " + tenantID + ")"
	}
	fmt.Fprintf(&tr.b, "%s\n", line)
	if body != nil {
		fmt.Fprintf(&tr.b, "  -> %s\n", compact(tr.t, body))
	}
	fmt.Fprintf(&tr.b, "  %d %s\n\n", rr.Code, normalise(rr.Body.String()))
}

func (tr *transcript) String() string { return tr.b.String() }

func compact(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// normalise makes a response comparable across runs. Anything that changes
// without the API changing has to be removed here, and every removal is a
// statement that the field is not part of the contract.
func normalise(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no body)"
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s // not JSON: record it verbatim
	}
	stripVolatile(v)
	b, err := json.Marshal(v)
	if err != nil {
		return s
	}
	return string(b)
}

// volatileFields change on every run and say nothing about behaviour.
var volatileFields = map[string]string{
	"created_at": "<time>",
	"updated_at": "<time>",
	"timestamp":  "<time>",
	"took_ms":    "<ms>",
	"duration":   "<duration>",
	// /traverse reports how long it took, in the response body. It is not a
	// contract, and left alone it changes the transcript on every run.
	"time": "<elapsed>",
}

func stripVolatile(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k := range t {
			if placeholder, ok := volatileFields[k]; ok {
				t[k] = placeholder
				continue
			}
			stripVolatile(t[k])
		}
	case []any:
		for i := range t {
			stripVolatile(t[i])
		}
	}
}

// firstDifference reports the first line that differs, with a little context.
// A whole-file diff of a transcript is unreadable, and an unreadable failure is
// one that gets re-approved without being read.
func firstDifference(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "first difference at line %d:\n", i+1)
		for j := max(0, i-3); j < i; j++ {
			fmt.Fprintf(&b, "     %s\n", w[j])
		}
		fmt.Fprintf(&b, "  -  %s\n", wl)
		fmt.Fprintf(&b, "  +  %s\n", gl)
		return b.String()
	}
	return "the files differ only in trailing content"
}
