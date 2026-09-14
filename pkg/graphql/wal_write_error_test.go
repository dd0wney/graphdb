package graphql

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// TestCreateNodeMutation_WALWriteFailed_ExtensionsCarryNoRetry is R2: a
// createNode mutation whose WAL append fails must carry a WAL_WRITE_FAILED
// error extension (code, applied, retry, id) so a client can tell "applied,
// do not retry" apart from a plain resolver error, instead of the extensions
// field being silently absent (graphdb:v1.4-api-wal-error-no-retry).
func TestCreateNodeMutation_WALWriteFailed_ExtensionsCarryNoRetry(t *testing.T) {
	dir := t.TempDir()
	faults := vfstest.NewFaults(vfs.OS(), t.Name())
	cfg := storage.DefaultStorageConfig(dir)
	cfg.FS = faults

	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()

	schema, err := GenerateSchemaWithMutations(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithMutations: %v", err)
	}
	handler := NewGraphQLHandler(schema)

	faults.FailWrite(vfstest.Once, 0)

	mutation := `
		mutation {
			createNode(labels: ["Person"], properties: "{\"name\": \"Alice\"}") {
				id
			}
		}
	`
	body, err := json.Marshal(GraphQLRequest{Query: mutation})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Positive control: a fault that never fired proves nothing about the
	// WAL failure path (verify-the-instrument).
	if !faults.Fired() {
		t.Fatal("the write fault never fired, so this test proves nothing about the WAL failure path")
	}

	// GraphQL reports the failure inside the errors array, not the HTTP
	// status — the transport-level response is always 200.
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	rawBody := rr.Body.String()
	var resp GraphQLResponse
	if err := json.Unmarshal([]byte(rawBody), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rawBody)
	}
	if len(resp.Errors) == 0 {
		t.Fatalf("expected an error, got none; body=%s", rawBody)
	}

	ext := resp.Errors[0].Extensions
	if ext == nil {
		t.Fatalf("errors[0].extensions is absent; body=%s", rawBody)
	}
	if code, _ := ext["code"].(string); code != "WAL_WRITE_FAILED" {
		t.Errorf("extensions.code = %v, want WAL_WRITE_FAILED", ext["code"])
	}
	if applied, _ := ext["applied"].(bool); !applied {
		t.Errorf("extensions.applied = %v, want true", ext["applied"])
	}
	if durable, _ := ext["durable"].(bool); durable {
		t.Errorf("extensions.durable = %v, want false", ext["durable"])
	}
	if retry, _ := ext["retry"].(bool); retry {
		t.Errorf("extensions.retry = %v, want false", ext["retry"])
	}
	id, _ := ext["id"].(string)
	if id == "" {
		t.Error("extensions.id is empty, want the created node's id as a non-empty string")
	}
}
