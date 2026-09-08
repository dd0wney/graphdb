package storage

// Tests for the enforcement path (ADR 0001, commit 2):
// CreateNodeWithUniquenessRulesForTenant, the one lookup path both write
// surfaces will call (GraphQL resolver and REST handler, added in stage 2).
//
// Naming note: R8 (ruling, 2026-09-08) resolved the naming question stage 1
// flagged. Rule names match ^[A-Za-z_][A-Za-z0-9_]*$, the same shape as a
// property key, so the coord rule is spelled "claim_for_task", not
// "claim-for-task". This file, stage 2's REST/GraphQL wiring, and the
// GRAPHDB_REQUIRED_UNIQUENESS_RULES env var all use the underscore form.

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// newTestGraphStorageWithConfig builds a GraphStorage over a fresh temp data
// dir, letting the caller adjust StorageConfig (e.g. RequiredUniquenessRules)
// before construction. Mirrors newTestGraphStorage (unique_constraint_test.go)
// but exposes the config for the enforcement-path tests.
func newTestGraphStorageWithConfig(t *testing.T, mutate func(*StorageConfig)) *GraphStorage {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultStorageConfig(dir)
	if mutate != nil {
		mutate(&cfg)
	}
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	return gs
}

func TestCreateNodeWithUniquenessRules_TwoDirection(t *testing.T) {
	gs := newTestGraphStorageWithConfig(t, func(c *StorageConfig) {
		c.RequiredUniquenessRules = []RequiredUniquenessRule{{Name: "claim_for_task", Label: "Claim"}}
	})
	tenantID := "tenant-a"

	before := gs.CountNodesForTenant(tenantID)
	_, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Claim"}, map[string]Value{"for_task": StringValue("t1")})
	if err == nil {
		t.Fatalf("create with the required rule missing should have failed, got nil")
	}
	if !errors.Is(err, ErrRequiredUniquenessRuleMissing) {
		t.Fatalf("expected ErrRequiredUniquenessRuleMissing, got %T: %v", err, err)
	}
	if after := gs.CountNodesForTenant(tenantID); after != before {
		t.Errorf("node count changed from %d to %d; the refused create must touch no node", before, after)
	}

	if _, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Other"}, map[string]Value{}); err != nil {
		t.Errorf("create with an uncovered label should succeed while the required rule is missing: %v", err)
	}

	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}

	first, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Claim"}, map[string]Value{"for_task": StringValue("t1")})
	if err != nil {
		t.Fatalf("create after registering the rule should succeed: %v", err)
	}
	if first == nil {
		t.Fatalf("create returned a nil node with no error")
	}

	_, err = gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Claim"}, map[string]Value{"for_task": StringValue("t1")})
	if err == nil {
		t.Fatalf("second identical create should have been refused, got nil")
	}
	if !errors.Is(err, ErrUniqueConstraintViolation) {
		t.Fatalf("expected ErrUniqueConstraintViolation, got %T: %v", err, err)
	}

	if _, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Other"}, map[string]Value{}); err != nil {
		t.Errorf("create with an uncovered label should still succeed once the rule is registered: %v", err)
	}
}

// TestCreateNodeWithUniquenessRules_RequiredPairLabelMustMatch is ruling R9
// (fix round 1): a required (Name, Label) pair is satisfied only by a
// registered rule carrying BOTH the same name AND the same label. A
// registered rule that reuses the required name under a DIFFERENT label
// must not satisfy the pair.
func TestCreateNodeWithUniquenessRules_RequiredPairLabelMustMatch(t *testing.T) {
	gs := newTestGraphStorageWithConfig(t, func(c *StorageConfig) {
		c.RequiredUniquenessRules = []RequiredUniquenessRule{{Name: "claim_for_task", Label: "Claim"}}
	})

	// Registered under the required NAME, but a different Label. Under a
	// name-only check this would (wrongly) satisfy the required pair.
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "claim_for_task", Label: "Task", PropertyKey: "id"}); err != nil {
		t.Fatalf("RegisterUniquenessRule (wrong label): %v", err)
	}

	_, err := gs.CreateNodeWithUniquenessRulesForTenant("tenant-a", []string{"Claim"}, map[string]Value{"for_task": StringValue("t1")})
	if err == nil {
		t.Fatalf("create should be refused: the registered rule's label does not match the required pair's label")
	}
	if !errors.Is(err, ErrRequiredUniquenessRuleMissing) {
		t.Fatalf("expected ErrRequiredUniquenessRuleMissing, got %T: %v", err, err)
	}

	// Re-registering the SAME name with the label the required pair actually
	// names satisfies it.
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}); err != nil {
		t.Fatalf("RegisterUniquenessRule (correct label): %v", err)
	}

	if _, err := gs.CreateNodeWithUniquenessRulesForTenant("tenant-a", []string{"Claim"}, map[string]Value{"for_task": StringValue("t1")}); err != nil {
		t.Fatalf("create should succeed once the registered rule's label matches the required pair: %v", err)
	}
}

func TestCreateNodeWithUniquenessRules_ContainmentNotExactMatch(t *testing.T) {
	gs := newTestGraphStorageWithConfig(t, nil)
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}

	tenantID := "tenant-a"
	if _, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Claim", "Urgent"}, map[string]Value{"for_task": StringValue("t1")}); err != nil {
		t.Fatalf("first create with an extra label should succeed: %v", err)
	}
	_, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Claim", "Urgent"}, map[string]Value{"for_task": StringValue("t1")})
	if err == nil {
		t.Fatalf("second create carrying both labels should be refused (#470 containment semantics)")
	}
	if !errors.Is(err, ErrUniqueConstraintViolation) {
		t.Fatalf("expected ErrUniqueConstraintViolation, got %T: %v", err, err)
	}
}

func TestRegisterUniquenessRule_SucceedsWhileRequiredMissing(t *testing.T) {
	gs := newTestGraphStorageWithConfig(t, func(c *StorageConfig) {
		c.RequiredUniquenessRules = []RequiredUniquenessRule{{Name: "claim_for_task", Label: "Claim"}}
	})

	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}); err != nil {
		t.Fatalf("RegisterUniquenessRule must succeed while its own required rule is unmet (ADR 0001 exemption): %v", err)
	}
}

func TestRemoveUniquenessRule_ReenablesRefusal(t *testing.T) {
	gs := newTestGraphStorageWithConfig(t, func(c *StorageConfig) {
		c.RequiredUniquenessRules = []RequiredUniquenessRule{{Name: "claim_for_task", Label: "Claim"}}
	})
	tenantID := "tenant-a"

	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
	if _, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Claim"}, map[string]Value{"for_task": StringValue("t1")}); err != nil {
		t.Fatalf("create while the rule is registered should succeed: %v", err)
	}

	if err := gs.RemoveUniquenessRule("claim_for_task"); err != nil {
		t.Fatalf("RemoveUniquenessRule: %v", err)
	}

	_, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, []string{"Claim"}, map[string]Value{"for_task": StringValue("t2")})
	if err == nil {
		t.Fatalf("create after removal should be refused without a reopen, got nil")
	}
	if !errors.Is(err, ErrRequiredUniquenessRuleMissing) {
		t.Fatalf("expected ErrRequiredUniquenessRuleMissing, got %T: %v", err, err)
	}
}

func TestCreateNodeWithUniquenessRules_MultipleRulesRefused(t *testing.T) {
	gs := newTestGraphStorageWithConfig(t, nil)
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "rule_a", Label: "Alpha", PropertyKey: "key_a"}); err != nil {
		t.Fatalf("RegisterUniquenessRule rule_a: %v", err)
	}
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "rule_b", Label: "Beta", PropertyKey: "key_b"}); err != nil {
		t.Fatalf("RegisterUniquenessRule rule_b: %v", err)
	}

	_, err := gs.CreateNodeWithUniquenessRulesForTenant("tenant-a", []string{"Alpha", "Beta"},
		map[string]Value{"key_a": StringValue("a"), "key_b": StringValue("b")})
	if err == nil {
		t.Fatalf("a node matching two registered rules should be refused, got nil")
	}
	if !errors.Is(err, ErrMultipleUniquenessRules) {
		t.Fatalf("expected ErrMultipleUniquenessRules, got %T: %v", err, err)
	}
}

func TestCreateNodeWithUniquenessRules_MissingPropertyRefused(t *testing.T) {
	gs := newTestGraphStorageWithConfig(t, nil)
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}

	_, err := gs.CreateNodeWithUniquenessRulesForTenant("tenant-a", []string{"Claim"}, map[string]Value{})
	if err == nil {
		t.Fatalf("create missing the rule's property should be refused, got nil")
	}
	if errors.Is(err, ErrRequiredUniquenessRuleMissing) || errors.Is(err, ErrUniqueConstraintViolation) {
		t.Errorf("wrong error class for a missing property: %T: %v", err, err)
	}
	// Stage 2's REST handler maps this class to HTTP 400 (a client error:
	// the caller's own request omitted a required field), distinct from the
	// 500 bucket every other CreateNodeWithUniquenessRulesForTenant failure
	// falls into. That mapping needs a sentinel to check, not a substring
	// match on the message.
	if !errors.Is(err, ErrUniquenessRulePropertyMissing) {
		t.Errorf("error %v does not wrap ErrUniquenessRulePropertyMissing", err)
	}
	if !strings.Contains(err.Error(), "Claim") {
		t.Errorf("error %q does not name the label", err.Error())
	}
	if !strings.Contains(err.Error(), "for_task") {
		t.Errorf("error %q does not name the required property", err.Error())
	}
}

func TestCreateNodeWithUniquenessRules_ConcurrentCreatesOneWins(t *testing.T) {
	gs := newTestGraphStorageWithConfig(t, nil)
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}

	const attempts = 8
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := gs.CreateNodeWithUniquenessRulesForTenant("tenant-a", []string{"Claim"}, map[string]Value{"for_task": StringValue("same-task")})
			if err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("concurrent creates for the same (label, for_task) succeeded %d times, want exactly 1", got)
	}
}

// TestUniquenessRuleRequiredRuleMissingError_ErrorMessage pins the exact wording stage 2
// uses verbatim in its HTTP/GraphQL error mapping (R4, errors.go's doc
// comment on RequiredRuleMissingError.Error). A change to this string
// without updating those call sites is the review's Important 3.
func TestUniquenessRuleRequiredRuleMissingError_ErrorMessage(t *testing.T) {
	err := &RequiredRuleMissingError{RuleName: "claim_for_task", Label: "Claim"}
	want := `required uniqueness rule "claim_for_task" for label "Claim" is not registered`
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
