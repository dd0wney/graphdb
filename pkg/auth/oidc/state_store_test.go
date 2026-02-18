package oidc

import (
	"sync"
	"testing"
	"time"
)

func TestStateStore_GenerateAndValidate(t *testing.T) {
	store := NewStateStore()
	defer store.Close()

	// Generate state
	state, err := store.GenerateState()
	if err != nil {
		t.Fatalf("Failed to generate state: %v", err)
	}

	if state == "" {
		t.Error("Expected non-empty state")
	}

	// Validate and consume
	entry, valid := store.ValidateAndConsume(state)
	if !valid {
		t.Error("Expected state to be valid")
	}
	if entry == nil {
		t.Error("Expected entry to be returned")
	}
	if entry.Nonce == "" {
		t.Error("Expected nonce to be set")
	}

	// State should be consumed (one-time use)
	_, valid = store.ValidateAndConsume(state)
	if valid {
		t.Error("Expected state to be invalid after consumption")
	}
}

func TestStateStore_InvalidState(t *testing.T) {
	store := NewStateStore()
	defer store.Close()

	// Unknown state
	_, valid := store.ValidateAndConsume("unknown-state")
	if valid {
		t.Error("Expected unknown state to be invalid")
	}
}

func TestStateStore_ExpiredState(t *testing.T) {
	// Create store with very short TTL
	store := NewStateStoreWithTTL(10 * time.Millisecond)
	defer store.Close()

	state, err := store.GenerateState()
	if err != nil {
		t.Fatalf("Failed to generate state: %v", err)
	}

	// Wait for expiry
	time.Sleep(20 * time.Millisecond)

	// State should be expired
	_, valid := store.ValidateAndConsume(state)
	if valid {
		t.Error("Expected expired state to be invalid")
	}
}

func TestStateStore_MaxEntries(t *testing.T) {
	store := NewStateStore()
	defer store.Close()

	// Generate many states to trigger eviction
	for i := 0; i < MaxStateEntries+100; i++ {
		_, err := store.GenerateState()
		if err != nil {
			t.Fatalf("Failed to generate state %d: %v", i, err)
		}
	}

	// Should have evicted some entries
	if store.Len() > MaxStateEntries {
		t.Errorf("Expected at most %d entries, got %d", MaxStateEntries, store.Len())
	}
}

func TestStateStore_Len(t *testing.T) {
	store := NewStateStore()
	defer store.Close()

	if store.Len() != 0 {
		t.Error("Expected empty store")
	}

	_, _ = store.GenerateState()
	if store.Len() != 1 {
		t.Errorf("Expected 1 entry, got %d", store.Len())
	}

	_, _ = store.GenerateState()
	if store.Len() != 2 {
		t.Errorf("Expected 2 entries, got %d", store.Len())
	}
}

func TestStateStore_Close(t *testing.T) {
	store := NewStateStore()

	// Generate some states
	for i := 0; i < 5; i++ {
		_, _ = store.GenerateState()
	}

	// Close should not block
	done := make(chan struct{})
	go func() {
		store.Close()
		close(done)
	}()

	select {
	case <-done:
		// Success - Close returned
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return in time - possible goroutine leak")
	}
}

func TestStateStore_CloseStopsCleanup(t *testing.T) {
	store := NewStateStore()

	// Close the store
	store.Close()

	// wg.Wait() was called in Close(), so cleanup goroutine has stopped
	// Generate should still work (no panic)
	_, err := store.GenerateState()
	if err != nil {
		t.Errorf("GenerateState after Close should still work: %v", err)
	}
}

func TestStateStore_ConcurrentAccess(t *testing.T) {
	store := NewStateStore()
	defer store.Close()

	var generateWg sync.WaitGroup
	states := make(chan string, 100)

	// Concurrent generators
	for i := 0; i < 10; i++ {
		generateWg.Add(1)
		go func() {
			defer generateWg.Done()
			for j := 0; j < 10; j++ {
				state, err := store.GenerateState()
				if err != nil {
					t.Errorf("Failed to generate state: %v", err)
					return
				}
				states <- state
			}
		}()
	}

	// Close channel when all generators are done
	go func() {
		generateWg.Wait()
		close(states)
	}()

	// Concurrent validators - drain the channel
	validatorDone := make(chan struct{})
	go func() {
		for state := range states {
			store.ValidateAndConsume(state)
		}
		close(validatorDone)
	}()

	// Wait for validator with timeout
	select {
	case <-validatorDone:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out - possible deadlock")
	}
}

func TestStateStore_DefaultTTL(t *testing.T) {
	// Test with 0 TTL defaults to DefaultStateTTL
	store := NewStateStoreWithTTL(0)
	defer store.Close()

	if store.ttl != DefaultStateTTL {
		t.Errorf("Expected default TTL %v, got %v", DefaultStateTTL, store.ttl)
	}

	// Test negative TTL
	store2 := NewStateStoreWithTTL(-1 * time.Second)
	defer store2.Close()

	if store2.ttl != DefaultStateTTL {
		t.Errorf("Expected default TTL for negative value %v, got %v", DefaultStateTTL, store2.ttl)
	}
}
