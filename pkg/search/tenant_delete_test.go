package search

import "testing"

// TestDeleteLSASnapshot_NoResurrectionOnLoadAll is the #223 LSA durability
// teeth: deleting a tenant must remove its on-disk .lsa snapshot, or LoadAll
// resurrects the deleted tenant's index on the next restart. Registry-only
// Delete is NOT enough.
func TestDeleteLSASnapshot_NoResurrectionOnLoadAll(t *testing.T) {
	dir := t.TempDir()
	reg := NewTenantLSAIndexes()
	reg.Set("victim", buildTinyLSA(t))
	reg.Set("keep", buildTinyLSA(t))
	if err := reg.SaveAll(dir); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Delete the victim tenant: in-memory entry + on-disk snapshot.
	reg.Delete("victim")
	if err := DeleteLSASnapshot(dir, "victim"); err != nil {
		t.Fatalf("DeleteLSASnapshot: %v", err)
	}

	// A fresh registry loading from disk must NOT bring victim back.
	reloaded := NewTenantLSAIndexes()
	if err := reloaded.LoadAll(dir); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if reloaded.Get("victim") != nil {
		t.Errorf("victim LSA index resurrected by LoadAll after delete — on-disk snapshot not removed")
	}
	if reloaded.Get("keep") == nil {
		t.Errorf("bystander LSA index 'keep' lost — blast radius too wide")
	}
}

func TestDeleteLSASnapshot_MissingFileIsOK(t *testing.T) {
	if err := DeleteLSASnapshot(t.TempDir(), "no-such-tenant"); err != nil {
		t.Errorf("DeleteLSASnapshot on a missing file = %v, want nil", err)
	}
}
