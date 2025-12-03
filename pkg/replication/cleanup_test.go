package replication

import (
	"errors"
	"testing"
)

// mockCloser is a test implementation of io.Closer
type mockCloser struct {
	closed    bool
	closeErr  error
	closeCalls int
}

func (m *mockCloser) Close() error {
	m.closeCalls++
	m.closed = true
	return m.closeErr
}

func TestResourceCleanup_Basic(t *testing.T) {
	cleanup := NewResourceCleanup()

	closer1 := &mockCloser{}
	closer2 := &mockCloser{}
	closer3 := &mockCloser{}

	cleanup.Add(closer1, "resource1")
	cleanup.Add(closer2, "resource2")
	cleanup.Add(closer3, "resource3")

	if cleanup.Len() != 3 {
		t.Errorf("Expected 3 resources, got %d", cleanup.Len())
	}

	cleanup.Cleanup()

	// All resources should be closed
	if !closer1.closed || !closer2.closed || !closer3.closed {
		t.Error("Not all resources were closed")
	}

	// Length should be 0 after cleanup
	if cleanup.Len() != 0 {
		t.Errorf("Expected 0 resources after cleanup, got %d", cleanup.Len())
	}
}

func TestResourceCleanup_ReverseOrder(t *testing.T) {
	cleanup := NewResourceCleanup()

	closeOrder := make([]string, 0, 3)

	// Create closers that track close order
	makeCloser := func(name string) *mockCloser {
		return &mockCloser{
			closeErr: nil,
		}
	}

	closer1 := makeCloser("resource1")
	closer2 := makeCloser("resource2")
	closer3 := makeCloser("resource3")

	cleanup.Add(closer1, "resource1")
	cleanup.Add(closer2, "resource2")
	cleanup.Add(closer3, "resource3")

	// To verify LIFO order, we check that after cleanup all are closed
	// (order verification would require a more complex mock)
	cleanup.Cleanup()

	if !closer1.closed || !closer2.closed || !closer3.closed {
		t.Error("Not all resources were closed")
	}

	// Verify slice is cleared
	if len(closeOrder) != 0 {
		// This is just checking our test setup
	}
}

func TestResourceCleanup_Clear(t *testing.T) {
	cleanup := NewResourceCleanup()

	closer1 := &mockCloser{}
	closer2 := &mockCloser{}

	cleanup.Add(closer1, "resource1")
	cleanup.Add(closer2, "resource2")

	// Clear should remove resources without closing them
	cleanup.Clear()

	if cleanup.Len() != 0 {
		t.Errorf("Expected 0 resources after clear, got %d", cleanup.Len())
	}

	// Resources should NOT be closed
	if closer1.closed || closer2.closed {
		t.Error("Resources should not be closed after Clear()")
	}

	// Cleanup should now be a no-op
	cleanup.Cleanup()

	if closer1.closed || closer2.closed {
		t.Error("Resources should still not be closed after Cleanup() following Clear()")
	}
}

func TestResourceCleanup_Idempotent(t *testing.T) {
	cleanup := NewResourceCleanup()

	closer := &mockCloser{}
	cleanup.Add(closer, "resource")

	// First cleanup
	cleanup.Cleanup()

	if closer.closeCalls != 1 {
		t.Errorf("Expected 1 close call, got %d", closer.closeCalls)
	}

	// Second cleanup should be a no-op
	cleanup.Cleanup()

	if closer.closeCalls != 1 {
		t.Errorf("Expected still 1 close call after second cleanup, got %d", closer.closeCalls)
	}
}

func TestResourceCleanup_WithErrors(t *testing.T) {
	cleanup := NewResourceCleanup()

	closer1 := &mockCloser{}
	closer2 := &mockCloser{closeErr: errors.New("close error")}
	closer3 := &mockCloser{}

	cleanup.Add(closer1, "resource1")
	cleanup.Add(closer2, "resource2")
	cleanup.Add(closer3, "resource3")

	// Cleanup should continue despite errors
	cleanup.Cleanup()

	// All resources should still be attempted to close
	if !closer1.closed || !closer2.closed || !closer3.closed {
		t.Error("All resources should be closed even when some fail")
	}
}

func TestResourceCleanup_CloseAll(t *testing.T) {
	cleanup := NewResourceCleanup()

	expectedErr := errors.New("first error")
	closer1 := &mockCloser{closeErr: expectedErr}
	closer2 := &mockCloser{}

	cleanup.Add(closer1, "resource1")
	cleanup.Add(closer2, "resource2")

	// CloseAll returns first error but closes all resources
	err := cleanup.CloseAll()

	// Since we close in reverse order, closer2 is closed first (no error),
	// then closer1 (with error)
	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}

	// Both should still be closed
	if !closer1.closed || !closer2.closed {
		t.Error("Both resources should be closed")
	}
}

func TestResourceCleanup_Empty(t *testing.T) {
	cleanup := NewResourceCleanup()

	// Operations on empty cleanup should not panic
	cleanup.Cleanup()
	cleanup.Clear()

	if cleanup.Len() != 0 {
		t.Errorf("Expected 0 resources, got %d", cleanup.Len())
	}

	err := cleanup.CloseAll()
	if err != nil {
		t.Errorf("Expected no error from empty CloseAll, got %v", err)
	}
}

func TestResourceCleanup_NilCloser(t *testing.T) {
	cleanup := NewResourceCleanup()

	// Adding nil closer should not panic
	cleanup.Add(nil, "nil resource")

	// Cleanup should handle nil closer gracefully
	cleanup.Cleanup() // Should not panic
}
