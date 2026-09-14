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

// TestWALWriteFailed_RESTRespondsNotDurable is R1: a single-op write whose
// WAL append fails must answer 202 Accepted with a WriteNotDurableResponse
// body, never a bare 500 that discards the id and invites a client retry.
// Table covers both id shapes the spec calls out: createNode returns the id
// in the response body, deleteNode's id comes from the URL instead.
func TestWALWriteFailed_RESTRespondsNotDurable(t *testing.T) {
	cases := []struct {
		name string
		// do arms the fault, performs the write, and returns the recorded
		// response plus the id the caller already knew ahead of the call
		// (0 when the id is only known from the decoded response, i.e.
		// createNode).
		do func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64)
	}{
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
		},
		{
			name: "deleteNode",
			do: func(t *testing.T, server *Server, faults *vfstest.FaultFS) (*httptest.ResponseRecorder, uint64) {
				// Create the node with the fault disarmed, so only the
				// delete's WAL append fails.
				createBody, err := json.Marshal(NodeRequest{
					Labels:     []string{"Person"},
					Properties: map[string]any{"name": "Bob"},
				})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				createReq := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(createBody))
				createReq.Header.Set("Content-Type", "application/json")
				createRR := httptest.NewRecorder()
				server.handleNodes(createRR, createReq)
				if createRR.Code != http.StatusCreated {
					t.Fatalf("setup create: status = %d, want 201; body=%s", createRR.Code, createRR.Body.String())
				}
				var created NodeResponse
				if err := json.NewDecoder(createRR.Body).Decode(&created); err != nil {
					t.Fatalf("setup create: decode: %v", err)
				}

				faults.FailWrite(vfstest.Once, 0)

				delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/nodes/%d", created.ID), nil)
				delRR := httptest.NewRecorder()
				server.handleNode(delRR, delReq)
				return delRR, created.ID
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

			var resp WriteNotDurableResponse
			if err := json.Unmarshal([]byte(rawBody), &resp); err != nil {
				t.Fatalf("decode response: %v; body=%s", err, rawBody)
			}
			if !resp.Applied {
				t.Error("applied = false, want true: the change WAS applied in memory")
			}
			if resp.Durable {
				t.Error("durable = true, want false: the WAL append failed")
			}
			if resp.Retry {
				t.Error("retry = true, want false: a retry would apply the change a second time")
			}
			if resp.ID == 0 {
				t.Error("id = 0, want a non-zero entity id")
			}
			if wantID != 0 && resp.ID != wantID {
				t.Errorf("id = %d, want %d", resp.ID, wantID)
			}

			if tc.name == "createNode" {
				// The node IS in memory: GET must see it. Without this, "applied"
				// in the body would be an unverified claim.
				getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d", resp.ID), nil)
				getRR := httptest.NewRecorder()
				server.handleNode(getRR, getReq)
				if getRR.Code != http.StatusOK {
					t.Fatalf("GET /nodes/%d after WAL failure: status = %d, want 200; body=%s",
						resp.ID, getRR.Code, getRR.Body.String())
				}
			}
		})
	}
}
