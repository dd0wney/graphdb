package replication

import (
	"sync"
	"sync/atomic"
)

// ConnectionState provides thread-safe connection state management.
// It can be embedded in structs that need to track running/connected status.
type ConnectionState struct {
	running   atomic.Bool
	connected atomic.Bool
	mu        sync.Mutex // protects startup/shutdown sequences
}

// NewConnectionState creates a new connection state.
func NewConnectionState() *ConnectionState {
	return &ConnectionState{}
}

// IsRunning returns whether the connection is running.
func (cs *ConnectionState) IsRunning() bool {
	return cs.running.Load()
}

// IsConnected returns whether the connection is established.
func (cs *ConnectionState) IsConnected() bool {
	return cs.connected.Load()
}

// SetRunning sets the running state.
func (cs *ConnectionState) SetRunning(running bool) {
	cs.running.Store(running)
}

// SetConnected sets the connected state.
func (cs *ConnectionState) SetConnected(connected bool) {
	cs.connected.Store(connected)
}

// TryStart attempts to start, returning false if already running.
// The returned unlock function must be called when startup is complete.
func (cs *ConnectionState) TryStart() (unlock func(), alreadyRunning bool) {
	cs.mu.Lock()
	if cs.running.Load() {
		cs.mu.Unlock()
		return nil, true
	}
	return func() { cs.mu.Unlock() }, false
}

// TryStop attempts to stop, returning false if not running.
// The returned unlock function must be called when shutdown is complete.
func (cs *ConnectionState) TryStop() (unlock func(), notRunning bool) {
	cs.mu.Lock()
	if !cs.running.Load() {
		cs.mu.Unlock()
		return nil, true
	}
	return func() { cs.mu.Unlock() }, false
}

// MarkStarted marks the connection as running and connected.
func (cs *ConnectionState) MarkStarted() {
	cs.running.Store(true)
	cs.connected.Store(true)
}

// MarkStopped marks the connection as not running and disconnected.
func (cs *ConnectionState) MarkStopped() {
	cs.running.Store(false)
	cs.connected.Store(false)
}

// WaitGroup provides a managed wait group for tracking goroutines.
type ManagedWaitGroup struct {
	wg sync.WaitGroup
}

// Add adds delta to the wait group counter.
func (mwg *ManagedWaitGroup) Add(delta int) {
	mwg.wg.Add(delta)
}

// Done decrements the wait group counter by one.
func (mwg *ManagedWaitGroup) Done() {
	mwg.wg.Done()
}

// Wait blocks until the wait group counter is zero.
func (mwg *ManagedWaitGroup) Wait() {
	mwg.wg.Wait()
}

// Go runs a function in a goroutine and tracks it with the wait group.
// The function should return when ctx is cancelled.
func (mwg *ManagedWaitGroup) Go(fn func()) {
	mwg.wg.Add(1)
	go func() {
		defer mwg.wg.Done()
		fn()
	}()
}
