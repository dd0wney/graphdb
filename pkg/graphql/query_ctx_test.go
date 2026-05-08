package graphql

import (
	"context"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
	"github.com/graphql-go/graphql"
)

// TestExecuteQuery_ContextFlowsToResolver is the audit A6c-graphql-ctx
// gate. Pre-fix, ExecuteQuery dropped the request context entirely;
// resolvers ran with context.Background(). This test pins that the
// context now reaches the resolver and that a tenant ID stamped via
// tenant.WithTenant survives the trip.
//
// The next PR (A6c-graphql-resolvers) migrates ~31 resolver call
// sites to actually use this — but without the plumb proven to work
// end-to-end, those migrations would be silent no-ops.
func TestExecuteQuery_ContextFlowsToResolver(t *testing.T) {
	// Build a minimal schema with one resolver that captures the
	// context it was called with. The schema doesn't touch the graph
	// — this is a pure plumbing test.
	var captured context.Context
	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"whoami": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (any, error) {
					captured = p.Context
					return "ok", nil
				},
			},
		},
	})
	schema, err := graphql.NewSchema(graphql.SchemaConfig{Query: queryType})
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	t.Run("plain context propagates", func(t *testing.T) {
		captured = nil
		type sentinelKey struct{}
		ctx := context.WithValue(context.Background(), sentinelKey{}, "value-A")

		result := ExecuteQuery(ctx, "{ whoami }", schema)
		if result.HasErrors() {
			t.Fatalf("query errors: %v", result.Errors)
		}
		if captured == nil {
			t.Fatal("resolver got nil context — plumb is broken")
		}
		if got, _ := captured.Value(sentinelKey{}).(string); got != "value-A" {
			t.Errorf("context value lost: want %q, got %q", "value-A", got)
		}
	})

	t.Run("tenant-tagged context reaches resolver", func(t *testing.T) {
		captured = nil
		ctx := tenant.WithTenant(context.Background(), "tenant-A")

		result := ExecuteQuery(ctx, "{ whoami }", schema)
		if result.HasErrors() {
			t.Fatalf("query errors: %v", result.Errors)
		}
		got, ok := tenant.FromContext(captured)
		if !ok {
			t.Fatal("tenant.FromContext: not found in resolver's p.Context — A6c-graphql-resolvers will be a no-op")
		}
		if got != "tenant-A" {
			t.Errorf("tenant: want %q, got %q", "tenant-A", got)
		}
	})

	t.Run("ExecuteQueryWithVariables also propagates", func(t *testing.T) {
		captured = nil
		ctx := tenant.WithTenant(context.Background(), "tenant-B")

		result := ExecuteQueryWithVariables(ctx, "{ whoami }", schema, map[string]any{})
		if result.HasErrors() {
			t.Fatalf("query errors: %v", result.Errors)
		}
		got, _ := tenant.FromContext(captured)
		if got != "tenant-B" {
			t.Errorf("variables-path: want %q, got %q", "tenant-B", got)
		}
	})
}
