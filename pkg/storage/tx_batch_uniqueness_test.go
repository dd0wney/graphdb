package storage

import (
	"errors"
	"os"
	"testing"
)

// The storage Batch and Transaction are the embedded-API write paths. Before
// this file they never read the uniqueness-rules registry (ADR 0001), so an
// embedded caller could store a second holder of a registered key with no
// error, while every REST, GraphQL and Cypher path refused it.

const txUniqTenant = "tx-uniq-tenant"

// skuHolders counts the nodes of the tenant in the Widget bucket whose sku is
// val, through the same scan the unique create uses.
func skuHolders(t *testing.T, gs *GraphStorage, tenant, val string) int {
	t.Helper()
	gs.mu.Lock()
	defer gs.mu.Unlock()
	holders, err := gs.scanKeyHoldersLocked(effectiveTenantID(tenant), "Widget", "sku", StringValue(val))
	if err != nil {
		t.Fatalf("scanKeyHoldersLocked(%s): %v", val, err)
	}
	return len(holders)
}

func skuIn(t *testing.T, gs *GraphStorage, tenant string, id uint64) string {
	t.Helper()
	n, err := gs.GetNodeForTenant(id, tenant)
	if err != nil {
		t.Fatalf("GetNodeForTenant(%d): %v", id, err)
	}
	s, _ := n.Properties["sku"].AsString()
	return s
}

// widgetPairIn makes the Widget.sku rule and two nodes in tenant: A holds w-1,
// B holds w-2.
func widgetPairIn(t *testing.T, tenant string) (gs *GraphStorage, a, b *Node) {
	t.Helper()
	gs = newUpsertStore(t)
	registerRule(t, gs, "widget_sku", "Widget", "sku")
	var err error
	a, err = gs.CreateNodeWithTenant(tenant, []string{"Widget"}, map[string]Value{"sku": StringValue("w-1")})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err = gs.CreateNodeWithTenant(tenant, []string{"Widget"}, map[string]Value{"sku": StringValue("w-2")})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	return gs, a, b
}

func sku(v string) map[string]Value { return map[string]Value{"sku": StringValue(v)} }

func wantUniqueConflict(t *testing.T, err error, conflicting uint64) {
	t.Helper()
	var uerr *UniqueConstraintError
	if !errors.As(err, &uerr) {
		t.Fatalf("err = %v, want *UniqueConstraintError", err)
	}
	if uerr.ConflictingNodeID != conflicting {
		t.Errorf("ConflictingNodeID = %d, want %d", uerr.ConflictingNodeID, conflicting)
	}
}

// --- Batch: tenant-blind, so every node is in the default tenant. ---

func TestBatch_CreateRefusesHeldKey(t *testing.T) {
	gs, a, _ := widgetPairIn(t, DefaultTenantID)

	b := gs.BeginBatch()
	if _, err := b.AddNode([]string{"Widget"}, sku("w-1")); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	wantUniqueConflict(t, b.Commit(), a.ID)

	if n := skuHolders(t, gs, DefaultTenantID, "w-1"); n != 1 {
		t.Errorf("w-1 holders = %d after a refused batch create, want 1", n)
	}
}

// A Batch applies its operations in order and keeps the ones before a failure
// (v1.4-storage-batch-atomic-commit owns that), so the second create of one
// key is refused against the first.
func TestBatch_CreateRefusesKeyAnEarlierOpCreated(t *testing.T) {
	gs, _, _ := widgetPairIn(t, DefaultTenantID)

	b := gs.BeginBatch()
	first, err := b.AddNode([]string{"Widget"}, sku("w-5"))
	if err != nil {
		t.Fatalf("AddNode first: %v", err)
	}
	if _, err := b.AddNode([]string{"Widget"}, sku("w-5")); err != nil {
		t.Fatalf("AddNode second: %v", err)
	}
	wantUniqueConflict(t, b.Commit(), first)

	if n := skuHolders(t, gs, DefaultTenantID, "w-5"); n != 1 {
		t.Errorf("w-5 holders = %d, want 1", n)
	}
}

func TestBatch_UpdateRefusesHeldKey(t *testing.T) {
	gs, a, bNode := widgetPairIn(t, DefaultTenantID)
	lsn := gs.GetCurrentLSN()

	b := gs.BeginBatch()
	b.UpdateNode(bNode.ID, sku("w-1"))
	wantUniqueConflict(t, b.Commit(), a.ID)

	if got := skuIn(t, gs, DefaultTenantID, bNode.ID); got != "w-2" {
		t.Errorf("B sku = %q after a refused batch update, want w-2", got)
	}
	if got := gs.GetCurrentLSN(); got != lsn {
		t.Errorf("WAL LSN %d -> %d, want no entry for a refused update", lsn, got)
	}
}

func TestBatch_CreateRefusesMissingKeyProperty(t *testing.T) {
	gs, _, _ := widgetPairIn(t, DefaultTenantID)

	b := gs.BeginBatch()
	if _, err := b.AddNode([]string{"Widget"}, map[string]Value{"name": StringValue("no sku")}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := b.Commit(); !errors.Is(err, ErrUniquenessRulePropertyMissing) {
		t.Fatalf("Commit err = %v, want ErrUniquenessRulePropertyMissing", err)
	}
}

func TestBatch_CreateRefusesWhenRequiredRuleMissing(t *testing.T) {
	cfg := DefaultStorageConfig(t.TempDir())
	cfg.UseMmapSnapshot = false
	cfg.RequiredUniquenessRules = []RequiredUniquenessRule{{Name: "widget_sku", Label: "Widget"}}
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	b := gs.BeginBatch()
	if _, err := b.AddNode([]string{"Widget"}, sku("w-1")); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	var missing *RequiredRuleMissingError
	if err := b.Commit(); !errors.As(err, &missing) {
		t.Fatalf("Commit err = %v, want *RequiredRuleMissingError", err)
	}
}

func TestBatch_AllowsFreeAndOwnValues(t *testing.T) {
	gs, _, bNode := widgetPairIn(t, DefaultTenantID)

	b := gs.BeginBatch()
	b.UpdateNode(bNode.ID, sku("w-2"))
	if _, err := b.AddNode([]string{"Widget"}, sku("w-3")); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := b.AddNode([]string{"Gadget"}, sku("w-1")); err != nil {
		t.Fatalf("AddNode Gadget: %v", err)
	}
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// --- Transaction: tenant-bound, and a refused commit applies nothing. ---

func TestTransaction_CreateRefusesHeldKeyAndAppliesNothing(t *testing.T) {
	gs, a, _ := widgetPairIn(t, txUniqTenant)
	lsn := gs.GetCurrentLSN()

	tx, err := gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if _, err := tx.CreateNode([]string{"Widget"}, sku("w-9")); err != nil {
		t.Fatalf("CreateNode free: %v", err)
	}
	if _, err := tx.CreateNode([]string{"Widget"}, sku("w-1")); err != nil {
		t.Fatalf("CreateNode held: %v", err)
	}
	wantUniqueConflict(t, tx.Commit(), a.ID)

	if n := skuHolders(t, gs, txUniqTenant, "w-1"); n != 1 {
		t.Errorf("w-1 holders = %d, want 1", n)
	}
	if n := skuHolders(t, gs, txUniqTenant, "w-9"); n != 0 {
		t.Errorf("w-9 holders = %d, want 0: a refused commit must apply none of its creates", n)
	}
	if got := gs.GetCurrentLSN(); got != lsn {
		t.Errorf("WAL LSN %d -> %d, want no entry for a refused commit", lsn, got)
	}
}

func TestTransaction_RefusesTwoCreatesOfOneKey(t *testing.T) {
	gs, _, _ := widgetPairIn(t, txUniqTenant)

	tx, err := gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := tx.CreateNode([]string{"Widget"}, sku("w-5")); err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
	}
	var uerr *UniqueConstraintError
	if err := tx.Commit(); !errors.As(err, &uerr) {
		t.Fatalf("Commit err = %v, want *UniqueConstraintError", err)
	}
	if n := skuHolders(t, gs, txUniqTenant, "w-5"); n != 0 {
		t.Errorf("w-5 holders = %d, want 0", n)
	}
}

func TestTransaction_UpdateRefusesHeldKey(t *testing.T) {
	gs, a, b := widgetPairIn(t, txUniqTenant)

	tx, err := gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if err := tx.UpdateNode(b.ID, sku("w-1")); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	wantUniqueConflict(t, tx.Commit(), a.ID)

	if got := skuIn(t, gs, txUniqTenant, b.ID); got != "w-2" {
		t.Errorf("B sku = %q after a refused commit, want w-2", got)
	}
}

// An update of a node the same transaction created is merged into the buffered
// create, so it is checked as a create.
func TestTransaction_UpdateOfBufferedCreateRefusesHeldKey(t *testing.T) {
	gs, a, _ := widgetPairIn(t, txUniqTenant)

	tx, err := gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	c, err := tx.CreateNode([]string{"Widget"}, sku("w-9"))
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := tx.UpdateNode(c.ID, sku("w-1")); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	wantUniqueConflict(t, tx.Commit(), a.ID)
}

// The check is on the state the commit leaves, not on each buffered write in
// turn: a transaction is one write. A swap, and a create of a value another
// write in the same transaction gives up, both end with one holder per value.
func TestTransaction_AllowsSwapAndReleasedValue(t *testing.T) {
	gs, a, b := widgetPairIn(t, txUniqTenant)

	tx, err := gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if err := tx.UpdateNode(a.ID, sku("w-2")); err != nil {
		t.Fatalf("UpdateNode A: %v", err)
	}
	if err := tx.UpdateNode(b.ID, sku("w-1")); err != nil {
		t.Fatalf("UpdateNode B: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("swap Commit: %v", err)
	}
	if got := skuIn(t, gs, txUniqTenant, a.ID); got != "w-2" {
		t.Errorf("A sku = %q, want w-2", got)
	}

	tx, err = gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if err := tx.UpdateNode(a.ID, sku("w-7")); err != nil {
		t.Fatalf("UpdateNode A: %v", err)
	}
	if _, err := tx.CreateNode([]string{"Widget"}, sku("w-2")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("released-value Commit: %v", err)
	}
	if n := skuHolders(t, gs, txUniqTenant, "w-2"); n != 1 {
		t.Errorf("w-2 holders = %d, want 1", n)
	}
}

func TestTransaction_CreateRefusesMissingKeyProperty(t *testing.T) {
	gs, _, _ := widgetPairIn(t, txUniqTenant)

	tx, err := gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if _, err := tx.CreateNode([]string{"Widget"}, map[string]Value{"name": StringValue("no sku")}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := tx.Commit(); !errors.Is(err, ErrUniquenessRulePropertyMissing) {
		t.Fatalf("Commit err = %v, want ErrUniquenessRulePropertyMissing", err)
	}
}

// The rule is per tenant: another tenant's holder is not a conflict.
func TestTransaction_HolderInAnotherTenantIsNoConflict(t *testing.T) {
	gs, _, _ := widgetPairIn(t, txUniqTenant)

	tx, err := gs.BeginTransactionForTenant("other-tenant")
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if _, err := tx.CreateNode([]string{"Widget"}, sku("w-1")); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// Two rule names may cover one (Label, PropertyKey) when a rules.json written
// before RegisterUniquenessRule refused that pair is loaded (the loader keeps
// it). A transaction update then makes one key claim for each rule, both for
// the same node, and the node must not conflict with itself. The direct
// update path accepts the same write, and a real conflict must name the other
// holder, not the node being written.
func TestTransaction_TwoRulesOnOneKeyDoNotConflictWithTheSameNode(t *testing.T) {
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
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	if got := len(gs.UniquenessRules()); got != 2 {
		t.Fatalf("loaded %d rules, want the duplicate pair", got)
	}
	a, err := gs.CreateNodeWithTenant(txUniqTenant, []string{"Widget"}, sku("w-1"))
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := gs.CreateNodeWithTenant(txUniqTenant, []string{"Widget"}, sku("w-2"))
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	tx, err := gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if err := tx.UpdateNode(a.ID, sku("w-9")); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit of a free value under two rules on one key: %v", err)
	}

	tx, err = gs.BeginTransactionForTenant(txUniqTenant)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if err := tx.UpdateNode(a.ID, sku("w-2")); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	wantUniqueConflict(t, tx.Commit(), b.ID)
}
