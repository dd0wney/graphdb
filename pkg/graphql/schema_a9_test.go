package graphql

import (
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Audit A9 #2 (2026-05-08): pin the per-tenant schema-builder
// variants. Each *ForTenant function produces a schema whose type
// registry only contains node types for labels that exist in the
// caller's tenant. Foreign-tenant labels (the metadata leak A9
// closes) must not appear.
//
// The HTTP-level introspection test lands in A9 #4. This file pins
// the pkg-level contract.

func setupA9Fixture(t *testing.T) *storage.GraphStorage {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	// tenant-A has labels {Person, Doc}.
	if _, err := gs.CreateNodeWithTenant("tenant-A", []string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("alice"),
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := gs.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil); err != nil {
		t.Fatalf("seed A doc: %v", err)
	}

	// tenant-B has a label tenant-A must NEVER see in introspection.
	if _, err := gs.CreateNodeWithTenant("tenant-B", []string{"InternalSecretThing"}, nil); err != nil {
		t.Fatalf("seed B: %v", err)
	}
	return gs
}

// schemaHasType walks the graphql.Schema's type map looking for a
// type with the given name. Mirrors how introspection (`__schema`)
// would surface labels.
func schemaHasType(schema graphql.Schema, name string) bool {
	for typeName := range schema.TypeMap() {
		if typeName == name {
			return true
		}
	}
	return false
}

// TestGenerateSchemaForTenant_ExcludesForeignLabels is the cardinal
// A9 contract: the per-tenant schema's type registry contains the
// caller's labels and excludes foreign-tenant labels.
func TestGenerateSchemaForTenant_ExcludesForeignLabels(t *testing.T) {
	gs := setupA9Fixture(t)

	t.Run("tenant-A schema includes A's labels", func(t *testing.T) {
		schema, err := GenerateSchemaForTenant(gs, "tenant-A")
		if err != nil {
			t.Fatalf("schema: %v", err)
		}
		for _, want := range []string{"Person", "Doc"} {
			if !schemaHasType(schema, want) {
				t.Errorf("tenant-A schema missing own type %q", want)
			}
		}
	})

	t.Run("tenant-A schema excludes B's labels", func(t *testing.T) {
		schema, err := GenerateSchemaForTenant(gs, "tenant-A")
		if err != nil {
			t.Fatalf("schema: %v", err)
		}
		if schemaHasType(schema, "InternalSecretThing") {
			t.Error("tenant-A schema leaked tenant-B label 'InternalSecretThing' (A9 metadata leak)")
		}
	})

	t.Run("tenant-blind GenerateSchema sees both", func(t *testing.T) {
		// Sanity: the legacy tenant-blind path still surfaces every
		// label. Used by CLI / single-tenant — must keep working.
		schema, err := GenerateSchema(gs)
		if err != nil {
			t.Fatalf("schema: %v", err)
		}
		for _, want := range []string{"Person", "Doc", "InternalSecretThing"} {
			if !schemaHasType(schema, want) {
				t.Errorf("tenant-blind schema missing %q", want)
			}
		}
	})
}

// TestGenerateSchemaWithLimitsForTenant_ExcludesForeignLabels mirrors
// the cardinal test for the limits-flavored builder — the one the
// server actually uses (server_init.go uses GenerateSchemaWithLimits).
func TestGenerateSchemaWithLimitsForTenant_ExcludesForeignLabels(t *testing.T) {
	gs := setupA9Fixture(t)
	cfg := &LimitConfig{DefaultLimit: 100, MaxLimit: 1000}

	schema, err := GenerateSchemaWithLimitsForTenant(gs, cfg, "tenant-A")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	if !schemaHasType(schema, "Person") {
		t.Error("tenant-A schema (limits) missing own type 'Person'")
	}
	if schemaHasType(schema, "InternalSecretThing") {
		t.Error("tenant-A schema (limits) leaked tenant-B label 'InternalSecretThing'")
	}
}

// TestAllSchemaForTenantBuilders_ExcludeForeignLabels is the
// safety-net loop: every *ForTenant variant must satisfy the same
// contract. Catches regressions if a future variant is added without
// extending isolation.
func TestAllSchemaForTenantBuilders_ExcludeForeignLabels(t *testing.T) {
	gs := setupA9Fixture(t)
	cfg := &LimitConfig{DefaultLimit: 100, MaxLimit: 1000}

	builders := []struct {
		name  string
		build func() (graphql.Schema, error)
	}{
		{"GenerateSchemaForTenant", func() (graphql.Schema, error) {
			return GenerateSchemaForTenant(gs, "tenant-A")
		}},
		{"GenerateSchemaWithEdgesForTenant", func() (graphql.Schema, error) {
			return GenerateSchemaWithEdgesForTenant(gs, "tenant-A")
		}},
		{"GenerateSchemaWithFilteringForTenant", func() (graphql.Schema, error) {
			return GenerateSchemaWithFilteringForTenant(gs, "tenant-A")
		}},
		{"GenerateSchemaWithLimitsForTenant", func() (graphql.Schema, error) {
			return GenerateSchemaWithLimitsForTenant(gs, cfg, "tenant-A")
		}},
		{"GenerateSchemaWithAggregationForTenant", func() (graphql.Schema, error) {
			return GenerateSchemaWithAggregationForTenant(gs, "tenant-A")
		}},
		{"GenerateSchemaWithMutationsForTenant", func() (graphql.Schema, error) {
			return GenerateSchemaWithMutationsForTenant(gs, "tenant-A")
		}},
	}

	// Each variant exposes node types under different names
	// (aggregation flavors use "PersonAggregate", base flavors use
	// "Person"). The contract-level assertion here is exclusion of
	// foreign-tenant labels in any form. The "includes own" sanity
	// check is in TestGenerateSchemaForTenant_ExcludesForeignLabels
	// using the canonical builder.
	for _, b := range builders {
		t.Run(b.name, func(t *testing.T) {
			schema, err := b.build()
			if err != nil {
				t.Fatalf("%s: %v", b.name, err)
			}
			// Must not contain tenant-B's label or any prefix/suffix
			// derivative (e.g. "InternalSecretThingAggregate").
			for typeName := range schema.TypeMap() {
				if typeName == "InternalSecretThing" || typeName == "InternalSecretThingAggregate" {
					t.Errorf("%s leaked tenant-B-derived type %q", b.name, typeName)
				}
			}
		})
	}
}
