package storage

import (
	"errors"
	"testing"
)

// assertGraphInvariants verifies that every DERIVED representation of the graph
// agrees with the AUTHORITATIVE shard maps, failing the test with one error per
// divergence. See CheckInvariants for the full contract; this is the thin
// *testing.T wrapper retrofitted into tests.
func assertGraphInvariants(t *testing.T, gs *GraphStorage) {
	t.Helper()
	for _, v := range mustCheckInvariants(t, gs) {
		t.Error("invariant violation: " + v)
	}
}

// mustCheckInvariants runs CheckInvariants and fails the test if the store is
// one the checker cannot inspect. A test that silently skipped the check would
// look identical to a test that passed it.
func mustCheckInvariants(t *testing.T, gs *GraphStorage) []string {
	t.Helper()
	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	return violations
}

// TestCheckInvariants_RefusesMmapBackedStore proves the guard fires. The check
// rebuilds ground truth from the shard maps, which an mmap-backed store fills
// on demand, so running it there would report absent-but-healthy records as
// violations. It must refuse rather than answer.
func TestCheckInvariants_RefusesMmapBackedStore(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	if _, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"n": StringValue("one")}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	// Close writes snapshot.mmap, which the reopen below maps lazily.
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	if reopened.mmapSnap == nil {
		t.Skip("reopen did not take the mmap path; nothing to guard against here")
	}

	violations, err := CheckInvariants(reopened)
	if !errors.Is(err, ErrInvariantsUnsupported) {
		t.Fatalf("CheckInvariants error = %v, want ErrInvariantsUnsupported", err)
	}
	if violations != nil {
		t.Errorf("violations = %v, want nil when the check refuses", violations)
	}
}

// TestCheckInvariants_InspectsJSONBackedStore is the positive control for the
// test above: the guard must be specific to the mmap path, not a blanket
// refusal that quietly disables the checker everywhere.
func TestCheckInvariants_InspectsJSONBackedStore(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(jsonConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if _, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"n": StringValue("one")}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	violations, err := CheckInvariants(gs)
	if err != nil {
		t.Fatalf("CheckInvariants on a JSON-backed store: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none on a healthy store", violations)
	}
}
