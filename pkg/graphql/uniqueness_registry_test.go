package graphql

import (
	"context"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// TestCreateNodeMutation_GenericLabelUniquenessEnforced is the GraphQL half
// of the ADR 0001 parity test (see the REST twin,
// TestCreateNode_GenericLabelUniquenessEnforced in pkg/api): the resolver
// must enforce whatever rule a deployment REGISTERS, not only the
// coord-domain Claim/for_task pair it used to hardcode.
//
// Before stage 2 wired CreateNodeWithUniquenessRulesForTenant into this
// resolver, a generic label like "Widget" had no uniqueness enforcement on
// this surface at all, no matter what rule a deployment registered — the
// only label the resolver ever checked was the literal string "Claim".
func TestCreateNodeMutation_GenericLabelUniquenessEnforced(t *testing.T) {
	gs, schema := setupClaimSchema(t)
	if err := gs.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "widget_sku", Label: "Widget", PropertyKey: "sku",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}

	ctx := tenant.WithTenant(context.Background(), "default")
	create := func(sku string) *graphql.Result {
		return graphql.Do(graphql.Params{
			Schema:        schema,
			RequestString: `mutation { createNode(labels: ["Widget"], properties: "{\"sku\":\"` + sku + `\"}") { id } }`,
			Context:       ctx,
		})
	}

	if res := create("sku-1"); res.HasErrors() {
		t.Fatalf("first Widget create reported errors: %v", res.Errors)
	}

	second := create("sku-1")
	if !second.HasErrors() {
		t.Fatal("duplicate Widget sku was accepted; the registered rule was not enforced")
	}
	if !strings.Contains(second.Errors[0].Message, "unique constraint violation") {
		t.Errorf("error %q should mention unique constraint violation", second.Errors[0].Message)
	}

	if res := create("sku-2"); res.HasErrors() {
		t.Errorf("distinct sku should succeed: %v", res.Errors)
	}

	widgets, err := gs.GetNodesByLabelForTenant("default", "Widget")
	if err != nil {
		t.Fatalf("enumerate widgets: %v", err)
	}
	if len(widgets) != 2 {
		t.Errorf("storage holds %d Widget nodes, want 2", len(widgets))
	}
}

// TestCreateNodeMutation_RequiredRuleMissingIsFriendlyError pins R4 on the
// GraphQL surface: a required pair covering the write's label, with no
// matching rule registered, refuses with the fixed wording naming only the
// caller's own label — never the rule name, never err.Error() from storage.
func TestCreateNodeMutation_RequiredRuleMissingIsFriendlyError(t *testing.T) {
	dir := t.TempDir()
	cfg := storage.DefaultStorageConfig(dir)
	cfg.RequiredUniquenessRules = []storage.RequiredUniquenessRule{
		{Name: "claim_for_task", Label: "Claim"},
	}
	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges: %v", err)
	}

	res := runClaimMutation(t, schema, "default", "graphdb:missing-rule")
	if !res.HasErrors() {
		t.Fatal("create covered by a missing required rule should error")
	}
	msg := res.Errors[0].Message
	want := `a required uniqueness rule for label "Claim" is not registered; contact the administrator`
	if msg != want {
		t.Errorf("error = %q, want %q", msg, want)
	}
	if strings.Contains(msg, "claim_for_task") {
		t.Errorf("error %q leaks the rule name, which the caller never supplied", msg)
	}

	// Registering the rule turns the same mutation into a normal create.
	if err := gs.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
	ok := runClaimMutation(t, schema, "default", "graphdb:missing-rule")
	if ok.HasErrors() {
		t.Fatalf("create should succeed once the rule is registered: %v", ok.Errors)
	}
}

// TestCreateNodeMutation_MultipleRulesRefusedWithoutNamingThem pins the
// fix-round-1 finding (Critical 1): before this fix, a write covered by
// two registered rules reached the caller as "failed to create node:
// multiple uniqueness rules match this write's labels: \"rule_a\" and
// \"rule_b\"" — both rule names, straight from storage.Error(). The
// resolver must map storage.ErrMultipleUniquenessRules to fixed wording
// that names neither rule, the same way the missing-required-rule case
// already does.
func TestCreateNodeMutation_MultipleRulesRefusedWithoutNamingThem(t *testing.T) {
	gs, schema := setupClaimSchema(t)
	if err := gs.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "rule_a", Label: "A", PropertyKey: "x",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule rule_a: %v", err)
	}
	if err := gs.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "rule_b", Label: "B", PropertyKey: "y",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule rule_b: %v", err)
	}

	ctx := tenant.WithTenant(context.Background(), "default")
	res := graphql.Do(graphql.Params{
		Schema: schema,
		RequestString: `mutation { createNode(labels: ["A", "B"], ` +
			`properties: "{\"x\":\"1\",\"y\":\"2\"}") { id } }`,
		Context: ctx,
	})

	if !res.HasErrors() {
		t.Fatal("a node covered by two registered rules should be refused")
	}
	msg := res.Errors[0].Message
	want := "more than one uniqueness rule covers the labels in this request; contact the administrator"
	if msg != want {
		t.Errorf("error = %q, want %q", msg, want)
	}
	for _, leak := range []string{"rule_a", "rule_b"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error %q leaks rule name %q", msg, leak)
		}
	}
}
