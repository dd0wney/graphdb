package graphql

import (
	"context"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// claimMutationFor builds the createNode mutation string used by the
// :Claim B-lite tests. Kept here (not as a helper in production code)
// because the resolver currently takes pre-serialized JSON for
// `properties`; in tests we want a small fixture, not a generalised
// builder.
func claimMutationFor(forTask string) string {
	return `mutation { createNode(labels: ["Claim"], properties: "{\"for_task\":\"` + forTask + `\"}") { id labels properties } }`
}

// runClaimMutation executes the :Claim createNode mutation under a
// tenant-scoped context — the resolver pulls tenant from the context
// via tenant.MustFromContext.
func runClaimMutation(t *testing.T, schema graphql.Schema, tenantID, forTask string) *graphql.Result {
	t.Helper()
	ctx := tenant.WithTenant(context.Background(), tenantID)
	return graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: claimMutationFor(forTask),
		Context:       ctx,
	})
}

// TestCreateClaimMutation_FirstSucceeds is the happy path: a new
// :Claim node with for_task="X" should be created when no other Claim
// holds the same for_task.
func TestCreateClaimMutation_FirstSucceeds(t *testing.T) {
	gs, schema := setupClaimSchema(t)

	res := runClaimMutation(t, schema, "default", "graphdb:H4-PR1-blite")
	if res.HasErrors() {
		t.Fatalf("first :Claim create reported errors: %v", res.Errors)
	}

	// Confirm the node landed in storage with the right shape.
	claims := gs.GetNodesByLabelForTenant("default", "Claim")
	if len(claims) != 1 {
		t.Fatalf("storage holds %d :Claim nodes after first create, want 1", len(claims))
	}
}

// TestCreateClaimMutation_DuplicateRejected verifies the B-lite
// rule: a second :Claim for the same for_task in the same tenant
// must be rejected with an error mentioning the conflict.
func TestCreateClaimMutation_DuplicateRejected(t *testing.T) {
	gs, schema := setupClaimSchema(t)

	first := runClaimMutation(t, schema, "default", "graphdb:H4-PR1-blite")
	if first.HasErrors() {
		t.Fatalf("first :Claim create errored: %v", first.Errors)
	}

	second := runClaimMutation(t, schema, "default", "graphdb:H4-PR1-blite")
	if !second.HasErrors() {
		t.Fatalf("second :Claim create should have errored, got data=%v", second.Data)
	}
	msg := second.Errors[0].Message
	if !strings.Contains(msg, "unique constraint violation") {
		t.Errorf("error message %q should mention unique constraint violation", msg)
	}

	// Storage should still hold exactly one Claim.
	claims := gs.GetNodesByLabelForTenant("default", "Claim")
	if len(claims) != 1 {
		t.Errorf("storage holds %d :Claim nodes after rejected second create, want 1", len(claims))
	}
}

// TestCreateClaimMutation_DifferentTaskSucceeds confirms uniqueness
// is keyed on the for_task value, not on label-presence — different
// tasks may each be claimed concurrently.
func TestCreateClaimMutation_DifferentTaskSucceeds(t *testing.T) {
	gs, schema := setupClaimSchema(t)

	if res := runClaimMutation(t, schema, "default", "graphdb:H4-PR1-blite"); res.HasErrors() {
		t.Fatalf("first claim errored: %v", res.Errors)
	}
	if res := runClaimMutation(t, schema, "default", "graphdb:H4-PR2-skill"); res.HasErrors() {
		t.Fatalf("second claim (different for_task) errored: %v", res.Errors)
	}

	claims := gs.GetNodesByLabelForTenant("default", "Claim")
	if len(claims) != 2 {
		t.Errorf("expected 2 :Claim nodes for distinct for_task values, got %d", len(claims))
	}
}

// TestCreateClaimMutation_RequiresForTask rejects a Claim that omits
// the for_task property — the spike requires for_task as the
// uniqueness key, so absence is a malformed call.
func TestCreateClaimMutation_RequiresForTask(t *testing.T) {
	_, schema := setupClaimSchema(t)

	ctx := tenant.WithTenant(context.Background(), "default")
	mutation := `mutation { createNode(labels: ["Claim"], properties: "{\"id\":\"claim-1\"}") { id } }`
	res := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: mutation,
		Context:       ctx,
	})

	if !res.HasErrors() {
		t.Fatalf(":Claim without for_task should error, got data=%v", res.Data)
	}
	if !strings.Contains(res.Errors[0].Message, `"for_task"`) {
		t.Errorf("error %q should mention the missing for_task property", res.Errors[0].Message)
	}
}

// TestCreateClaimMutation_NonClaimLabelUnaffected confirms the
// special-case is narrow: a Person (or any non-Claim label) is
// created with no uniqueness check, even when properties happen to
// share a for_task value.
func TestCreateClaimMutation_NonClaimLabelUnaffected(t *testing.T) {
	gs, schema := setupClaimSchema(t)

	ctx := tenant.WithTenant(context.Background(), "default")
	personMutation := func() *graphql.Result {
		return graphql.Do(graphql.Params{
			Schema:        schema,
			RequestString: `mutation { createNode(labels: ["Person"], properties: "{\"for_task\":\"x\"}") { id } }`,
			Context:       ctx,
		})
	}

	if res := personMutation(); res.HasErrors() {
		t.Fatalf("first Person create errored: %v", res.Errors)
	}
	if res := personMutation(); res.HasErrors() {
		t.Errorf("second Person create with same for_task should NOT be rejected: %v", res.Errors)
	}

	if persons := gs.GetNodesByLabelForTenant("default", "Person"); len(persons) != 2 {
		t.Errorf("expected 2 Person nodes, got %d", len(persons))
	}
}

// setupClaimSchema returns a fresh GraphStorage + schema wired with
// the createNode resolver. The storage is closed via t.Cleanup.
func setupClaimSchema(t *testing.T) (*storage.GraphStorage, graphql.Schema) {
	t.Helper()
	dir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{
		DataDir:        dir,
		BulkImportMode: true,
	})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges: %v", err)
	}
	return gs, schema
}
