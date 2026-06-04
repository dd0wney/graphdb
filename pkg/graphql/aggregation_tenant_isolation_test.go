package graphql

import (
	"sort"
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func fieldNameSet(fields graphql.FieldDefinitionMap) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestAggregateSchemaForTenant_DoesNotLeakCrossTenantPropertyKeys pins that the
// per-tenant aggregate schema discovers property-key field names ONLY from the
// requesting tenant's own nodes. buildNodeAggregateTypes samples nodes to turn
// each distinct property key into a GraphQL field on <Label>AggregateMinFields
// etc.; sampling across all tenants would surface another tenant's property-key
// names in this tenant's schema introspection (a cross-tenant metadata leak).
// The query-time resolvers are already tenant-scoped (audit A6c); this closes
// the schema-generation side for the aggregation generator.
func TestAggregateSchemaForTenant_DoesNotLeakCrossTenantPropertyKeys(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	// Tenant A: a Person with a property only tenant A uses.
	if _, err := gs.CreateNodeWithTenant("tenantA", []string{"Person"}, map[string]storage.Value{
		"shared": storage.IntValue(1),
		"a_only": storage.IntValue(2),
	}); err != nil {
		t.Fatalf("create tenantA node: %v", err)
	}
	// Tenant B: a Person with a property only tenant B uses (the secret).
	if _, err := gs.CreateNodeWithTenant("tenantB", []string{"Person"}, map[string]storage.Value{
		"shared":   storage.IntValue(1),
		"b_secret": storage.IntValue(3),
	}); err != nil {
		t.Fatalf("create tenantB node: %v", err)
	}

	schema, err := GenerateSchemaWithAggregationForTenant(gs, "tenantA", nil)
	if err != nil {
		t.Fatalf("GenerateSchemaWithAggregationForTenant: %v", err)
	}

	minType, ok := schema.Type("PersonAggregateMinFields").(*graphql.Object)
	if !ok {
		t.Fatalf("PersonAggregateMinFields type not found in schema")
	}
	fields := minType.Fields()

	if _, leaked := fields["b_secret"]; leaked {
		t.Errorf("cross-tenant leak: tenantA's aggregate schema exposes tenantB-only property key 'b_secret'; fields=%v", fieldNameSet(fields))
	}
	if _, present := fields["a_only"]; !present {
		t.Errorf("tenantA's own property key 'a_only' missing from its aggregate schema; fields=%v", fieldNameSet(fields))
	}
}
