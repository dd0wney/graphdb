package api

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// TestRESTMatchesReferenceModel drives random sequences of node operations
// through the REST handlers and compares the result with a plain map.
//
// Every consumer contract in docs/CONSUMER_CONTRACTS.md was found by driving a
// real consumer end to end: CC1, CC4 and CC6 are all "a write went in through
// one path and a read through another did not see it". That is a property a
// generator can check without a consumer, without a corpus, and without an
// embedder, and this repository had no such test at any layer above storage.
//
// The model is the whole specification: a node exists with the labels and
// properties it was last given, until it is deleted. After every operation the
// test asks the API about every id it has ever seen, and about every label,
// and both answers must agree with the map.
//
// Edges are not modelled yet. The node surface carries the CC1, CC4 and CC6
// shape, which is what this was written for; adding edges and traversal is the
// obvious next widening and needs its own reachability rules in the model.
//
// Teeth-proven: dropping the label filter in listNodes (handlers_nodes.go:91,
// the CC5 shape of a filter silently not applied) fails at step 1 of seed 0
// with "label City listed [1], the model says []". Note that the same edit to
// countNodes at line 46 does NOT fail it — that is the HEAD path, which this
// test does not call, and a teeth-proof against it would have proved nothing.
func TestRESTMatchesReferenceModel(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a server per seed")
	}

	const (
		seeds = 16
		steps = 60
	)

	for seed := int64(0); seed < seeds; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()

			m := newRESTModel(server, t)
			rng := rand.New(rand.NewSource(seed))

			for step := 1; step <= steps; step++ {
				op := m.apply(rng, step)
				m.checkEveryNode(step, op)
				m.checkEveryLabel(step, op)
			}
		})
	}
}

var modelLabels = []string{"Person", "City", "Thing"}

type modelNode struct {
	label string
	value int
}

type restModel struct {
	t      *testing.T
	server *Server
	// live is the specification: what the API must say now.
	live map[uint64]modelNode
	// seen is every id ever created, so a deleted node is checked for
	// absence rather than forgotten.
	seen []uint64
}

func newRESTModel(server *Server, t *testing.T) *restModel {
	return &restModel{t: t, server: server, live: map[uint64]modelNode{}}
}

// apply performs one random operation and returns a description of it, which
// goes into any failure message so a failing seed can be read.
func (m *restModel) apply(rng *rand.Rand, step int) string {
	m.t.Helper()

	live := make([]uint64, 0, len(m.live))
	for id := range m.live {
		live = append(live, id)
	}
	sort.Slice(live, func(i, j int) bool { return live[i] < live[j] })

	// Bias towards create while there is little to work with, so the first
	// steps do not spend themselves on an empty store.
	roll := rng.Intn(100)
	if len(live) == 0 || roll < 45 {
		label := modelLabels[rng.Intn(len(modelLabels))]
		value := rng.Intn(1000)
		rr := m.call(http.MethodPost, "/nodes", map[string]any{
			"labels":     []string{label},
			"properties": map[string]any{"v": value},
		})
		if rr.Code != http.StatusCreated {
			m.t.Fatalf("step %d: POST /nodes gave %d: %s", step, rr.Code, rr.Body.String())
		}
		var got NodeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			m.t.Fatalf("step %d: decode create response: %v", step, err)
		}
		m.live[got.ID] = modelNode{label: label, value: value}
		m.seen = append(m.seen, got.ID)
		return fmt.Sprintf("create id=%d label=%s v=%d", got.ID, label, value)
	}

	target := live[rng.Intn(len(live))]

	if roll < 75 {
		value := rng.Intn(1000)
		rr := m.call(http.MethodPut, fmt.Sprintf("/nodes/%d", target), map[string]any{
			"properties": map[string]any{"v": value},
		})
		if rr.Code != http.StatusOK {
			m.t.Fatalf("step %d: PUT /nodes/%d gave %d: %s", step, target, rr.Code, rr.Body.String())
		}
		entry := m.live[target]
		entry.value = value
		m.live[target] = entry
		return fmt.Sprintf("update id=%d v=%d", target, value)
	}

	rr := m.call(http.MethodDelete, fmt.Sprintf("/nodes/%d", target), nil)
	if rr.Code != http.StatusOK {
		m.t.Fatalf("step %d: DELETE /nodes/%d gave %d: %s", step, target, rr.Code, rr.Body.String())
	}
	delete(m.live, target)
	return fmt.Sprintf("delete id=%d", target)
}

// checkEveryNode asks about every id ever created, live or not.
func (m *restModel) checkEveryNode(step int, op string) {
	m.t.Helper()

	for _, id := range m.seen {
		rr := m.call(http.MethodGet, fmt.Sprintf("/nodes/%d", id), nil)
		want, alive := m.live[id]

		if !alive {
			if rr.Code != http.StatusNotFound {
				m.t.Fatalf("step %d (%s): GET /nodes/%d gave %d for a deleted node, want 404\nbody: %s",
					step, op, id, rr.Code, rr.Body.String())
			}
			continue
		}

		if rr.Code != http.StatusOK {
			m.t.Fatalf("step %d (%s): GET /nodes/%d gave %d, want 200\nbody: %s",
				step, op, id, rr.Code, rr.Body.String())
		}
		var got NodeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			m.t.Fatalf("step %d (%s): decode node %d: %v", step, op, id, err)
		}
		if len(got.Labels) != 1 || got.Labels[0] != want.label {
			m.t.Fatalf("step %d (%s): node %d has labels %v, the model says [%s]",
				step, op, id, got.Labels, want.label)
		}
		if v, ok := got.Properties["v"]; !ok {
			m.t.Fatalf("step %d (%s): node %d has no v property: %v", step, op, id, got.Properties)
		} else if asInt(v) != want.value {
			m.t.Fatalf("step %d (%s): node %d has v=%v, the model says %d",
				step, op, id, v, want.value)
		}
	}
}

// checkEveryLabel compares each label listing with the model, following the
// cursor to the end. A listing that stops early would otherwise read as a
// smaller set rather than as an incomplete one.
func (m *restModel) checkEveryLabel(step int, op string) {
	m.t.Helper()

	for _, label := range modelLabels {
		want := map[uint64]bool{}
		for id, node := range m.live {
			if node.label == label {
				want[id] = true
			}
		}

		got := map[uint64]bool{}
		path := "/nodes?label=" + label
		for page := 0; ; page++ {
			if page > 100 {
				m.t.Fatalf("step %d (%s): label %s did not stop paginating", step, op, label)
			}
			rr := m.call(http.MethodGet, path, nil)
			if rr.Code != http.StatusOK {
				m.t.Fatalf("step %d (%s): GET %s gave %d: %s", step, op, path, rr.Code, rr.Body.String())
			}
			var nodes []NodeResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &nodes); err != nil {
				m.t.Fatalf("step %d (%s): decode %s: %v", step, op, path, err)
			}
			for _, n := range nodes {
				if got[n.ID] {
					m.t.Fatalf("step %d (%s): %s returned node %d two times", step, op, label, n.ID)
				}
				got[n.ID] = true
			}
			cursor := rr.Header().Get(CursorHeader)
			if cursor == "" {
				break
			}
			path = "/nodes?label=" + label + "&cursor=" + cursor
		}

		if len(got) != len(want) {
			m.t.Fatalf("step %d (%s): label %s listed %v, the model says %v",
				step, op, label, sortedIDs(got), sortedIDs(want))
		}
		for id := range want {
			if !got[id] {
				m.t.Fatalf("step %d (%s): label %s omitted node %d. listed %v, the model says %v",
					step, op, label, id, sortedIDs(got), sortedIDs(want))
			}
		}
	}
}

func (m *restModel) call(method, path string, body any) *httptest.ResponseRecorder {
	m.t.Helper()
	req := reqWithTenant(m.t, method, path, body, "default")
	rr := httptest.NewRecorder()
	if path == "/nodes" || len(path) > 7 && path[:7] == "/nodes?" {
		m.server.handleNodes(rr, req)
	} else {
		m.server.handleNode(rr, req)
	}
	return rr
}

// asInt reads a JSON number back as an int. encoding/json gives float64 for
// every number, so a direct comparison with the model's int never matches.
func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return -1
}

func sortedIDs(set map[uint64]bool) []uint64 {
	out := make([]uint64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
