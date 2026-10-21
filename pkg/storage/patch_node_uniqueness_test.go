package storage

import (
	"errors"
	"testing"
)

const patchTenant = "patch-uniq-tenant"

// widgetPair makes the Widget.sku rule and two nodes: A holds w-1, B holds w-2.
func widgetPair(t *testing.T) (gs *GraphStorage, a, b *Node) {
	t.Helper()
	gs = newUpsertStore(t)
	registerRule(t, gs, "widget_sku", "Widget", "sku")
	var err error
	a, err = gs.CreateNodeWithTenant(patchTenant, []string{"Widget"}, map[string]Value{"sku": StringValue("w-1")})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err = gs.CreateNodeWithTenant(patchTenant, []string{"Widget"}, map[string]Value{"sku": StringValue("w-2")})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	return gs, a, b
}

func skuOf(t *testing.T, gs *GraphStorage, id uint64) string {
	t.Helper()
	n, err := gs.GetNodeForTenant(id, patchTenant)
	if err != nil {
		t.Fatalf("GetNodeForTenant(%d): %v", id, err)
	}
	s, _ := n.Properties["sku"].AsString()
	return s
}

func TestPatchNode_RefusesValueAnotherNodeHolds(t *testing.T) {
	gs, a, b := widgetPair(t)
	lsn := gs.GetCurrentLSN()

	err := gs.PatchNodeForTenant(b.ID, map[string]Value{"sku": StringValue("w-1")}, nil, patchTenant)
	var uerr *UniqueConstraintError
	if !errors.As(err, &uerr) {
		t.Fatalf("PatchNodeForTenant err = %v, want *UniqueConstraintError", err)
	}
	if uerr.ConflictingNodeID != a.ID {
		t.Errorf("ConflictingNodeID = %d, want %d", uerr.ConflictingNodeID, a.ID)
	}
	if got := skuOf(t, gs, b.ID); got != "w-2" {
		t.Errorf("B sku = %q after a refused patch, want w-2", got)
	}
	if got := gs.GetCurrentLSN(); got != lsn {
		t.Errorf("WAL LSN %d -> %d, want no entry for a refused patch", lsn, got)
	}
}

func TestUpdateNode_RefusesValueAnotherNodeHolds(t *testing.T) {
	gs, a, b := widgetPair(t)
	err := gs.UpdateNodeForTenant(b.ID, map[string]Value{"sku": StringValue("w-1")}, patchTenant)
	var uerr *UniqueConstraintError
	if !errors.As(err, &uerr) || uerr.ConflictingNodeID != a.ID {
		t.Fatalf("UpdateNodeForTenant err = %v, want *UniqueConstraintError naming %d", err, a.ID)
	}
}

func TestPatchNode_AllowsFreeOwnAndRemovedValues(t *testing.T) {
	gs, _, b := widgetPair(t)

	if err := gs.PatchNodeForTenant(b.ID, map[string]Value{"sku": StringValue("w-3")}, nil, patchTenant); err != nil {
		t.Fatalf("free value: %v", err)
	}
	if got := skuOf(t, gs, b.ID); got != "w-3" {
		t.Errorf("sku = %q, want w-3", got)
	}
	if err := gs.PatchNodeForTenant(b.ID, map[string]Value{"sku": StringValue("w-3")}, nil, patchTenant); err != nil {
		t.Fatalf("own value: %v", err)
	}
	if err := gs.PatchNodeForTenant(b.ID, nil, []string{"sku"}, patchTenant); err != nil {
		t.Fatalf("removal: %v", err)
	}
	n, err := gs.GetNodeForTenant(b.ID, patchTenant)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, has := n.Properties["sku"]; has {
		t.Error("sku still present after removal")
	}
}

// A key in both set and remove ends removed (set, then remove), so it cannot
// leave a duplicate and must not be refused.
func TestPatchNode_SetAndRemoveSameKeyIsNotRefused(t *testing.T) {
	gs, _, b := widgetPair(t)
	if err := gs.PatchNodeForTenant(b.ID, map[string]Value{"sku": StringValue("w-1")}, []string{"sku"}, patchTenant); err != nil {
		t.Fatalf("set+remove of one key: %v", err)
	}
}

func TestPatchNode_RuleOnOtherLabelDoesNotRefuse(t *testing.T) {
	gs := newUpsertStore(t)
	registerRule(t, gs, "widget_sku", "Widget", "sku")
	if _, err := gs.CreateNodeWithTenant(patchTenant, []string{"Widget"}, map[string]Value{"sku": StringValue("w-1")}); err != nil {
		t.Fatalf("create widget: %v", err)
	}
	gadget, err := gs.CreateNodeWithTenant(patchTenant, []string{"Gadget"}, map[string]Value{"sku": StringValue("g-1")})
	if err != nil {
		t.Fatalf("create gadget: %v", err)
	}
	if err := gs.PatchNodeForTenant(gadget.ID, map[string]Value{"sku": StringValue("w-1")}, nil, patchTenant); err != nil {
		t.Fatalf("patch of a node without the rule's label: %v", err)
	}
}
