package storage

import (
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// newTestGraphStorage builds an in-memory-ish GraphStorage backed by a
// temp data dir; sufficient for these unit-style checks of the
// uniqueness primitive without exercising the disk-backed edge cache.
func newTestGraphStorage(t *testing.T) *GraphStorage {
	t.Helper()
	dir := t.TempDir()
	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() {
		_ = gs.Close()
		_ = os.RemoveAll(dir)
	})
	return gs
}

// TestCreateNodeWithUniquePropertyForTenant covers the B-lite atomic
// uniqueness primitive used by the GraphQL :Claim resolver.
//
// Asserts:
//   - first create succeeds
//   - second sequential create with the same (label, prop) is rejected
//   - rejection unwraps to ErrUniqueConstraintViolation
//   - the conflicting node ID is reported
//   - different values for the unique property both succeed
//   - tenants are isolated — same value in two tenants both succeed
func TestCreateNodeWithUniquePropertyForTenant(t *testing.T) {
	gs := newTestGraphStorage(t)

	tenantA := "tenant-a"
	tenantB := "tenant-b"

	first, err := gs.CreateNodeWithUniquePropertyForTenant(
		tenantA,
		[]string{"Claim"},
		map[string]Value{"for_task": StringValue("graphdb:H4-PR1-blite")},
		"Claim",
		"for_task",
	)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = gs.CreateNodeWithUniquePropertyForTenant(
		tenantA,
		[]string{"Claim"},
		map[string]Value{"for_task": StringValue("graphdb:H4-PR1-blite")},
		"Claim",
		"for_task",
	)
	if err == nil {
		t.Fatalf("second create should have failed with conflict, got nil error")
	}
	if !errors.Is(err, ErrUniqueConstraintViolation) {
		t.Fatalf("expected ErrUniqueConstraintViolation, got %T: %v", err, err)
	}
	var ucErr *UniqueConstraintError
	if !errors.As(err, &ucErr) {
		t.Fatalf("expected *UniqueConstraintError via errors.As, got %T", err)
	}
	if ucErr.ConflictingNodeID != first.ID {
		t.Errorf("conflicting node ID = %d, want %d", ucErr.ConflictingNodeID, first.ID)
	}
	if ucErr.Label != "Claim" || ucErr.PropertyKey != "for_task" {
		t.Errorf("conflict carries wrong label/property: %+v", ucErr)
	}

	// Different value → should succeed.
	if _, err := gs.CreateNodeWithUniquePropertyForTenant(
		tenantA,
		[]string{"Claim"},
		map[string]Value{"for_task": StringValue("graphdb:H4-PR2-skill")},
		"Claim",
		"for_task",
	); err != nil {
		t.Errorf("different for_task should succeed: %v", err)
	}

	// Same value in a different tenant → should succeed (tenant scope).
	if _, err := gs.CreateNodeWithUniquePropertyForTenant(
		tenantB,
		[]string{"Claim"},
		map[string]Value{"for_task": StringValue("graphdb:H4-PR1-blite")},
		"Claim",
		"for_task",
	); err != nil {
		t.Errorf("same value in tenant B should succeed: %v", err)
	}
}

// TestCreateNodeWithUniquePropertyForTenant_InputValidation guards
// against forms of the call that don't make sense — surfaces the
// problem early instead of silently misbehaving.
func TestCreateNodeWithUniquePropertyForTenant_InputValidation(t *testing.T) {
	gs := newTestGraphStorage(t)

	tests := []struct {
		name       string
		labels     []string
		properties map[string]Value
		uniqLabel  string
		uniqProp   string
		wantErr    string
	}{
		{
			name:       "missing uniqueLabel",
			labels:     []string{"Claim"},
			properties: map[string]Value{"for_task": StringValue("x")},
			uniqLabel:  "",
			uniqProp:   "for_task",
			wantErr:    "uniqueLabel and uniquePropertyKey are required",
		},
		{
			name:       "missing uniquePropertyKey",
			labels:     []string{"Claim"},
			properties: map[string]Value{"for_task": StringValue("x")},
			uniqLabel:  "Claim",
			uniqProp:   "",
			wantErr:    "uniqueLabel and uniquePropertyKey are required",
		},
		{
			name:       "uniqueLabel not in labels",
			labels:     []string{"Other"},
			properties: map[string]Value{"for_task": StringValue("x")},
			uniqLabel:  "Claim",
			uniqProp:   "for_task",
			wantErr:    "uniqueLabel \"Claim\" must be present in labels",
		},
		{
			name:       "missing required property",
			labels:     []string{"Claim"},
			properties: map[string]Value{"other": StringValue("x")},
			uniqLabel:  "Claim",
			uniqProp:   "for_task",
			wantErr:    "property \"for_task\" is required for uniqueness check",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gs.CreateNodeWithUniquePropertyForTenant(
				"tenant-a", tt.labels, tt.properties, tt.uniqLabel, tt.uniqProp,
			)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if got := err.Error(); got != tt.wantErr {
				t.Errorf("error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

// TestCreateNodeWithUniquePropertyForTenant_Concurrent verifies the
// atomicity contract: N goroutines racing to create a Claim with the
// same for_task value yield exactly one success and N-1 conflicts.
//
// This is the test that justifies the storage-level enforcement; a
// resolver-only check would let multiple successes through under
// contention because the read- and write-locks are separate
// acquisitions.
func TestCreateNodeWithUniquePropertyForTenant_Concurrent(t *testing.T) {
	gs := newTestGraphStorage(t)

	const numGoroutines = 32
	var (
		wg          sync.WaitGroup
		successes   atomic.Uint64
		conflicts   atomic.Uint64
		otherErrors atomic.Uint64
	)

	start := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			_, err := gs.CreateNodeWithUniquePropertyForTenant(
				"default",
				[]string{"Claim"},
				map[string]Value{
					"for_task":   StringValue("graphdb:race-test"),
					"started_by": IntValue(int64(idx)),
				},
				"Claim",
				"for_task",
			)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrUniqueConstraintViolation):
				conflicts.Add(1)
			default:
				otherErrors.Add(1)
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
			}
		}(i)
	}

	close(start)
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Errorf("successes = %d, want exactly 1", got)
	}
	if got := conflicts.Load(); got != numGoroutines-1 {
		t.Errorf("conflicts = %d, want %d", got, numGoroutines-1)
	}
	if got := otherErrors.Load(); got != 0 {
		t.Errorf("unexpected other errors: %d", got)
	}

	// Verify the storage actually contains exactly one Claim node.
	claims := gs.GetNodesByLabelForTenant("default", "Claim")
	if len(claims) != 1 {
		t.Errorf("storage holds %d :Claim nodes, want 1", len(claims))
	}
}

// TestCreateNodeWithUniquePropertyForTenant_DifferentTypesNotEqual
// verifies that the type tag is part of equality. A StringValue("1")
// and an IntValue(1) must NOT collide, even though their decoded
// representations rhyme.
func TestCreateNodeWithUniquePropertyForTenant_DifferentTypesNotEqual(t *testing.T) {
	gs := newTestGraphStorage(t)

	_, err := gs.CreateNodeWithUniquePropertyForTenant(
		"default", []string{"Claim"},
		map[string]Value{"for_task": StringValue("1")},
		"Claim", "for_task",
	)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = gs.CreateNodeWithUniquePropertyForTenant(
		"default", []string{"Claim"},
		map[string]Value{"for_task": IntValue(1)},
		"Claim", "for_task",
	)
	if err != nil {
		t.Errorf("IntValue(1) should not collide with StringValue(\"1\"): %v", err)
	}
}

// TestUniqueConstraintError_ResponseBodySafe pins the contract that
// UniqueConstraintError.Error() does NOT include the tenant identifier in
// its formatted message, so the string can be safely forwarded to HTTP or
// GraphQL response bodies without leaking the operating tenant. The
// TenantID struct field remains accessible via errors.As for legitimate
// internal observers (logs, audit). See docs/AUDIT_error_sanitization_2026-05-11.md.
func TestUniqueConstraintError_ResponseBodySafe(t *testing.T) {
	uc := &UniqueConstraintError{
		Label:             "Claim",
		PropertyKey:       "for_task",
		ConflictingNodeID: 42,
		TenantID:          "secret-tenant-id",
	}
	msg := uc.Error()

	if strings.Contains(msg, "secret-tenant-id") {
		t.Errorf("Error() leaks TenantID into formatted message: %q", msg)
	}
	if strings.Contains(strings.ToLower(msg), "tenant") {
		t.Errorf("Error() contains the substring \"tenant\": %q — must not appear in response-body-safe format", msg)
	}

	// Required content: label, property, node ID, the class prefix.
	for _, want := range []string{"unique constraint violation", "label=Claim", "property=for_task", "node 42"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing required substring %q", msg, want)
		}
	}

	// TenantID remains accessible via the struct field for internal use.
	var ucAs *UniqueConstraintError
	if !errors.As(error(uc), &ucAs) {
		t.Fatalf("errors.As should match *UniqueConstraintError")
	}
	if ucAs.TenantID != "secret-tenant-id" {
		t.Errorf("TenantID field = %q, want %q", ucAs.TenantID, "secret-tenant-id")
	}
}
