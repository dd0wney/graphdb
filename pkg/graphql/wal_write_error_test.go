package graphql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// assertWALWriteFailedExtensions checks the five extension fields
// walWriteFailedError.Extensions() sets (wal_errors.go) on the first
// GraphQL error in rawBody. Shared by every R2 case so the assertion
// itself cannot drift between the test-schema and production-schema
// variants.
func assertWALWriteFailedExtensions(t *testing.T, rawBody string) {
	t.Helper()

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
		t.Error("extensions.id is empty, want the affected entity's id as a non-empty string")
	}
}

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

	assertWALWriteFailedExtensions(t, rr.Body.String())
}

// TestCreateEdgeMutation_ProductionSchema_WALWriteFailed_ExtensionsCarryNoRetry
// is the second R2 case: it builds the schema via
// GenerateSchemaWithLimitsForTenant (pkg/graphql/limits.go), the function
// cmd/server actually calls per tenant, instead of the simpler
// GenerateSchemaWithMutations the first R2 case uses — so the edge
// resolvers are exercised on the exact schema-construction path the
// running server uses, not just a test-only stand-in. Runs createEdge
// specifically, since createEdgeMutationResolver is untested by the first
// case (which only exercises createNode).
func TestCreateEdgeMutation_ProductionSchema_WALWriteFailed_ExtensionsCarryNoRetry(t *testing.T) {
	dir := t.TempDir()
	faults := vfstest.NewFaults(vfs.OS(), t.Name())
	cfg := storage.DefaultStorageConfig(dir)
	cfg.FS = faults

	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Two nodes to connect, and a "Person" label for the tenant's schema
	// to register. Created with the fault disarmed.
	from, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{"name": storage.StringValue("From")})
	if err != nil {
		t.Fatalf("setup create from node: %v", err)
	}
	to, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{"name": storage.StringValue("To")})
	if err != nil {
		t.Fatalf("setup create to node: %v", err)
	}

	limits := &LimitConfig{DefaultLimit: 100, MaxLimit: 1000}
	schema, err := GenerateSchemaWithLimitsForTenant(gs, limits, tenant.DefaultTenantID, nil)
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimitsForTenant: %v", err)
	}
	handler := NewGraphQLHandler(schema)

	faults.FailWrite(vfstest.Once, 0)

	mutation := fmt.Sprintf(`
		mutation {
			createEdge(fromNodeId: "%d", toNodeId: "%d", type: "KNOWS") {
				id
			}
		}
	`, from.ID, to.ID)
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

	assertWALWriteFailedExtensions(t, rr.Body.String())
}
