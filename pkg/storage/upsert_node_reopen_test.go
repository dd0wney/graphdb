package storage

import "testing"

// TestUpsertNode_ReopenGivesSameNode proves an upsert's create and merge reach
// disk in both snapshot formats. The oracle is the JSON<->mmap fingerprint
// from mmap_reopen_test.go, taken before Close and after reopen.
func TestUpsertNode_ReopenGivesSameNode(t *testing.T) {
	modes := []struct {
		name string
		cfg  func(dir string) StorageConfig
	}{
		{"json", jsonConfig},
		{"mmap", mmapConfig},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			dir := t.TempDir()
			gs, err := NewGraphStorageWithConfig(m.cfg(dir))
			if err != nil {
				t.Fatal(err)
			}
			base, created := mustUpsert(t, gs, rtTenantA, []string{"Person"},
				map[string]Value{"name": StringValue("ada"), "age": IntValue(36)}, "Person", "name")
			if !created {
				t.Fatal("first upsert did not create")
			}
			mustUpsert(t, gs, rtTenantA, []string{"Person"},
				map[string]Value{"name": StringValue("ada"), "city": StringValue("london")}, "Person", "name")
			want := fingerprintTenant(t, gs, rtTenantA)
			if err := gs.Close(); err != nil {
				t.Fatal(err)
			}

			gs2, err := NewGraphStorageWithConfig(m.cfg(dir))
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer func() { _ = gs2.Close() }()
			assertFingerprintEqual(t, want, fingerprintTenant(t, gs2, rtTenantA), m.name+" reopen")

			node, err := gs2.GetNodeForTenant(base.ID, rtTenantA)
			if err != nil {
				t.Fatal(err)
			}
			if c, _ := node.Properties["city"].AsString(); c != "london" {
				t.Errorf("city = %q after reopen, want london", c)
			}
			if a, _ := node.Properties["age"].AsInt(); a != 36 {
				t.Errorf("age = %d after reopen, want 36", a)
			}

			// An upsert against the reopened store matches the stored node.
			// In mmap mode that node is base-resident, so this walks the
			// copy-on-write promotion in the merge.
			again, created := mustUpsert(t, gs2, rtTenantA, []string{"Person"},
				map[string]Value{"name": StringValue("ada"), "age": IntValue(37)}, "Person", "name")
			if created || again.ID != base.ID {
				t.Fatalf("upsert after reopen: created %v node %d, want match of %d", created, again.ID, base.ID)
			}
			if a, _ := again.Properties["age"].AsInt(); a != 37 {
				t.Errorf("age = %d, want 37", a)
			}
			if gs2.CountNodesForTenant(rtTenantA) != 1 {
				t.Errorf("node count = %d after reopen+upsert, want 1", gs2.CountNodesForTenant(rtTenantA))
			}
		})
	}
}
