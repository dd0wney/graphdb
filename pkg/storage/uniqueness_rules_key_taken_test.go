package storage

import (
	"errors"
	"os"
	"testing"
)

func TestRegisterUniquenessRule_RefusesASecondNameOnAHeldKey(t *testing.T) {
	gs := newUpsertStore(t)
	registerRule(t, gs, "widget_sku", "Widget", "sku")

	err := gs.RegisterUniquenessRule(UniquenessRule{Name: "widget_sku_2", Label: "Widget", PropertyKey: "sku"})
	if !errors.Is(err, ErrUniquenessRuleKeyTaken) {
		t.Fatalf("second name on Widget.sku: err = %v, want ErrUniquenessRuleKeyTaken", err)
	}
	if got := len(gs.UniquenessRules()); got != 1 {
		t.Errorf("registry holds %d rules after a refusal, want 1", got)
	}
}

func TestRegisterUniquenessRule_RefusesMovingARuleOntoAHeldKey(t *testing.T) {
	gs := newUpsertStore(t)
	registerRule(t, gs, "widget_sku", "Widget", "sku")
	registerRule(t, gs, "widget_serial", "Widget", "serial")

	err := gs.RegisterUniquenessRule(UniquenessRule{Name: "widget_serial", Label: "Widget", PropertyKey: "sku"})
	if !errors.Is(err, ErrUniquenessRuleKeyTaken) {
		t.Fatalf("moving widget_serial onto Widget.sku: err = %v, want ErrUniquenessRuleKeyTaken", err)
	}
	for _, r := range gs.UniquenessRules() {
		if r.Name == "widget_serial" && r.PropertyKey != "serial" {
			t.Errorf("widget_serial was changed to %+v by a refused register", r)
		}
	}
}

// The guard: registering the same rule again is the documented idempotent
// upsert, and it must not be refused as a clash with itself.
func TestRegisterUniquenessRule_SameRuleAgainIsStillAnUpsert(t *testing.T) {
	gs := newUpsertStore(t)
	registerRule(t, gs, "widget_sku", "Widget", "sku")
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "widget_sku", Label: "Widget", PropertyKey: "sku"}); err != nil {
		t.Fatalf("re-register of the same rule: %v", err)
	}
}

// The guard on the load path: a rules.json written before the refusal may
// hold two names on one key. The store must still open, or the operator is
// locked out of the data by a registry mistake.
func TestUniquenessRules_LoadKeepsAnExistingDuplicateKey(t *testing.T) {
	dir := t.TempDir()
	body := `{"version":1,"rules":[` +
		`{"name":"widget_sku","label":"Widget","propertyKey":"sku"},` +
		`{"name":"widget_sku_2","label":"Widget","propertyKey":"sku"}]}`
	if err := os.WriteFile(uniquenessRulesPath(dir), []byte(body), 0o600); err != nil {
		t.Fatalf("seed rules.json: %v", err)
	}
	cfg := DefaultStorageConfig(dir)
	cfg.UseMmapSnapshot = false
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open with a duplicate key in rules.json: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	if got := len(gs.UniquenessRules()); got != 2 {
		t.Errorf("loaded %d rules, want 2", got)
	}
}
