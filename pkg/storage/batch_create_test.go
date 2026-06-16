package storage

import "testing"

func TestCreateNodesAndEdgesWithTenant(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Close()
	const tenant = "t1"

	nodeSpecs := []NodeSpec{
		{Labels: []string{"A"}, Properties: map[string]Value{"k": StringValue("v0")}},
		{Labels: []string{"A"}, Properties: map[string]Value{"k": StringValue("v1")}},
		{Labels: []string{"B"}, Properties: map[string]Value{"k": StringValue("v2")}},
	}
	ids, err := gs.CreateNodesWithTenant(tenant, nodeSpecs)
	if err != nil {
		t.Fatalf("CreateNodesWithTenant: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("got %d ids, want 3", len(ids))
	}
	if _, err := gs.GetNodeForTenant(ids[0], tenant); err != nil {
		t.Errorf("node 0 not found: %v", err)
	}
	if got := len(gs.GetNodesByLabelForTenant(tenant, "A")); got != 2 {
		t.Errorf("label A count = %d, want 2", got)
	}

	edgeSpecs := []EdgeSpec{
		{FromID: ids[0], ToID: ids[1], Type: "E"},
		{FromID: ids[1], ToID: ids[2], Type: "E"},
	}
	eids, err := gs.CreateEdgesWithTenant(tenant, edgeSpecs)
	if err != nil {
		t.Fatalf("CreateEdgesWithTenant: %v", err)
	}
	if len(eids) != 2 {
		t.Fatalf("got %d edge ids, want 2", len(eids))
	}
	out, err := gs.GetOutgoingEdgesForTenant(ids[0], tenant)
	if err != nil || len(out) != 1 || out[0].ToNodeID != ids[1] {
		t.Errorf("outgoing of node0 = %v, err %v", out, err)
	}
}
