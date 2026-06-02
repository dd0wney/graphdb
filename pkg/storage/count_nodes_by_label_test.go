package storage

import "testing"

// TestCountNodesByLabelForTenant pins that the index-level count matches the
// length of the materialized bucket (the value the old clone-everything path
// returned) across present, absent, and cross-tenant cases — without
// depending on cloning.
func TestCountNodesByLabelForTenant(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	for i := 0; i < 7; i++ {
		if _, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil); err != nil {
			t.Fatalf("create acme Doc: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := gs.CreateNodeWithTenant("acme", []string{"Note"}, nil); err != nil {
			t.Fatalf("create acme Note: %v", err)
		}
	}
	// Another tenant with the same label — must not be counted.
	for i := 0; i < 4; i++ {
		if _, err := gs.CreateNodeWithTenant("other", []string{"Doc"}, nil); err != nil {
			t.Fatalf("create other Doc: %v", err)
		}
	}

	cases := []struct {
		tenant, label string
		want          int
	}{
		{"acme", "Doc", 7},
		{"acme", "Note", 2},
		{"acme", "Missing", 0},
		{"other", "Doc", 4},
		{"ghost", "Doc", 0}, // tenant with no data
	}
	for _, c := range cases {
		got := gs.CountNodesByLabelForTenant(c.tenant, c.label)
		if got != c.want {
			t.Errorf("CountNodesByLabelForTenant(%q,%q)=%d, want %d", c.tenant, c.label, got, c.want)
		}
		// Parity with the path it replaces: len(GetNodesByLabelForTenant).
		if ref := len(gs.GetNodesByLabelForTenant(c.tenant, c.label)); ref != got {
			t.Errorf("count %d disagrees with len(GetNodesByLabelForTenant)=%d for (%q,%q)", got, ref, c.tenant, c.label)
		}
	}
}
