package lsm

import (
	"testing"
	"time"
)

// flush and compact take lsm.mu and then lsm.stats.mu. GetStats must take them
// in the same order: in the opposite order, a GetStats that holds stats.mu and
// waits for lsm.mu, against a flush that holds lsm.mu and waits for stats.mu,
// blocks both for ever.
//
// The test plays flush's half by holding lsm.mu, then checks whether GetStats
// holds stats.mu while it waits. That shows the inversion without having to
// win the race that turns it into a deadlock.
func TestGetStats_LockOrderMatchesFlush(t *testing.T) {
	lsm := newTestLSM(t)
	defer lsm.Close()

	lsm.mu.Lock()
	done := make(chan struct{})
	go func() {
		lsm.GetStats()
		close(done)
	}()

	// After the fix GetStats blocks on lsm.mu and never reaches stats.mu, so
	// this window only bounds how long the pass takes.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !lsm.stats.mu.TryLock() {
			lsm.mu.Unlock()
			<-done
			t.Fatal("GetStats holds lsm.stats.mu while it waits for lsm.mu: the opposite order to flush and compact, which deadlocks")
		}
		lsm.stats.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	lsm.mu.Unlock()
	<-done
}
