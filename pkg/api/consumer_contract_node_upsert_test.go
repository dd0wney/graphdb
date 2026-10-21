package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// The tests in this file decode every response through literal JSON keys, not
// through the Go response structs. mapper reads these keys by name, so a
// rename of a Go field or a json tag must fail here.

func registerHostRule(t *testing.T, server *Server) {
	t.Helper()
	if err := server.graph.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "secret_host_rule", Label: "Host", PropertyKey: "ip",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
}

func postRaw(t *testing.T, server *Server, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	if strings.HasPrefix(target, "/nodes/batch") {
		server.handleBatchNodes(rr, req)
	} else {
		server.handleNodes(rr, req)
	}
	return rr
}

func decodeMap(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, rr.Body.String())
	}
	return m
}

func hostNode(ip string, extra map[string]any) NodeRequest {
	props := map[string]any{"ip": ip}
	for k, v := range extra {
		props[k] = v
	}
	return NodeRequest{Labels: []string{"Host"}, Properties: props}
}

func countHosts(t *testing.T, server *Server, ip string) int {
	t.Helper()
	nodes, err := server.graph.FindNodesByLabelAcrossTenants("Host")
	if err != nil {
		t.Fatalf("FindNodesByLabelAcrossTenants: %v", err)
	}
	n := 0
	for _, node := range nodes {
		if v, ok := node.Properties["ip"]; ok {
			if s, err := v.AsString(); err == nil && s == ip {
				n++
			}
		}
	}
	return n
}

// CONSUMER CONTRACT: CC27-upsert-created-flag — mapper (coord graphdb:v1.4-rest-upsert-node)
//
// mapper-6b ingests one host from two assessors. POST /nodes?on_conflict=merge
// answers 201 with "created": true when it makes the node, and 200 with
// "created": false when the key already exists. A matched upsert merges: a
// property the request omits stays, a property the request sends overrides.
func TestNodeUpsert_CreatedThenMatchedReportsTheFlag(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	registerHostRule(t, server)

	first := postRaw(t, server, "/nodes?on_conflict=merge", hostNode("10.0.0.1", map[string]any{"os": "linux"}))
	if first.Code != http.StatusCreated {
		t.Fatalf("first upsert: status = %d, want 201, body=%s", first.Code, first.Body.String())
	}
	b1 := decodeMap(t, first)
	if b1["created"] != true {
		t.Errorf(`first upsert "created" = %v, want true`, b1["created"])
	}
	id, ok := b1["id"].(float64)
	if !ok {
		t.Fatalf(`first upsert has no numeric "id": %s`, first.Body.String())
	}

	second := postRaw(t, server, "/nodes?on_conflict=merge", hostNode("10.0.0.1", map[string]any{"hostname": "h1"}))
	if second.Code != http.StatusOK {
		t.Fatalf("second upsert: status = %d, want 200, body=%s", second.Code, second.Body.String())
	}
	b2 := decodeMap(t, second)
	if b2["created"] != false {
		t.Errorf(`second upsert "created" = %v, want false`, b2["created"])
	}
	if b2["id"] != id {
		t.Errorf(`second upsert "id" = %v, want %v`, b2["id"], id)
	}
	props, _ := b2["properties"].(map[string]any)
	if props["os"] != "linux" {
		t.Errorf(`merge dropped "os": properties = %v`, props)
	}
	if props["hostname"] != "h1" {
		t.Errorf(`merge did not set "hostname": properties = %v`, props)
	}

	// A repeat that changes nothing is still a match.
	third := postRaw(t, server, "/nodes?on_conflict=merge", hostNode("10.0.0.1", map[string]any{"hostname": "h1"}))
	if third.Code != http.StatusOK || decodeMap(t, third)["created"] != false {
		t.Errorf("no-change upsert: status = %d, body=%s, want 200 with created=false", third.Code, third.Body.String())
	}
	if got := countHosts(t, server, "10.0.0.1"); got != 1 {
		t.Errorf("Host nodes with ip 10.0.0.1 = %d, want 1", got)
	}
}

// CONSUMER CONTRACT: CC28-upsert-concurrent-one-node — mapper (coord graphdb:v1.4-rest-upsert-node)
//
// Two assessors report one host at the same time. Exactly one request gets 201
// and the other gets 200, and one node exists. This is the race that the
// create, 409, lookup, update sequence cannot close.
func TestNodeUpsert_ConcurrentRequestsMakeOneNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	registerHostRule(t, server)

	for round := 0; round < 25; round++ {
		ip := "10.1.0." + string(rune('a'+round))
		start := make(chan struct{})
		var wg sync.WaitGroup
		codes := make([]int, 2)
		created := make([]any, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				rr := postRaw(t, server, "/nodes?on_conflict=merge", hostNode(ip, map[string]any{"by": i}))
				codes[i] = rr.Code
				var m map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &m); err == nil {
					created[i] = m["created"]
				}
			}(i)
		}
		close(start)
		wg.Wait()

		n201, n200 := 0, 0
		for i, c := range codes {
			switch c {
			case http.StatusCreated:
				n201++
				if created[i] != true {
					t.Errorf("round %d: 201 body created = %v, want true", round, created[i])
				}
			case http.StatusOK:
				n200++
				if created[i] != false {
					t.Errorf("round %d: 200 body created = %v, want false", round, created[i])
				}
			}
		}
		if n201 != 1 || n200 != 1 {
			t.Fatalf("round %d: statuses = %v, want exactly one 201 and one 200", round, codes)
		}
		if got := countHosts(t, server, ip); got != 1 {
			t.Fatalf("round %d: Host nodes with ip %s = %d, want 1", round, ip, got)
		}
	}
}

// CONSUMER CONTRACT: CC29-upsert-refusals-are-specific — mapper (coord graphdb:v1.4-rest-upsert-node)
//
// mapper tells its user why an upsert was refused. A label with no rule is a
// 400. A key held by two nodes is a 409 that lists "conflicting_node_ids". A
// label the stored node lacks is a 409 with "conflicting_node_id" and
// "missing_labels". No body names the rule.
func TestNodeUpsert_RefusalsCarryTheirDetail(t *testing.T) {
	t.Run("no rule covers the labels", func(t *testing.T) {
		server, cleanup := setupTestServer(t)
		defer cleanup()
		rr := postRaw(t, server, "/nodes?on_conflict=merge", NodeRequest{
			Labels: []string{"Widget"}, Properties: map[string]any{"sku": "s1"},
		})
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body=%s", rr.Code, rr.Body.String())
		}
		msg, _ := decodeMap(t, rr)["message"].(string)
		if !strings.Contains(msg, "on_conflict=merge needs one") {
			t.Errorf(`"message" = %q, want the no-rule sentence`, msg)
		}
	})

	t.Run("two nodes hold the key", func(t *testing.T) {
		server, cleanup := setupTestServer(t)
		defer cleanup()
		var ids []float64
		for i := 0; i < 2; i++ {
			n, err := server.graph.CreateNodeWithTenant(tenant.DefaultTenantID, []string{"Host"},
				map[string]storage.Value{"ip": storage.StringValue("10.2.0.1")})
			if err != nil {
				t.Fatalf("seed node: %v", err)
			}
			ids = append(ids, float64(n.ID))
		}
		registerHostRule(t, server)

		rr := postRaw(t, server, "/nodes?on_conflict=merge", hostNode("10.2.0.1", nil))
		if rr.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409, body=%s", rr.Code, rr.Body.String())
		}
		body := decodeMap(t, rr)
		got, ok := body["conflicting_node_ids"].([]any)
		if !ok || len(got) != 2 {
			t.Fatalf(`"conflicting_node_ids" = %v, want two IDs; body=%s`, body["conflicting_node_ids"], rr.Body.String())
		}
		want := map[float64]bool{ids[0]: true, ids[1]: true}
		for _, g := range got {
			if f, _ := g.(float64); !want[f] {
				t.Errorf("conflicting_node_ids has %v, want one of %v", g, ids)
			}
		}
		if strings.Contains(rr.Body.String(), "secret_host_rule") {
			t.Errorf("409 body names the rule: %s", rr.Body.String())
		}
	})

	t.Run("a request label is missing on the stored node", func(t *testing.T) {
		server, cleanup := setupTestServer(t)
		defer cleanup()
		registerHostRule(t, server)
		seed := postRaw(t, server, "/nodes", hostNode("10.3.0.1", nil))
		if seed.Code != http.StatusCreated {
			t.Fatalf("seed create = %d, body=%s", seed.Code, seed.Body.String())
		}
		seedID := decodeMap(t, seed)["id"]

		rr := postRaw(t, server, "/nodes?on_conflict=merge", NodeRequest{
			Labels: []string{"Host", "Server"}, Properties: map[string]any{"ip": "10.3.0.1"},
		})
		if rr.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409, body=%s", rr.Code, rr.Body.String())
		}
		body := decodeMap(t, rr)
		if body["conflicting_node_id"] != seedID {
			t.Errorf(`"conflicting_node_id" = %v, want %v`, body["conflicting_node_id"], seedID)
		}
		missing, ok := body["missing_labels"].([]any)
		if !ok || len(missing) != 1 || missing[0] != "Server" {
			t.Errorf(`"missing_labels" = %v, want ["Server"]`, body["missing_labels"])
		}
		if strings.Contains(rr.Body.String(), "secret_host_rule") {
			t.Errorf("409 body names the rule: %s", rr.Body.String())
		}
	})
}

// CONSUMER CONTRACT: CC30-batch-upsert-reports-matched — mapper (coord graphdb:v1.4-rest-upsert-node)
//
// POST /nodes/batch?on_conflict=merge keeps status 201 and the "created"
// count (it counts nodes made). It adds "matched", and each element of
// "nodes" carries its own "created". Two items with one key give one create
// and one match, in request order. An item error never names a rule.
func TestBatchNodeUpsert_ReportsCreatedAndMatched(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	registerHostRule(t, server)

	rr := postRaw(t, server, "/nodes/batch?on_conflict=merge", BatchNodeRequest{Nodes: []NodeRequest{
		hostNode("10.4.0.1", map[string]any{"os": "linux"}),
		hostNode("10.4.0.1", map[string]any{"hostname": "h4"}),
		{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "s1"}}, // no rule
	}})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	body := decodeMap(t, rr)
	if body["created"] != float64(1) || body["matched"] != float64(1) || body["failed"] != float64(1) {
		t.Errorf("created=%v matched=%v failed=%v, want 1, 1, 1; body=%s",
			body["created"], body["matched"], body["failed"], rr.Body.String())
	}
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf(`"nodes" = %v, want two elements`, body["nodes"])
	}
	wantCreated := []bool{true, false}
	for i, n := range nodes {
		m, _ := n.(map[string]any)
		if m["created"] != wantCreated[i] {
			t.Errorf(`nodes[%d]["created"] = %v, want %v`, i, m["created"], wantCreated[i])
		}
	}
	errs, _ := body["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf(`"errors" = %v, want one entry`, body["errors"])
	}
	if e, _ := errs[0].(map[string]any); e["index"] != float64(2) {
		t.Errorf(`errors[0]["index"] = %v, want 2`, e["index"])
	}
	if got := countHosts(t, server, "10.4.0.1"); got != 1 {
		t.Errorf("Host nodes with ip 10.4.0.1 = %d, want 1", got)
	}
}

// CONSUMER CONTRACT: CC30-batch-upsert-reports-matched — mapper (coord graphdb:v1.4-rest-upsert-node)
//
// A batch upsert item that meets two rules is refused, and its error text must
// not carry the rule names that storage's error contains (ADR 0001, R4).
func TestBatchNodeUpsert_ItemErrorsNameNoRule(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	for _, r := range []storage.UniquenessRule{
		{Name: "secret_rule_a", Label: "Widget", PropertyKey: "sku"},
		{Name: "secret_rule_b", Label: "Widget", PropertyKey: "serial"},
	} {
		if err := server.graph.RegisterUniquenessRule(r); err != nil {
			t.Fatalf("RegisterUniquenessRule: %v", err)
		}
	}
	rr := postRaw(t, server, "/nodes/batch?on_conflict=merge", BatchNodeRequest{Nodes: []NodeRequest{
		{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "a", "serial": "b"}},
	}})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "secret_rule") {
		t.Errorf("batch body names a rule: %s", rr.Body.String())
	}
	if body := decodeMap(t, rr); body["failed"] != float64(1) {
		t.Errorf(`"failed" = %v, want 1`, body["failed"])
	}
}

// CONSUMER CONTRACT: CC31-on-conflict-values — mapper (coord graphdb:v1.4-rest-upsert-node)
//
// Without on_conflict=merge, POST /nodes keeps today's behaviour: 201, then
// 409 for the same key. on_conflict=error means the same. Any other value is
// a 400 that names the accepted values, on both routes. The batch response
// has no "matched" key unless merge was asked for.
func TestNodeUpsert_OnConflictValues(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	registerHostRule(t, server)

	if rr := postRaw(t, server, "/nodes", hostNode("10.5.0.1", nil)); rr.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	for _, target := range []string{"/nodes", "/nodes?on_conflict=error"} {
		rr := postRaw(t, server, target, hostNode("10.5.0.1", nil))
		if rr.Code != http.StatusConflict {
			t.Errorf("POST %s duplicate = %d, want 409, body=%s", target, rr.Code, rr.Body.String())
		}
		if _, has := decodeMap(t, rr)["created"]; has {
			t.Errorf("POST %s 409 body has a created key", target)
		}
	}

	single := postRaw(t, server, "/nodes?on_conflict=replace", hostNode("10.5.0.2", nil))
	batch := postRaw(t, server, "/nodes/batch?on_conflict=replace", BatchNodeRequest{Nodes: []NodeRequest{hostNode("10.5.0.2", nil)}})
	for target, rr := range map[string]*httptest.ResponseRecorder{"/nodes": single, "/nodes/batch": batch} {
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("POST %s?on_conflict=replace = %d, want 400, body=%s", target, rr.Code, rr.Body.String())
		}
		msg, _ := decodeMap(t, rr)["message"].(string)
		if !strings.Contains(msg, "error") || !strings.Contains(msg, "merge") {
			t.Errorf("POST %s message = %q, want it to name error and merge", target, msg)
		}
	}
	if got := countHosts(t, server, "10.5.0.2"); got != 0 {
		t.Errorf("a refused on_conflict value wrote %d nodes", got)
	}

	rr := postRaw(t, server, "/nodes/batch", BatchNodeRequest{Nodes: []NodeRequest{hostNode("10.5.0.3", nil)}})
	if rr.Code != http.StatusCreated {
		t.Fatalf("batch = %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	body := decodeMap(t, rr)
	if _, has := body["matched"]; has {
		t.Errorf("a batch create body has a matched key: %s", rr.Body.String())
	}
	if nodes, _ := body["nodes"].([]any); len(nodes) == 1 {
		if m, _ := nodes[0].(map[string]any); m["created"] != nil {
			t.Errorf("a batch create node has a created key: %v", m)
		}
	}
}
