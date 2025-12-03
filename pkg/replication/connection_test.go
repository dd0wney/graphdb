package replication

import (
	"sync"
	"testing"
	"time"
)

func TestConnectionState_InitialState(t *testing.T) {
	cs := NewConnectionState()

	if cs.IsRunning() {
		t.Error("Expected IsRunning() to be false initially")
	}
	if cs.IsConnected() {
		t.Error("Expected IsConnected() to be false initially")
	}
}

func TestConnectionState_SetRunning(t *testing.T) {
	cs := NewConnectionState()

	cs.SetRunning(true)
	if !cs.IsRunning() {
		t.Error("Expected IsRunning() to be true after SetRunning(true)")
	}

	cs.SetRunning(false)
	if cs.IsRunning() {
		t.Error("Expected IsRunning() to be false after SetRunning(false)")
	}
}

func TestConnectionState_SetConnected(t *testing.T) {
	cs := NewConnectionState()

	cs.SetConnected(true)
	if !cs.IsConnected() {
		t.Error("Expected IsConnected() to be true after SetConnected(true)")
	}

	cs.SetConnected(false)
	if cs.IsConnected() {
		t.Error("Expected IsConnected() to be false after SetConnected(false)")
	}
}

func TestConnectionState_MarkStarted(t *testing.T) {
	cs := NewConnectionState()

	cs.MarkStarted()
	if !cs.IsRunning() {
		t.Error("Expected IsRunning() to be true after MarkStarted()")
	}
	if !cs.IsConnected() {
		t.Error("Expected IsConnected() to be true after MarkStarted()")
	}
}

func TestConnectionState_MarkStopped(t *testing.T) {
	cs := NewConnectionState()

	cs.MarkStarted()
	cs.MarkStopped()

	if cs.IsRunning() {
		t.Error("Expected IsRunning() to be false after MarkStopped()")
	}
	if cs.IsConnected() {
		t.Error("Expected IsConnected() to be false after MarkStopped()")
	}
}

func TestConnectionState_TryStart(t *testing.T) {
	cs := NewConnectionState()

	// First start should succeed
	unlock, alreadyRunning := cs.TryStart()
	if alreadyRunning {
		t.Error("Expected TryStart() to succeed on first call")
	}
	if unlock == nil {
		t.Fatal("Expected unlock function to be non-nil")
	}

	// Mark as running while holding the lock
	cs.SetRunning(true)
	unlock()

	// Second start should fail
	unlock2, alreadyRunning2 := cs.TryStart()
	if !alreadyRunning2 {
		t.Error("Expected TryStart() to fail when already running")
	}
	if unlock2 != nil {
		t.Error("Expected unlock function to be nil when already running")
	}
}

func TestConnectionState_TryStop(t *testing.T) {
	cs := NewConnectionState()

	// Stop when not running should fail
	unlock, notRunning := cs.TryStop()
	if !notRunning {
		t.Error("Expected TryStop() to fail when not running")
	}
	if unlock != nil {
		t.Error("Expected unlock function to be nil when not running")
	}

	// Start first
	cs.SetRunning(true)

	// Now stop should succeed
	unlock, notRunning = cs.TryStop()
	if notRunning {
		t.Error("Expected TryStop() to succeed when running")
	}
	if unlock == nil {
		t.Fatal("Expected unlock function to be non-nil")
	}
	unlock()
}

func TestConnectionState_ConcurrentAccess(t *testing.T) {
	cs := NewConnectionState()
	var wg sync.WaitGroup

	// Test concurrent reads and writes
	for i := 0; i < 100; i++ {
		wg.Add(4)

		go func() {
			defer wg.Done()
			cs.SetRunning(true)
		}()

		go func() {
			defer wg.Done()
			cs.SetConnected(true)
		}()

		go func() {
			defer wg.Done()
			_ = cs.IsRunning()
		}()

		go func() {
			defer wg.Done()
			_ = cs.IsConnected()
		}()
	}

	wg.Wait()
}

func TestManagedWaitGroup_Go(t *testing.T) {
	mwg := &ManagedWaitGroup{}

	done := make(chan struct{})

	mwg.Go(func() {
		time.Sleep(10 * time.Millisecond)
		close(done)
	})

	// Wait for the goroutine to finish
	mwg.Wait()

	// Verify the goroutine ran
	select {
	case <-done:
		// Success
	default:
		t.Error("Expected goroutine to have run")
	}
}

func TestManagedWaitGroup_MultipleGo(t *testing.T) {
	mwg := &ManagedWaitGroup{}

	var counter int64
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		mwg.Go(func() {
			mu.Lock()
			counter++
			mu.Unlock()
		})
	}

	mwg.Wait()

	if counter != 10 {
		t.Errorf("Expected counter to be 10, got %d", counter)
	}
}
