package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// setupTestServerWithFaultFS builds a test server whose storage config's
// filesystem is a vfstest.FaultFS, so a test can arm a write fault and
// observe how a REST handler responds to a WAL append failure (R1,
// graphdb:v1.4-api-wal-error-no-retry).
func setupTestServerWithFaultFS(t *testing.T) (*Server, *vfstest.FaultFS, func()) {
	t.Helper()

	dir := t.TempDir()
	faults := vfstest.NewFaults(vfs.OS(), t.Name())
	cfg := storage.DefaultStorageConfig(dir)
	cfg.FS = faults

	server, cleanup := setupTestServerWithConfig(t, cfg)
	return server, faults, cleanup
}

// mustCreateNodeForWALTest creates a node with the fault filesystem
// disarmed and returns its id. Used by table rows that need an existing
// node before they arm the fault on their own operation (update, delete,
// createEdge, and so on).
func mustCreateNodeForWALTest(t *testing.T, server *Server, name string) uint64 {
	t.Helper()

	body, err := json.Marshal(NodeRequest{
		Labels:     []string{"Person"},
		Properties: map[string]any{"name": name},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleNodes(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup create node: status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var created NodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("setup create node: decode: %v", err)
	}
	return created.ID
}

// mustCreateEdgeForWALTest creates an edge between fromID and toID with the
// fault filesystem disarmed and returns its id.
func mustCreateEdgeForWALTest(t *testing.T, server *Server, fromID, toID uint64) uint64 {
	t.Helper()

	body, err := json.Marshal(EdgeRequest{
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       "KNOWS",
		Weight:     1,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/edges", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleEdges(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup create edge: status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var created EdgeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("setup create edge: decode: %v", err)
	}
	return created.ID
}

// walWriteFailedRESTCase is one REST write path under test for R1.
type walWriteFailedRESTCase struct {
	name string
	// do arms the fault, performs the write, and returns the recorded
	// response plus the id the test already knew ahead of the call (from
	// the URL or a setup step). 0 means the id is only knowable from the
	// decoded response — true for the two creates, and also true (but not
	// checked at all — see idOptional) for createVectorIndex and
	// deleteAllNodes, which have no single per-write entity id.
	do func(t *testing.T, server *Server, faults *vfstest.FaultFS) (rr *httptest.ResponseRecorder, wantID uint64)
	// idOptional is true only for createVectorIndex and deleteAllNodes:
	// their body has no per-write entity id at all
	// (WriteNotDurableResponse.ID omits a zero value), so "id absent" is
	// the CORRECT outcome there, not a failure.
	idOptional bool
	// afterCheck runs a case-specific extra assertion. body is the
	// decoded response as a map[string]any rather than a concrete struct
	// type, so one table and one assertion loop covers
	// NodeNotDurableResponse, EdgeNotDurableResponse and
	// WriteNotDurableResponse alike — each is a JSON object carrying the
	// same five applied/durable/retry/error/message keys, plus whatever
	// entity fields (if any) that case's response also carries.
	afterCheck func(t *testing.T, server *Server, body map[string]any)
}

// TestWALWriteFailed_RESTRespondsNotDurable is R1: a single-op write whose
// WAL append fails must answer 202 Accepted with a not-durable body, never
// a bare 500 that discards the id and invites a client retry. The table
// covers every REST write path whose storage call can return
// storage.ErrWALWriteFailed: the two creates (whose body is a superset of
// the normal 201 body — see NodeNotDurableResponse/EdgeNotDurableResponse
// in types.go), update and delete for both nodes and edges, the
// vector-index create, and the bulk deleteAllNodes.
func TestWALWriteFailed_RESTRespondsNotDurable(t *testing.T) {
	cases := []walWriteFailedRESTCase{
		{
			name: "createNode",
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				faults.FailWrite(vfstest.Once, 0)

				body, err := json.Marshal(NodeRequest{
					Labels:     []string{"Person"},
					Properties: map[string]any{"name": "Alice"},
				})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				server.handleNodes(rr, req)
				return rr, 0
			},
			afterCheck: func(t *testing.T, server *Server, body map[string]any) {
				// The 202 body must be a superset of the 201 NodeResponse
				// body: a client that treats any 2xx as success and reads
				// only the node fields must still build a correct node.
				labels, _ := body["labels"].([]any)
				if len(labels) != 1 || labels[0] != "Person" {
					t.Errorf("labels = %v, want [Person] (the 202 body must be a superset of the 201 body)", body["labels"])
				}

				// The node IS in memory: GET must see it. Without this,
				// "applied" in the body would be an unverified claim.
				idF, _ := body["id"].(float64)
				getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d", uint64(idF)), nil)
				getRR := httptest.NewRecorder()
				server.handleNode(getRR, getReq)
				if getRR.Code != http.StatusOK {
					t.Fatalf("GET /nodes/%d after WAL failure: status = %d, want 200; body=%s",
						uint64(idF), getRR.Code, getRR.Body.String())
				}
			},
		},
		{
			name: "updateNode",
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				id := mustCreateNodeForWALTest(t, server, "Carol")

				faults.FailWrite(vfstest.Once, 0)

				body, err := json.Marshal(NodeRequest{Properties: map[string]any{"name": "Carol2"}})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/nodes/%d", id), bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				server.handleNode(rr, req)
				return rr, id
			},
		},
		{
			name: "deleteNode",
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				id := mustCreateNodeForWALTest(t, server, "Bob")

				faults.FailWrite(vfstest.Once, 0)

				req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/nodes/%d", id), nil)
				rr := httptest.NewRecorder()
				server.handleNode(rr, req)
				return rr, id
			},
			afterCheck: func(t *testing.T, server *Server, body map[string]any) {
				// The node IS gone from memory: GET must 404. Without
				// this, "applied" in the body would be an unverified
				// claim.
				idF, _ := body["id"].(float64)
				getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d", uint64(idF)), nil)
				getRR := httptest.NewRecorder()
				server.handleNode(getRR, getReq)
				if getRR.Code != http.StatusNotFound {
					t.Fatalf("GET /nodes/%d after a not-durable delete: status = %d, want 404",
						uint64(idF), getRR.Code)
				}
			},
		},
		{
			name: "createEdge",
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				fromID := mustCreateNodeForWALTest(t, server, "From")
				toID := mustCreateNodeForWALTest(t, server, "To")

				faults.FailWrite(vfstest.Once, 0)

				body, err := json.Marshal(EdgeRequest{FromNodeID: fromID, ToNodeID: toID, Type: "KNOWS", Weight: 1})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, "/edges", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				server.handleEdges(rr, req)
				return rr, 0
			},
			afterCheck: func(t *testing.T, server *Server, body map[string]any) {
				// The 202 body must be a superset of the 201 EdgeResponse
				// body.
				edgeType, _ := body["type"].(string)
				if edgeType != "KNOWS" {
					t.Errorf("type = %v, want KNOWS (the 202 body must be a superset of the 201 body)", body["type"])
				}
			},
		},
		{
			name: "updateEdge",
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				fromID := mustCreateNodeForWALTest(t, server, "From2")
				toID := mustCreateNodeForWALTest(t, server, "To2")
				edgeID := mustCreateEdgeForWALTest(t, server, fromID, toID)

				faults.FailWrite(vfstest.Once, 0)

				weight := 2.5
				body, err := json.Marshal(EdgeUpdateRequest{Weight: &weight})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/edges/%d", edgeID), bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				server.handleEdge(rr, req)
				return rr, edgeID
			},
		},
		{
			name: "deleteEdge",
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				fromID := mustCreateNodeForWALTest(t, server, "From3")
				toID := mustCreateNodeForWALTest(t, server, "To3")
				edgeID := mustCreateEdgeForWALTest(t, server, fromID, toID)

				faults.FailWrite(vfstest.Once, 0)

				req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/edges/%d", edgeID), nil)
				rr := httptest.NewRecorder()
				server.handleEdge(rr, req)
				return rr, edgeID
			},
			afterCheck: func(t *testing.T, server *Server, body map[string]any) {
				idF, _ := body["id"].(float64)
				getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/edges/%d", uint64(idF)), nil)
				getRR := httptest.NewRecorder()
				server.handleEdge(getRR, getReq)
				if getRR.Code != http.StatusNotFound {
					t.Fatalf("GET /edges/%d after a not-durable delete: status = %d, want 404",
						uint64(idF), getRR.Code)
				}
			},
		},
		{
			name:       "createVectorIndex",
			idOptional: true,
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				faults.FailWrite(vfstest.Once, 0)

				body, err := json.Marshal(VectorIndexRequest{PropertyName: "embedding", Dimensions: 4})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, "/vector-indexes", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				server.handleVectorIndexes(rr, req)
				return rr, 0
			},
		},
		{
			name:       "deleteAllNodes",
			idOptional: true,
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				// At least one node must exist, or the delete loop never
				// performs a write and the fault never fires.
				mustCreateNodeForWALTest(t, server, "Dave")

				faults.FailWrite(vfstest.Once, 0)

				req := httptest.NewRequest(http.MethodDelete, "/nodes", nil)
				rr := httptest.NewRecorder()
				server.handleNodes(rr, req)
				return rr, 0
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, faults, cleanup := setupTestServerWithFaultFS(t)
			defer cleanup()

			rr, wantID := tc.do(t, server, faults)

			// Positive control: a fault that never fired proves nothing
			// about the WAL failure path (verify-the-instrument).
			if !faults.Fired() {
				t.Fatal("the write fault never fired, so this test proves nothing about the WAL failure path")
			}

			rawBody := rr.Body.String()
			if rr.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want %d (202 Accepted); body=%s", rr.Code, http.StatusAccepted, rawBody)
			}

			var body map[string]any
			if err := json.Unmarshal([]byte(rawBody), &body); err != nil {
				t.Fatalf("decode response: %v; body=%s", err, rawBody)
			}

			if applied, _ := body["applied"].(bool); !applied {
				t.Errorf("applied = %v, want true: the change WAS applied in memory", body["applied"])
			}
			if durable, _ := body["durable"].(bool); durable {
				t.Errorf("durable = %v, want false: the WAL append failed", body["durable"])
			}
			if retry, _ := body["retry"].(bool); retry {
				t.Errorf("retry = %v, want false: a retry would apply the change a second time", body["retry"])
			}

			idVal, hasID := body["id"]
			switch {
			case tc.idOptional:
				// No single per-write entity id for this handler —
				// WriteNotDurableResponse.ID omits a zero value, so its
				// absence here is correct, not a failure.
			case !hasID:
				t.Error("id is absent, want a non-zero entity id")
			default:
				idF, ok := idVal.(float64)
				if !ok || idF == 0 {
					t.Errorf("id = %v, want a non-zero entity id", idVal)
				} else if wantID != 0 && uint64(idF) != wantID {
					t.Errorf("id = %v, want %d", idF, wantID)
				}
			}

			if tc.afterCheck != nil {
				tc.afterCheck(t, server, body)
			}
		})
	}
}
